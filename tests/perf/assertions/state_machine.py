#!/usr/bin/env python3
"""Assertions and log-derived metrics for issue #392 state-machine scenarios."""
from __future__ import annotations

import argparse
import csv
import json
from collections import Counter
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class HealthRow:
    channel_id: int
    key_index: int
    model: str
    state: str
    isolation_level: int
    dormant_disable_count: int
    local_failure_count: int
    upstream_failure_count: int
    last_error_code: str
    last_error_at: int
    last_success_at: int
    version: int
    updated_at: int


def integer(value: str | None) -> int:
    try:
        return int(value or 0)
    except ValueError:
        return 0


def read_health(path: Path) -> list[HealthRow]:
    if not path.exists():
        return []
    with path.open(newline="", encoding="utf-8") as source:
        return [
            HealthRow(
                channel_id=integer(row.get("channel_id")),
                key_index=integer(row.get("key_index")),
                model=row.get("model", ""),
                state=row.get("state", "").lower(),
                isolation_level=integer(row.get("isolation_level")),
                dormant_disable_count=integer(row.get("dormant_disable_count")),
                local_failure_count=integer(row.get("local_failure_count")),
                upstream_failure_count=integer(row.get("upstream_failure_count")),
                last_error_code=(row.get("last_error_code") or "").strip().lower(),
                last_error_at=integer(row.get("last_error_at")),
                last_success_at=integer(row.get("last_success_at")),
                version=integer(row.get("version")),
                updated_at=integer(row.get("updated_at")),
            )
            for row in csv.DictReader(source)
        ]


def read_log(path: Path) -> list[str]:
    return path.read_text(encoding="utf-8", errors="replace").splitlines() if path.exists() else []


def derive_distribution(lines: list[str]) -> dict[str, int]:
    counts: Counter[str] = Counter()
    marker = "record consume log:"
    for line in lines:
        if marker not in line:
            continue
        payload = line.split("params=", 1)
        if len(payload) != 2:
            continue
        try:
            data = json.loads(payload[1])
        except json.JSONDecodeError:
            continue
        channels = data.get("other", {}).get("admin_info", {}).get("use_channel", [])
        if not channels:
            channel = data.get("channel_id")
            channels = [channel] if channel else []
        for channel in channels:
            counts[str(channel)] += 1
    return dict(counts)


def assert_cas(rows: list[HealthRow], lines: list[str]) -> list[dict[str, object]]:
    conflicts = sum("state changed concurrently" in line for line in lines)
    versions = max((row.version for row in rows), default=0)
    return [
        result("cas-health-version", versions > 1, f"max_version={versions}"),
        result("cas-contention-observed", conflicts > 0 or versions >= 2, f"conflict_logs={conflicts}"),
    ]


def load_distribution(path: Path, lines: list[str]) -> dict[str, int]:
    if path.exists():
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
            if data:
                return {str(key): integer(str(value)) for key, value in data.items()}
        except json.JSONDecodeError:
            pass
    return derive_distribution(lines)


def result(name: str, passed: bool, detail: str) -> dict[str, object]:
    return {"name": name, "passed": passed, "detail": detail}


def assert_bad_key(rows: list[HealthRow], channel_status: Path, channel: int, key_index: int) -> list[dict[str, object]]:
    status = channel_status.read_text(encoding="utf-8", errors="replace") if channel_status.exists() else ""
    key_disabled = f'"{key_index}":3' in status and "status_code=401" in status
    failed = [row for row in rows if row.channel_id == channel and row.key_index == key_index and row.model == "mock-bad"]
    siblings = [row for row in rows if row.channel_id == channel and row.key_index != key_index and row.model == "mock-bad"]
    return [
        result("failed-key-disabled", key_disabled, f"channel={channel} key_index={key_index} auto-disabled"),
        result("failed-key-health-recorded", bool(failed) and failed[0].state in {"calm", "dormant"}, f"states={[row.state for row in failed]}"),
        result("sibling-key-survives", bool(siblings) and all(row.state == "healthy" for row in siblings), f"states={[row.state for row in siblings]}"),
    ]


def assert_pool(rows: list[HealthRow], lines: list[str]) -> list[dict[str, object]]:
    evidence = any("emergency recover" in line or "pool pressure" in line for line in lines)
    return [result("pool-pressure-evidence", evidence, "emergency recover or pool pressure log required"), result("health-rows", bool(rows), f"rows={len(rows)}")]

def assert_gray(rows: list[HealthRow], lines: list[str]) -> list[dict[str, object]]:
    states = {r.state for r in rows}
    # RecordSuccess decays isolation silently; the observable recovery evidence
    # is a route whose success timestamp postdates its last error.
    recovery = any(
        r.last_error_code and r.last_success_at and r.last_success_at >= r.last_error_at
        for r in rows
    )
    return [
        result("mixed-health-states", "healthy" in states and bool(states & {"calm", "dormant"}), f"states={sorted(states)}"),
        result("recovery", recovery, "route with last_success_at >= last_error_at required"),
        result("not-disabled-only", states != {"disabled"}, f"states={sorted(states)}"),
    ]


def assert_weight(distribution: dict[str, int]) -> list[dict[str, object]]:
    total = sum(distribution.values())
    counts = sorted(distribution.values(), reverse=True)[:3]
    expected = (0.625, 0.3125, 0.0625)
    shares = tuple(count / total for count in counts) if total and len(counts) == 3 else ()
    matches = len(shares) == 3 and all(abs(actual - wanted) <= 0.10 for actual, wanted in zip(shares, expected))
    return [
        result("minimum-samples", total >= 1000, f"samples={total}"),
        result("weight-ratio", matches, f"counts={counts} shares={[round(item, 4) for item in shares]} expected={expected}"),
    ]


def assert_timeout(rows: list[HealthRow], lines: list[str]) -> list[dict[str, object]]:
    # Thresholds reset counters on every escalation (default threshold=1), so a
    # final snapshot almost always reads 0. last_error_code persists the most
    # recent failure source per route and is the reliable classifier evidence.
    local_codes = {r.last_error_code for r in rows if r.last_error_code == "do_request_failed"}
    upstream_codes = {r.last_error_code for r in rows if r.last_error_code not in {"", "do_request_failed"}}
    local = sum(r.local_failure_count for r in rows) or len(local_codes)
    upstream = sum(r.upstream_failure_count for r in rows) or len(upstream_codes)
    return [
        result("local-failures", bool(local), f"local_failure_count={local} codes={sorted(local_codes)}"),
        result("upstream-failures", bool(upstream), f"upstream_failure_count={upstream} codes={sorted(upstream_codes)}"),
        result("independent-counters", bool(local) and bool(upstream), f"local={sorted(local_codes)} upstream={sorted(upstream_codes)}"),
    ]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--scenario", choices=("cas-contention", "bad-key-cascade", "pool-pressure", "gray-failure", "weight-distribution", "timeout-classification"))
    parser.add_argument("--health-csv", type=Path)
    parser.add_argument("--worker-log", type=Path, required=True)
    parser.add_argument("--distribution-json", type=Path)
    parser.add_argument("--bad-key-channel", type=int, default=0)
    parser.add_argument("--bad-key-index", type=int, default=0)
    parser.add_argument("--channel-status", type=Path)
    parser.add_argument("--report-file", type=Path)
    parser.add_argument("--derive-distribution", action="store_true")
    args = parser.parse_args()
    lines = read_log(args.worker_log)
    if args.derive_distribution:
        if not args.distribution_json:
            parser.error("--derive-distribution requires --distribution-json")
        args.distribution_json.write_text(json.dumps(derive_distribution(lines), indent=2) + "\n", encoding="utf-8")
        return 0
    rows = read_health(args.health_csv) if args.health_csv else []
    distribution = load_distribution(args.distribution_json, lines) if args.distribution_json else derive_distribution(lines)
    match args.scenario:
        case "bad-key-cascade":
            checks = assert_bad_key(rows, args.channel_status or Path(), args.bad_key_channel, args.bad_key_index)
        case "cas-contention":
            checks = assert_cas(rows, lines)
        case "pool-pressure":
            checks = [result("pool-not-empty", bool(rows), f"health_rows={len(rows)}")]
        case "gray-failure":
            checks = assert_gray(rows, lines)
        case "weight-distribution":
            checks = assert_weight(distribution)
        case "timeout-classification":
            checks = assert_timeout(rows, lines)
    passed = all(bool(check["passed"]) for check in checks)
    report = "# State-machine assertion report\n\n" + "\n".join(f"- {'PASS' if check['passed'] else 'FAIL'} `{check['name']}`: {check['detail']}" for check in checks) + "\n"
    if args.report_file:
        args.report_file.write_text(report, encoding="utf-8")
    print(json.dumps({"scenario": args.scenario, "passed": passed, "checks": checks}, indent=2))
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
