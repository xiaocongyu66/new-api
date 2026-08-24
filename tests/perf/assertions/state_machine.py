#!/usr/bin/env python3
"""Assertions and log-derived metrics for issue #392 state-machine scenarios."""
from __future__ import annotations

import argparse
import csv
import json
import re
import sys
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


def assert_bad_key(rows: list[HealthRow], lines: list[str], channel: int, key_index: int) -> list[dict[str, object]]:
    disabled = {"disabled"}
    failed = [r for r in rows if r.channel_id == channel and r.key_index == key_index]
    siblings = [r for r in rows if r.channel_id == channel and r.key_index != key_index]
    verification = any("key verification failed" in line or "key verification cascade" in line for line in lines)
    return [
        result("key-verification", verification, "key verification failed/cascade log required"),
        result("failed-key-disabled", bool(failed) and all(r.state in disabled for r in failed), f"rows={len(failed)} states={[r.state for r in failed]}"),
        result("sibling-key-survives", bool(siblings) and all(r.state not in disabled for r in siblings), f"siblings={len(siblings)} states={[r.state for r in siblings]}"),
    ]


def assert_pool(rows: list[HealthRow], lines: list[str]) -> list[dict[str, object]]:
    evidence = any("emergency recover" in line or "pool pressure" in line for line in lines)
    return [result("pool-pressure-evidence", evidence, "emergency recover or pool pressure log required"), result("health-rows", bool(rows), f"rows={len(rows)}")]


def assert_gray(rows: list[HealthRow], lines: list[str]) -> list[dict[str, object]]:
    states = {r.state for r in rows}
    recovery = any("route isolation" in line and "healthy" in line for line in lines)
    return [
        result("mixed-health-states", "healthy" in states and bool(states & {"calm", "dormant"}), f"states={sorted(states)}"),
        result("recovery", recovery, "route isolation -> healthy log required"),
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
    local = sum(row.local_failure_count for row in rows)
    upstream = sum(row.upstream_failure_count for row in rows)
    if not rows:
        local = sum("timeout" in line.lower() or "context deadline" in line.lower() for line in lines)
        upstream = sum("channel error" in line.lower() and "status code: 5" in line.lower() for line in lines)
    return [
        result("local-failures", local > 0, f"local_failure_count={local}"),
        result("upstream-failures", upstream > 0, f"upstream_failure_count={upstream}"),
        result("independent-counters", local > 0 and upstream > 0, f"local={local} upstream={upstream}"),
    ]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--scenario", choices=("cas-contention", "bad-key-cascade", "pool-pressure", "gray-failure", "weight-distribution", "timeout-classification"))
    parser.add_argument("--health-csv", type=Path)
    parser.add_argument("--worker-log", type=Path, required=True)
    parser.add_argument("--distribution-json", type=Path)
    parser.add_argument("--bad-key-channel", type=int, default=0)
    parser.add_argument("--bad-key-index", type=int, default=0)
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
            checks = assert_bad_key(rows, lines, args.bad_key_channel, args.bad_key_index)
        case "cas-contention":
            checks = assert_cas(rows, lines)
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
