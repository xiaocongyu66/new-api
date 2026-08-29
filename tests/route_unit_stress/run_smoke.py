#!/usr/bin/env python3
"""S11 data chain smoke runner for route-unit EWMA stress (#418).

Runs end-to-end: healthz preflight -> concurrent /v1/chat/completions with X-Request-Id
-> fetch audit endpoint -> read mock ndjson -> reconcile -> write artifacts.
Supports --replay mode for dry-run against existing ndjson files.

Auth separation: --token is the sk- API key for relay /chat/completions;
--admin-token is the admin JWT (from /api/user/login) for /api/route_unit/audit.
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import requests

# Import from sibling module
sys.path.insert(0, str(Path(__file__).parent))
from lib_reconcile import reconcile, ReconcileResult


@dataclass
class SmokeConfig:
    gateway_url: str
    token: str              # sk- API key for relay /chat/completions
    admin_token: str        # admin JWT for /api/route_unit/audit
    mock_url: str
    requests: int = 100
    concurrency: int = 8
    out_dir: Path = Path("runtime/smoke")
    model: str = "mock-ok"
    replay: bool = False
    audit_file: Path | None = None
    mock_file: Path | None = None


def healthz_preflight(gateway_url: str, token: str, timeout: float = 5.0) -> bool:
    """Check gateway /api/status and /v1/models endpoints."""
    headers = {"Authorization": f"Bearer {token}"}
    base = gateway_url.rstrip("/")
    try:
        r = requests.get(f"{base}/api/status", headers=headers, timeout=timeout)
        if r.status_code != 200:
            return False
        r = requests.get(f"{base}/v1/models", headers=headers, timeout=timeout)
        return r.status_code == 200
    except Exception:
        return False


def send_one_request(
    gateway_url: str,
    token: str,
    model: str,
    request_id: str,
    timeout: float = 30.0,
) -> tuple[str, int, str | None]:
    """Send one chat completion request. Returns (request_id, http_status, error_msg)."""
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
        "X-Request-Id": request_id,
        "X-Mock-Mode": "ok",  # Use ok mode for smoke
    }
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": f"smoke test {request_id[:8]}"}],
        "max_tokens": 16,
        "stream": False,
    }
    try:
        r = requests.post(
            f"{gateway_url.rstrip('/')}/v1/chat/completions",
            headers=headers,
            json=payload,
            timeout=timeout,
        )
        return request_id, r.status_code, None if r.status_code == 200 else r.text[:200]
    except Exception as e:
        return request_id, 0, str(e)


def fetch_audit(gateway_url: str, admin_token: str, timeout: float = 10.0) -> list[dict[str, Any]] | None:
    """Fetch audit attempts from gateway using admin JWT.
    Returns list of attempt dicts or None on failure."""
    headers = {"Authorization": f"Bearer {admin_token}"}
    try:
        r = requests.get(
            f"{gateway_url.rstrip('/')}/api/route_unit/audit",
            headers=headers,
            timeout=timeout,
        )
        if r.status_code != 200:
            return None
        data = r.json()
        # Response: {"attempts": [...], "shares": [...]}
        return data.get("attempts", [])
    except Exception:
        return None


def read_mock_ndjson(path: Path) -> list[dict[str, Any]]:
    """Read mock upstream ndjson file."""
    rows = []
    if not path.exists():
        return rows
    with path.open("r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                rows.append(json.loads(line))
            except json.JSONDecodeError:
                pass
    return rows


def write_ndjson(path: Path, rows: list[dict[str, Any]]) -> None:
    """Write list of dicts as ndjson."""
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        for row in rows:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")


def run_smoke(config: SmokeConfig) -> tuple[int, ReconcileResult | None]:
    """Run the smoke test. Returns (exit_code, result)."""
    config.out_dir.mkdir(parents=True, exist_ok=True)

    # Replay mode: read existing files and reconcile
    if config.replay:
        if not config.audit_file or not config.mock_file:
            print("ERROR: --replay requires --audit-file and --mock-file", file=sys.stderr)
            return 2, None
        attempts = read_mock_ndjson(config.audit_file)  # audit ndjson format same as mock
        upstream = read_mock_ndjson(config.mock_file)
        result = reconcile(attempts, upstream)
        summary_path = config.out_dir / "summary.json"
        summary_path.write_text(json.dumps(result.to_summary(), indent=2, ensure_ascii=False))
        print(f"Replay done. Verdict: {result.verdict}")
        print(f"Summary written to {summary_path}")
        return 0 if result.verdict == "PASS" else 1, result

    # Live mode
    print(f"[1/5] Healthz preflight: {config.gateway_url}")
    if not healthz_preflight(config.gateway_url, config.token):
        print("ERROR: Gateway healthz preflight failed", file=sys.stderr)
        return 2, None
    print("      OK")

    print(f"[2/5] Sending {config.requests} requests @ concurrency {config.concurrency}")
    request_ids = [str(uuid.uuid4()) for _ in range(config.requests)]
    sent_rows: list[dict[str, Any]] = []

    with ThreadPoolExecutor(max_workers=config.concurrency) as ex:
        futures = [
            ex.submit(send_one_request, config.gateway_url, config.token, config.model, rid)
            for rid in request_ids
        ]
        for fut in as_completed(futures):
            rid, status, err = fut.result()
            sent_rows.append({"request_id": rid, "status": status, "error": err})

    # Write requests log
    requests_path = config.out_dir / "requests.ndjson"
    write_ndjson(requests_path, sent_rows)
    print(f"      Done. Written to {requests_path}")

    print(f"[3/5] Fetching audit from gateway (admin JWT)")
    # The audit record is written after the response is handed back, so a fixed
    # sleep races the last few requests and they get reported as missing_in_audit.
    # Poll until every sent request is present, or until the records stop arriving.
    sent_ids = {row["request_id"] for row in sent_rows}
    attempts: list[dict[str, Any]] | None = None
    deadline = time.time() + 30.0
    stable_since: tuple[float, int] | None = None
    while True:
        attempts = fetch_audit(config.gateway_url, config.admin_token)
        if attempts is None:
            print("ERROR: Failed to fetch audit endpoint", file=sys.stderr)
            return 2, None
        seen = {a.get("client_request_id") for a in attempts}
        if sent_ids <= seen:
            break
        if time.time() >= deadline:
            print(f"      audit still missing {len(sent_ids - seen)} of {len(sent_ids)} after 30s")
            break
        # Give up early once the count holds steady, so a genuinely dropped record
        # does not cost the full timeout.
        if stable_since is not None and len(seen & sent_ids) == stable_since[1] and time.time() - stable_since[0] > 5.0:
            print(f"      audit count steady at {stable_since[1]}; not waiting further")
            break
        if stable_since is None or len(seen & sent_ids) != stable_since[1]:
            stable_since = (time.time(), len(seen & sent_ids))
        time.sleep(0.5)
    print(f"      Got {len(attempts)} attempt records")

    # Write gateway attempts
    attempts_path = config.out_dir / "gateway-attempts.ndjson"
    write_ndjson(attempts_path, attempts)
    print(f"      Written to {attempts_path}")

    print(f"[4/5] Reading mock ndjson from {config.mock_url}")
    # Local runs share a filesystem with the mock, so the file is authoritative.
    # When the mock runs inside the cluster (required for the gateway to reach it)
    # there is no shared file and the export endpoint is the only source.
    mock_ndjson_path = config.mock_file or Path(os.getenv("MOCK_NDJSON", "/tmp/mock_upstream.ndjson"))
    upstream_rows = read_mock_ndjson(mock_ndjson_path)
    if not upstream_rows:
        try:
            r = requests.get(f"{config.mock_url.rstrip('/')}/_ndjson", timeout=10.0)
            if r.status_code == 200:
                for line in r.text.strip().split("\n"):
                    if line:
                        upstream_rows.append(json.loads(line))
                print(f"      Fetched {len(upstream_rows)} rows from {config.mock_url}/_ndjson")
            else:
                print(f"      mock /_ndjson returned {r.status_code}")
        except Exception as exc:
            print(f"      mock /_ndjson unreachable: {exc}")

    if not upstream_rows:
        print("WARNING: No mock upstream rows found", file=sys.stderr)

    upstream_path = config.out_dir / "upstream-received.ndjson"
    write_ndjson(upstream_path, upstream_rows)
    print(f"      Got {len(upstream_rows)} rows. Written to {upstream_path}")

    print(f"[5/5] Reconciling...")
    result = reconcile(
        attempts,
        upstream_rows,
        expected_requests=config.requests,
        expected_request_ids=set(request_ids),
    )

    summary_path = config.out_dir / "summary.json"
    summary_path.write_text(json.dumps(result.to_summary(), indent=2, ensure_ascii=False))
    print(f"      Verdict: {result.verdict}")
    print(f"      Matched: {result.matched_pairs}/{result.total_requests}")
    print(f"      Summary: {summary_path}")

    if result.verdict != "PASS":
        if result.missing_in_audit:
            print(f"      Missing in audit: {result.missing_in_audit}")
        if result.missing_in_mock:
            print(f"      Missing in mock: {result.missing_in_mock}")
        if result.identity_mismatch:
            print(f"      Identity mismatches: {len(result.identity_mismatch)}")
        if result.attempt_gaps:
            print(f"      Attempt gaps: {len(result.attempt_gaps)}")

    return 0 if result.verdict == "PASS" else 1, result


def parse_args() -> SmokeConfig:
    p = argparse.ArgumentParser(description="S11 route-unit EWMA stress smoke runner")
    p.add_argument("--gateway-url", help="Gateway base URL without /v1 suffix (e.g. http://localhost:3000)")
    p.add_argument("--token", help="sk- API key for relay /chat/completions")
    p.add_argument("--admin-token", help="Admin JWT (from /api/user/login access_token) for /api/route_unit/audit")
    p.add_argument("--mock-url", help="Mock upstream base URL (e.g. http://localhost:8099)")
    p.add_argument("--requests", type=int, default=100, help="Number of requests to send")
    p.add_argument("--concurrency", type=int, default=8, help="Concurrent workers")
    p.add_argument("--out-dir", type=Path, default=Path("runtime/smoke"), help="Output directory for artifacts")
    p.add_argument("--model", default="mock-ok", help="Model name to request")
    p.add_argument("--replay", action="store_true", help="Dry-run: reconcile existing ndjson files")
    p.add_argument("--audit-file", type=Path, help="Audit ndjson file for --replay")
    p.add_argument("--mock-file", type=Path, help="Mock ndjson file for --replay")
    args = p.parse_args()
    # Validate required args based on mode
    if not args.replay:
        missing = []
        if not args.gateway_url:
            missing.append("--gateway-url")
        if not args.token:
            missing.append("--token")
        if not args.admin_token:
            missing.append("--admin-token")
        if not args.mock_url:
            missing.append("--mock-url")
        if missing:
            p.error(f"the following arguments are required: {', '.join(missing)}")
    else:
        if not args.audit_file or not args.mock_file:
            p.error("--replay requires --audit-file and --mock-file")

    return SmokeConfig(
        gateway_url=args.gateway_url or "",
        token=args.token or "",
        admin_token=args.admin_token or "",
        mock_url=args.mock_url or "",
        requests=args.requests,
        concurrency=args.concurrency,
        out_dir=args.out_dir,
        model=args.model,
        replay=args.replay,
        audit_file=args.audit_file,
        mock_file=args.mock_file,
    )


def main() -> int:
    config = parse_args()
    exit_code, _ = run_smoke(config)
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())