#!/usr/bin/env python3
"""Mirror wildtoken's upstream catalog into this repo's dev new-api instance.

wt's `upstreams` table (sqlite, read-only) is the maintainer's curated source of
truth. This tool reconciles the new-api `channels` table toward it, one OpenAI
channel per upstream:

  * client alias list = model_names ∪ model_mappings keys;
  * model_mapping is written as JSON `{"client": "upstream"}` (route builder
    contract); identity aliases stay out of the mapping;
  * base_url drops the trailing `/v1` (the relay appends the path itself);
  * disabled wt rows mirror to status=2 (disabled) — wt's kill wins;
  * header_override always carries the opencode passthrough set (pie-xian
    family 405-bans clients without the opencode fingerprint) merged with the
    upstream's own extra_headers;
  * wt api_key moves through memory only; the report says `key=<set|empty>`.

Channel names are the pairing key (UNIQUE in wt, matched exactly here).
Existing channels are UPDATEd toward wt (mirror policy); dev-local channels
whose name is not in wt are never touched.

Usage:
  WT_PG_DSN must point at the new-api Postgres (or use the default dev DSN).
  python3 scripts/wt_import.py [--apply]
"""
import json
import os
import re
import sqlite3
import subprocess
import sys

WT_DB = os.environ.get("WT_DB", "/home/hathaway/projects/wildtoken/wildtoken.db")
# The dev Postgres runs in a container on this workstation/on hosts without
# native psql. Set WT_PG_CONTAINER="" plus WT_PG_DSN for a direct connection.
PG_CONTAINER = os.environ.get("WT_PG_CONTAINER", "new-api-dev-pg")
PG_USER = os.environ.get("WT_PG_USER", "root")
PG_DATABASE = os.environ.get("WT_PG_DB", "new-api")
PG_DSN = os.environ.get("WT_PG_DSN", "")

# The fingerprint the pie-xian family authenticates on; {client_header:...}
# forwards the caller's own value so any compliant client passes through.
PASSTHROUGH = {
    "re:^x-opencode-": "",
    "user-agent": "{client_header:user-agent}",
    "anthropic-version": "{client_header:anthropic-version}",
}


def psql_args() -> list:
    if PG_CONTAINER:
        return ["docker", "exec", PG_CONTAINER, "psql", "-U", PG_USER,
                "-d", PG_DATABASE, "-t", "-A", "-F", "|"]
    return ["psql", PG_DSN, "-t", "-A", "-F", "|"]


def psql(sql: str) -> str:
    r = subprocess.run(psql_args() + ["-v", "ON_ERROR_STOP=1", "-c", sql],
                       capture_output=True, text=True, check=True)
    return r.stdout


def psql_exec(sql: str) -> None:
    psql(sql)


def sql_quote(s: str) -> str:
    return "'" + s.replace("'", "''") + "'"


def strip_v1(url: str) -> str:
    return re.sub(r"/v1/?$", "", url.rstrip("/")) or url


def main() -> None:
    apply = "--apply" in sys.argv
    con = sqlite3.connect(f"file:{WT_DB}?mode=ro", uri=True)
    rows = con.execute(
        "SELECT id, name, base_url, api_key, model_names, model_mappings, "
        "enabled, extra_headers FROM upstreams ORDER BY id").fetchall()

    existing = {}
    for line in psql("SELECT name, id FROM channels").strip().splitlines():
        if line:
            name, cid = line.split("|", 1)
            existing[name] = cid

    stmts, report = [], []
    for _id, name, base_url, key, names_json, map_json, enabled, hdr_json in rows:
        names = json.loads(names_json or "[]")
        mapping = json.loads(map_json or "{}")
        wt_headers = {k.lower(): v for k, v in json.loads(hdr_json or "{}").items()}
        headers = {**PASSTHROUGH, **wt_headers}
        aliases = sorted(set(names) | set(mapping))
        status = 1 if enabled else 2
        clean_base = strip_v1(base_url or "")
        mapping_out = json.dumps(
            {a: u for a, u in mapping.items() if a != u}, ensure_ascii=False)
        models_csv = ",".join(aliases)

        q_name, q_base = sql_quote(name), sql_quote(clean_base)
        q_models, q_map = sql_quote(models_csv), sql_quote(mapping_out)
        q_hdr = sql_quote(json.dumps(headers, ensure_ascii=False))
        q_key = sql_quote(key or "")
        # The relay reaches a channel through (group, alias, channel) rows in
        # abilities; the admin write path maintains them transactionally with
        # channels, so a direct-SQL mirror must rebuild them in the same tx.
        enabled_sql = "true" if status == 1 else "false"
        if name in existing:
            cid = existing[name]
            stmts.append(
                f"UPDATE channels SET base_url={q_base}, key={q_key}, "
                f"models={q_models}, model_mapping={q_map}, "
                f"header_override={q_hdr}, status={status} "
                f"WHERE name={q_name};")
            stmts.append(
                f"DELETE FROM abilities WHERE channel_id={cid};")
            stmts.append(
                "INSERT INTO abilities(\"group\",model,channel_id,enabled,tag)"
                f" SELECT 'default', m, {cid}, {enabled_sql}, '' FROM "
                f"unnest(string_to_array({q_models}, ',')) m;")
            report.append(f"UPDATE {name} id={cid} "
                          f"models={len(aliases)} status={status} "
                          f"key={'set' if key else 'empty'} hdr_extra="
                          f"{','.join(sorted(wt_headers)) or '-'}")
        else:
            stmts.append(
                "WITH ins AS (INSERT INTO channels (type,key,status,name,"
                "created_time,base_url,\"group\",models,model_mapping,"
                f"header_override) VALUES (1,{q_key},{status},{q_name},"
                f"EXTRACT(EPOCH FROM NOW())::bigint,{q_base},'default',"
                f"{q_models},{q_map},{q_hdr}) "
                "RETURNING id, \"group\") "
                "INSERT INTO abilities(\"group\",model,channel_id,enabled,tag)"
                f" SELECT ins.\"group\", m, ins.id, {enabled_sql}, '' "
                f"FROM ins, unnest(string_to_array({q_models}, ',')) m;")
            report.append(f"INSERT {name} models={len(aliases)} "
                          f"status={status} key={'set' if key else 'empty'} "
                          f"hdr_extra={','.join(sorted(wt_headers)) or '-'}")

    for line in report:
        print(line)
    print(f"total={len(report)} "
          f"(wt upstreams; disabled mirrored as status=2)")
    if not apply:
        print("dry run — pass --apply to write")
        return

    script = "BEGIN;\n" + "\n".join(stmts) + "\nCOMMIT;\n"
    if PG_CONTAINER:
        runner = ["docker", "exec", "-i", PG_CONTAINER, "psql", "-q",
                  "-U", PG_USER, "-d", PG_DATABASE,
                  "-v", "ON_ERROR_STOP=1", "-f", "-"]
    else:
        runner = ["psql", PG_DSN, "-q", "-v", "ON_ERROR_STOP=1", "-f", "-"]
    r = subprocess.run(runner, input=script, capture_output=True, text=True,
                       check=False)
    if r.returncode != 0:
        sys.exit("transaction failed, nothing committed:\n" + r.stderr)
    print("applied. Route units rebuild through the channel write path only; "
          "after a direct-SQL import, restart the API (bootstrap seeds "
          "channel_model_routes) or edit a channel via the admin API.")


if __name__ == "__main__":
    main()
