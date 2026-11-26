#!/usr/bin/env python3
"""按渠道抓取上游价格表。

三档降级（便宜到贵）：
  api     公开 JSON，声明 rows/key/values 路径
  scrape  服务端渲染 HTML，声明 CSS 选择器
  browser Playwright 渲染后取 DOM，交给 scripts/browser_fetch.py

任何一档的结果都要过数值校验，失败整条丢弃并记原因——上游改版时
宁可没数据，也不能把「暂无价格」当成 0 写进报告。
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path
from urllib.parse import urlparse

sys.path.insert(0, str(Path(__file__).parent))
from lib import (  # noqa: E402
    Redactor,
    eval_path,
    http_get_json,
    load_secrets,
    parse_number,
    scalar_text,
    write_json,
)

SPEC_DIR = Path(__file__).parent.parent / "specs"

# 没有 spec 时按顺序试探这两个公开定价接口。
AUTO_PROBES = (
    ("/api/pricing", "data", "model_name", {"model_ratio": "model_ratio", "model_price": "model_price"}),
    (
        "/api/v1/model-plaza",
        "data.groups[].models",
        "name",
        {"input_price": "pricing.input_price", "output_price": "pricing.output_price"},
    ),
)


def load_spec(host: str) -> dict | None:
    path = SPEC_DIR / f"{host}.json"
    if not path.exists():
        return None
    return json.loads(path.read_text(encoding="utf-8"))


def run_api(base: str, spec: dict, token: str | None, redact: Redactor) -> list[dict]:
    url = spec["url"] if spec["url"].startswith("http") else base + spec["url"]
    payload = http_get_json(url, token, redact=redact)

    # sub2api 的 model-plaza 是「组 → 模型」，而每个组的倍率不同。只取
    # groups[0] 会把别的组的价格漏掉或错当成同价，所以按 group_of 逐组展开，
    # 并把组名并进 key，避免不同组同名模型互相覆盖。
    if spec.get("group_of"):
        rows: list[dict] = []
        for group in eval_path(payload, spec["group_of"]):
            if not isinstance(group, dict):
                continue
            label_values = eval_path(group, spec.get("group_name", "name"))
            label = scalar_text(label_values[0]) if label_values else ""
            for node in eval_path(group, spec["rows"]):
                if not isinstance(node, dict):
                    continue
                key_values = eval_path(node, spec["key"])
                key = scalar_text(key_values[0]) if key_values else ""
                if not key:
                    continue
                values = {}
                for name, field in spec["values"].items():
                    found = eval_path(node, field)
                    text = scalar_text(found[0]) if found else ""
                    if text:
                        values[name] = text
                if label:
                    values["group"] = label
                # 组级字段（倍率等）复制到每一行：同一个模型在不同组的实际
                # 价格差别往往全在倍率上，只抓模型价会得出「价格一样」的错觉。
                for name, field in (spec.get("group_values") or {}).items():
                    found = eval_path(group, field)
                    text = scalar_text(found[0]) if found else ""
                    if text:
                        values[name] = text
                # key 必须带组名：不同组有同名模型，只按模型名做 key 会让
                # 后一组覆盖前一组，差异报告直接丢数据。
                group_key = f"{label}::{key}" if label else key
                rows.append({"key": group_key, "values": values})
        return rows

    nodes = eval_path(payload, spec["rows"])
    rows: list[dict] = []
    for node in nodes:
        if not isinstance(node, dict):
            continue
        key_values = eval_path(node, spec["key"])
        key = scalar_text(key_values[0]) if key_values else ""
        if not key:
            continue
        values = {}
        for name, field in spec["values"].items():
            found = eval_path(node, field)
            text = scalar_text(found[0]) if found else ""
            if text:
                values[name] = text
        rows.append({"key": key, "values": values})
    return rows


def run_browser(base: str, spec: dict) -> list[dict]:
    url = spec["url"] if spec["url"].startswith("http") else base + spec["url"]
    job = {
        "url": url,
        "rows": spec["rows"],
        "key": spec["key"],
        "values": spec["values"],
        "timeout_seconds": spec.get("timeout_seconds", 45),
        "storage_state": spec.get("storage_state", ""),
    }
    adapter = Path(__file__).parent / "browser_fetch.py"
    proc = subprocess.run(
        [sys.executable, str(adapter)],
        input=json.dumps(job),
        capture_output=True,
        text=True,
        timeout=job["timeout_seconds"] + 15,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"browser 档失败: {(proc.stderr or '').strip()[:400]}")
    payload = json.loads(proc.stdout)
    return payload.get("rows") or []


def validate(rows: list[dict], spec: dict) -> list[dict]:
    """必需数值字段必须存在且落在区间内，否则整条丢弃。"""
    numeric = spec.get("numeric") or []
    low = spec.get("min")
    high = spec.get("max")
    kept = []
    for row in rows:
        ok = True
        for field in numeric:
            raw = row["values"].get(field, "")
            try:
                value = parse_number(raw)
            except ValueError:
                ok = False
                break
            if low is not None and value < low:
                ok = False
                break
            if high is not None and value > high:
                ok = False
                break
        if ok:
            kept.append(row)
    return kept


def collect_channel(channel: dict, secrets: dict[str, str], redact: Redactor) -> dict:
    host = urlparse(channel["base_url"]).netloc
    spec = load_spec(host)
    upstream_key = secrets.get(f"UPSTREAM_KEY_{channel['id']}", "").strip() or None

    attempts: list[str] = []
    candidates = [spec] if spec else [
        {"strategy": "api", "url": url, "rows": rows, "key": key, "values": values}
        for url, rows, key, values in AUTO_PROBES
    ]

    for candidate in candidates:
        strategy = candidate.get("strategy", "api")
        try:
            if strategy == "api":
                rows = run_api(channel["base_url"], candidate, upstream_key, redact)
            elif strategy == "browser":
                rows = run_browser(channel["base_url"], candidate)
            else:
                raise RuntimeError(f"未实现的 strategy={strategy}")
            rows = validate(rows, candidate)
            if rows:
                return {
                    "channel_id": channel["id"],
                    "channel_name": channel["name"],
                    "host": host,
                    "strategy": strategy,
                    "rows": rows,
                }
            attempts.append(f"{strategy}: 0 行（选择器/路径失配或全部被校验拒绝）")
        except Exception as exc:  # noqa: BLE001 - 逐渠道隔离，一个失败不影响其它
            attempts.append(redact(f"{strategy}: {exc}")[:300])

    return {
        "channel_id": channel["id"],
        "channel_name": channel["name"],
        "host": host,
        "strategy": "none",
        "rows": [],
        "error": " | ".join(attempts) or "无可用 spec",
        "needs_spec": True,
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--channels", default="out/channels.json")
    parser.add_argument("--out", default="out/prices.json")
    parser.add_argument("--only", type=int, default=0, help="只跑某个渠道 id，便于调试")
    args = parser.parse_args()

    secrets = load_secrets()
    redact = Redactor(secrets)
    channels = json.loads(Path(args.channels).read_text(encoding="utf-8"))
    channels = [c for c in channels if c.get("status") == 1 and c.get("base_url")]
    if args.only:
        channels = [c for c in channels if c.get("id") == args.only]

    results = []
    for index, channel in enumerate(channels, 1):
        result = collect_channel(channel, secrets, redact)
        status = f"{len(result['rows'])} 行" if result["rows"] else result.get("error", "无数据")
        print(f"[{index}/{len(channels)}] {channel['name']} ({channel['id']}) -> {result['strategy']}: {status}")
        results.append(result)

    write_json(Path(args.out), results)
    ok = sum(1 for r in results if r["rows"])
    needs = sum(1 for r in results if r.get("needs_spec"))
    print(f"\n完成：{ok}/{len(results)} 个渠道取到价格，{needs} 个需要新增 spec -> {args.out}")
    # 一个都没取到时必须非 0 退出：否则 systemd 认为成功，价格表静默停止更新。
    if ok == 0:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
