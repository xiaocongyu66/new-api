#!/usr/bin/env python3
"""
State Machine Stress Test Matrix Orchestrator.

Maps scenarios 1-8 to k6 scripts, runs them sequentially, collects artifacts,
and validates against assertions. Does not execute cloud commands; only
orchestrates local k6 runs and assertion checks.
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
from dataclasses import dataclass, asdict
from pathlib import Path


# Scenario mapping: number -> (script_name, assertion_alias, description)
SCENARIOS = {
    "1": ("pool-soft-derating.js", "pool-pressure", "Pool soft derating under sustained load"),
    "2": ("cas-contention.js", "pool-pressure", "CAS contention on channel health updates"),
    "3": ("bad-key-cascade.js", "bad-key-cascade", "Bad key cascade with credential failures"),
    "4": ("recovery-decay.js", "gray-failure", "Recovery decay after partial degradation"),
    "5": ("pool-pressure.js", "pool-pressure", "Connection pool pressure and retry exhaustion"),
    "6": ("gray-failure.js", "gray-failure", "Gray failure with partial degradation"),
    "7": ("weight-distribution.js", "weight-distribution", "Weight-based channel selection distribution"),
    "8": ("timeout-classification.js", "timeout-classification", "Transport timeout vs upstream error classification"),
}


@dataclass
class ScenarioResult:
    scenario: str
    name: str
    script: str
    assertion_alias: str
    passed: bool
    k6_exit_code: int
    assertion_exit_code: int
    summary_path: str | None
    report_path: str | None
    detail: str


def run_k6(script: Path, env: dict[str, str], summary_path: Path) -> tuple[int, str]:
    """Run k6 script and return exit code and stdout."""
    k6_env = os.environ.copy()
    k6_env.update(env)
    k6_env["SUMMARY_JSON"] = str(summary_path)

    cmd = ["k6", "run", "--out", f"json={summary_path}", str(script)]
    result = subprocess.run(cmd, env=k6_env, capture_output=True, text=True, timeout=600)
    return result.returncode, result.stdout + result.stderr


def run_assertions(
    assertion_script: Path,
    scenario_alias: str,
    worker_log: Path | None,
    health_csv: Path | None,
    distribution_json: Path | None,
    report_path: Path,
    bad_key_channel: int = 0,
    bad_key_index: int = 0,
) -> tuple[int, str]:
    """Run state_machine.py assertions and return exit code and stdout."""
    cmd = [
        sys.executable,
        str(assertion_script),
        "--scenario",
        scenario_alias,
        "--report-file",
        str(report_path),
    ]
    if worker_log:
        cmd.extend(["--worker-log", str(worker_log)])
    if health_csv:
        cmd.extend(["--health-csv", str(health_csv)])
    if distribution_json:
        cmd.extend(["--distribution-json", str(distribution_json)])
    if scenario_alias == "bad-key-cascade":
        cmd.extend(["--bad-key-channel", str(bad_key_channel), "--bad-key-index", str(bad_key_index)])

    result = subprocess.run(cmd, capture_output=True, text=True, timeout=120)
    return result.returncode, result.stdout + result.stderr


def find_latest_file(pattern: str, search_dir: Path) -> Path | None:
    """Find the most recent file matching pattern in search_dir."""
    matches = list(search_dir.glob(pattern))
    if not matches:
        return None
    return max(matches, key=lambda p: p.stat().st_mtime)


def main() -> int:
    parser = argparse.ArgumentParser(description="Run state machine stress test matrix")
    parser.add_argument("--scenario", required=True, choices=list(SCENARIOS.keys()) + ["all"])
    parser.add_argument("--target-url", required=True)
    parser.add_argument("--api-key", required=True)
    parser.add_argument("--model", default="")
    parser.add_argument("--vus", default="")
    parser.add_argument("--duration", default="")
    parser.add_argument("--report-dir", required=True, type=Path)
    parser.add_argument("--worker-log-source", default="", help="Path or glob for worker logs")
    parser.add_argument("--health-csv-source", default="", help="Path or glob for channel_model_health CSV")
    parser.add_argument("--postgres-host", default="")
    parser.add_argument("--postgres-port", default="")
    parser.add_argument("--postgres-db", default="")
    parser.add_argument("--postgres-user", default="")
    parser.add_argument("--postgres-password", default="")
    parser.add_argument("--run-only", action="store_true", help="Run k6 only; assert after external log/DB capture")
    args = parser.parse_args()

    # Determine which scenarios to run
    if args.scenario == "all":
        scenario_keys = list(SCENARIOS.keys())
    else:
        scenario_keys = [args.scenario]

    # Setup paths
    script_dir = Path(__file__).parent
    scenarios_dir = script_dir / "scenarios"
    assertions_dir = script_dir / "assertions"
    assertion_script = assertions_dir / "state_machine.py"

    report_dir = args.report_dir
    report_dir.mkdir(parents=True, exist_ok=True)

    # Build base k6 environment
    base_env = {
        "TARGET_URL": args.target_url,
        "API_KEY": args.api_key,
    }
    if args.model:
        base_env["MODEL"] = args.model
    if args.vus:
        base_env["VUS"] = args.vus
    if args.duration:
        base_env["DURATION"] = args.duration

    # Postgres env for worker log/health CSV access
    if args.postgres_host:
        base_env["POSTGRES_HOST"] = args.postgres_host
    if args.postgres_port:
        base_env["POSTGRES_PORT"] = args.postgres_port
    if args.postgres_db:
        base_env["POSTGRES_DB"] = args.postgres_db
    if args.postgres_user:
        base_env["POSTGRES_USER"] = args.postgres_user
    if args.postgres_password:
        base_env["POSTGRES_PASSWORD"] = args.postgres_password

    results: list[ScenarioResult] = []
    overall_passed = True

    for key in scenario_keys:
        script_name, assertion_alias, description = SCENARIOS[key]
        script_path = scenarios_dir / script_name

        print(f"\n{'='*60}")
        print(f"Running Scenario {key}: {description}")
        print(f"Script: {script_name}")
        print(f"Assertion alias: {assertion_alias}")
        print(f"{'='*60}")

        if not script_path.exists():
            print(f"ERROR: Scenario script not found: {script_path}")
            result = ScenarioResult(
                scenario=key,
                name=description,
                script=script_name,
                assertion_alias=assertion_alias,
                passed=False,
                k6_exit_code=-1,
                assertion_exit_code=-1,
                summary_path=None,
                report_path=None,
                detail=f"Script not found: {script_path}",
            )
            results.append(result)
            overall_passed = False
            continue

        # Per-scenario directories
        scenario_report_dir = report_dir / f"scenario_{key}"
        scenario_report_dir.mkdir(parents=True, exist_ok=True)

        summary_path = scenario_report_dir / "summary.json"
        report_path = scenario_report_dir / "assertion-report.md"

        # Run k6
        print(f"Starting k6 run for {script_name}...")
        k6_start = time.time()
        k6_exit, k6_output = run_k6(script_path, base_env, summary_path)
        k6_duration = time.time() - k6_start
        print(f"k6 exited with code {k6_exit} in {k6_duration:.1f}s")

        # Locate worker log and health CSV (from env sources or defaults)
        worker_log = None
        health_csv = None
        distribution_json = None

        if args.worker_log_source:
            worker_log = Path(args.worker_log_source)
            if not worker_log.exists():
                worker_log = find_latest_file(args.worker_log_source, Path("."))
        else:
            # Default locations
            worker_log = find_latest_file("worker*.log", Path("."))
            if not worker_log:
                worker_log = find_latest_file("*.log", Path("."))

        if args.health_csv_source:
            health_csv = Path(args.health_csv_source)
            if not health_csv.exists():
                health_csv = find_latest_file(args.health_csv_source, Path("."))
        else:
            health_csv = find_latest_file("*channel_model_health*.csv", Path("."))
            if not health_csv:
                health_csv = find_latest_file("*.csv", Path("."))

        # Assertions run after external capture when --run-only is not set.
        assertion_exit = 0
        assertion_output = ""
        if not args.run_only:
            print(f"Running assertions for {assertion_alias}...")
            assertion_exit, assertion_output = run_assertions(
                assertion_script, assertion_alias, worker_log, health_csv,
                distribution_json, report_path,
            )
            print(f"Assertions exited with code {assertion_exit}")

        passed = k6_exit == 0 and assertion_exit == 0
        if not passed:
            overall_passed = False

        detail = ""
        if k6_exit != 0:
            detail += f"k6 failed (exit {k6_exit}); "
        if assertion_exit != 0:
            detail += f"assertions failed (exit {assertion_exit}); "
        if not detail:
            detail = "All checks passed"

        result = ScenarioResult(
            scenario=key,
            name=description,
            script=script_name,
            assertion_alias=assertion_alias,
            passed=passed,
            k6_exit_code=k6_exit,
            assertion_exit_code=assertion_exit,
            summary_path=str(summary_path) if summary_path.exists() else None,
            report_path=str(report_path) if report_path.exists() else None,
            detail=detail.strip(),
        )
        results.append(result)

        # Print immediate result
        status = "PASS" if passed else "FAIL"
        print(f"Scenario {key} [{assertion_alias}]: {status} - {detail}")

    # Write matrix summary
    matrix_summary = {
        "scenario_mapping": {
            "1": "pool-pressure (pool-soft-derating)",
            "2": "pool-pressure (cas-contention)",
            "3": "bad-key-cascade",
            "4": "gray-failure (recovery-decay)",
            "5": "pool-pressure",
            "6": "gray-failure",
            "7": "weight-distribution",
            "8": "timeout-classification",
        },
        "results": [asdict(r) for r in results],
        "overall_passed": overall_passed,
    }

    matrix_path = report_dir / "matrix-summary.json"
    matrix_path.write_text(json.dumps(matrix_summary, indent=2))
    print(f"\nMatrix summary written to: {matrix_path}")

    # Print final summary
    print(f"\n{'='*60}")
    print("STATE MACHINE MATRIX SUMMARY")
    print(f"{'='*60}")
    for r in results:
        status = "PASS" if r.passed else "FAIL"
        print(f"  Scenario {r.scenario} [{r.assertion_alias}]: {status} - {r.detail}")
    print(f"\nOverall: {'PASS' if overall_passed else 'FAIL'}")

    return 0 if overall_passed else 1


if __name__ == "__main__":
    raise SystemExit(main())