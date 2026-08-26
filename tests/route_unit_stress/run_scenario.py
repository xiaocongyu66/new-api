#!/usr/bin/env python3
"""Scenario runner for route-unit EWMA share convergence (S1/S2/S3 of #418, #451).

Flow: preflight -> affinity gate -> topology -> warmup -> stats phase (with
resource sampling) -> post snapshots -> reconcile -> share evaluation -> verdict.

Verdict priority (highest first):
  ENVIRONMENT_INVALID (exit 2)  gateway unreachable, affinity on, bad topology
  DATA_INVALID        (exit 1)  reconciliation failed or effective samples < requests
  UNDERPOWERED        (exit 1)  --requests below the scenario's min_samples
  PRODUCT_FAIL        (exit 1)  share point estimate or CI outside criteria
  PASS                (exit 0)

Warmup traffic is recorded separately and never enters share fitting.
"""
from __future__ import annotations

import argparse
import json
import sys
import time
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import requests

sys.path.insert(0, str(Path(__file__).resolve().parent))

import lib_reconcile  # noqa: E402
import lib_report  # noqa: E402
import lib_resources  # noqa: E402
import lib_stats  # noqa: E402
class AdminTokenManager:
    """Holds admin JWT and refreshes on 401/403 if credentials provided."""

    def __init__(
        self,
        initial_token: str,
        gateway_url: str,
        username: str | None = None,
        password: str | None = None,
    ) -> None:
        self._token = initial_token
        self._gateway_url = gateway_url.rstrip("/")
        self._username = username
        self._password = password
        self.refresh_count = 0

    def auth_header(self) -> dict[str, str]:
        return {"Authorization": f"Bearer {self._token}"}

    def maybe_refresh(self, status_code: int) -> bool:
        """If 401/403 and credentials available, login and update token. Returns True if refreshed."""
        if status_code not in (401, 403):
            return False
        if not (self._username and self._password):
            return False
        try:
            r = requests.post(
                f"{self._gateway_url}/api/user/login",
                json={"username": self._username, "password": self._password},
                timeout=10,
            )
            if r.status_code == 200:
                data = r.json().get("data", {})
                new_token = data.get("access_token")
                if new_token:
                    self._token = new_token
                    self.refresh_count += 1
                    return True
        except Exception:
            pass
        return False
AFFINITY_OPTION_KEY = "channel_affinity_setting.enabled"
AUDIT_RING_CAPACITY = 32768  # keep in sync with routestats.AuditRingCapacity()


@dataclass
class RouteInfo:
    """One route unit as reported by the topology endpoint."""

    label: str
    channel_id: int
    key_index: int
    upstream_model: str
    static_weight: int
    base_weight: float
    ewma_quality: float
    health_multiplier: float
    share_correction: float
    final_score: float
    sample_count: int
    raw: dict[str, Any]
    enabled: bool = True

    def identity(self) -> tuple[int, int, str]:
        return (self.channel_id, self.key_index, self.upstream_model)


def fetch_options(gateway_url: str, token_mgr: AdminTokenManager) -> dict[str, str] | None:
    """Return the option map, or None when the endpoint is unusable."""
    for attempt in range(2):
        try:
            r = requests.get(
                f"{gateway_url.rstrip('/')}/api/option/",
                headers=token_mgr.auth_header(),
                timeout=10,
            )
            if r.status_code == 200:
                data = r.json().get("data")
                if isinstance(data, list):
                    return {str(o.get("key")): str(o.get("value")) for o in data}
                return None
            if token_mgr.maybe_refresh(r.status_code):
                continue
            return None
        except Exception:
            return None
    return None


def fetch_topology(gateway_url: str, token_mgr: AdminTokenManager, alias: str) -> list[dict[str, Any]] | None:
    """Return route-unit view items, or None when the response shape is wrong."""
    for attempt in range(2):
        try:
            r = requests.get(
                f"{gateway_url.rstrip('/')}/api/channel/route_unit/",
                params={"alias": alias},
                headers=token_mgr.auth_header(),
                timeout=15,
            )
            if r.status_code == 200:
                payload = r.json().get("data")
                if isinstance(payload, dict):
                    items = payload.get("items")
                    if isinstance(items, list):
                        return items
                return None
            if token_mgr.maybe_refresh(r.status_code):
                continue
            return None
        except Exception:
            return None
    return None

def fetch_audit(gateway_url: str, token_mgr: AdminTokenManager) -> list[dict[str, Any]] | None:
    """Return the audit ring contents, or None on failure."""
    for attempt in range(2):
        try:
            r = requests.get(
                f"{gateway_url.rstrip('/')}/api/route_unit/audit",
                headers=token_mgr.auth_header(),
                timeout=30,
            )
            if r.status_code == 200:
                attempts = r.json().get("attempts")
                return attempts if isinstance(attempts, list) else []
            if token_mgr.maybe_refresh(r.status_code):
                continue
            return None
        except Exception:
            return None
    return None


def read_ndjson(path: Path) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
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
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as f:
        for row in rows:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")


def send_request(
    gateway_url: str,
    token: str,
    model: str,
    request_id: str,
    mode: str,
    stream: bool,
    phase: str,
    timeout: float = 60.0,
) -> dict[str, Any]:
    """Send one relay request, measuring latency plus TTFT/ITL when streaming."""
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
        "X-Request-Id": request_id,
        "X-Mock-Mode": mode,
    }
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": f"scenario {request_id[:8]}"}],
        "max_tokens": 16,
        "stream": stream,
    }
    row: dict[str, Any] = {
        "request_id": request_id,
        "phase": phase,
        "stream": stream,
        "mode": mode,
        "status": 0,
        "latency_ms": None,
        "ttft_ms": None,
        "itl_ms": [],
        "error": None,
    }
    started = time.perf_counter()
    try:
        r = requests.post(
            f"{gateway_url.rstrip('/')}/v1/chat/completions",
            headers=headers,
            json=payload,
            timeout=timeout,
            stream=stream,
        )
        row["status"] = r.status_code
        if stream:
            frame_times: list[float] = []
            for raw_line in r.iter_lines(decode_unicode=True):
                if not raw_line or not raw_line.startswith("data:"):
                    continue
                now = time.perf_counter()
                if not frame_times:
                    row["ttft_ms"] = (now - started) * 1000.0
                else:
                    row["itl_ms"].append((now - frame_times[-1]) * 1000.0)
                frame_times.append(now)
        else:
            r.content  # drain so latency covers the whole response
        row["latency_ms"] = (time.perf_counter() - started) * 1000.0
        if r.status_code != 200:
            row["error"] = r.text[:200]
    except Exception as exc:  # network/timeout
        row["latency_ms"] = (time.perf_counter() - started) * 1000.0
        row["error"] = str(exc)[:200]
    return row


def run_phase(
    gateway_url: str,
    token: str,
    model: str,
    mode: str,
    count: int,
    concurrency: int,
    stream_ratio: float,
    phase: str,
) -> tuple[list[dict[str, Any]], list[str]]:
    """Run one traffic phase; stream allocation is deterministic, never random."""
    request_ids = [str(uuid.uuid4()) for _ in range(count)]
    stream_cutoff = int(round(count * stream_ratio))
    rows: list[dict[str, Any]] = []
    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures = [
            pool.submit(
                send_request, gateway_url, token, model, rid, mode, i < stream_cutoff, phase
            )
            for i, rid in enumerate(request_ids)
        ]
        for fut in as_completed(futures):
            rows.append(fut.result())
    return rows, request_ids


def service_quality(rows: list[dict[str, Any]], elapsed_s: float) -> dict[str, Any]:
    """Latency/throughput metrics with success and failure kept separate."""
    ok = [r for r in rows if r["status"] == 200]
    bad = [r for r in rows if r["status"] != 200]
    ok_lat = [r["latency_ms"] for r in ok if r["latency_ms"] is not None]
    bad_lat = [r["latency_ms"] for r in bad if r["latency_ms"] is not None]
    ttft = [r["ttft_ms"] for r in rows if r["ttft_ms"] is not None]
    itl = [v for r in rows for v in r["itl_ms"]]
    ok_p = lib_stats.percentiles(ok_lat, [50, 90, 95, 99])
    bad_p = lib_stats.percentiles(bad_lat, [50, 90, 95, 99])
    ttft_p = lib_stats.percentiles(ttft, [50, 90, 95, 99])
    status_counts: dict[str, int] = {}
    for r in rows:
        key = str(r["status"])
        status_counts[key] = status_counts.get(key, 0) + 1
    return {
        "requests": len(rows),
        "succeeded": len(ok),
        "failed": len(bad),
        "success_rate": (len(ok) / len(rows)) if rows else 0.0,
        "failure_rate": (len(bad) / len(rows)) if rows else 0.0,
        "elapsed_s": elapsed_s,
        "rps": (len(rows) / elapsed_s) if elapsed_s > 0 else 0.0,
        "success_p50_ms": ok_p["p50"],
        "success_p90_ms": ok_p["p90"],
        "success_p95_ms": ok_p["p95"],
        "success_p99_ms": ok_p["p99"],
        "success_max_ms": ok_p["max"],
        "failed_p50_ms": bad_p["p50"],
        "failed_p95_ms": bad_p["p95"],
        "failed_p99_ms": bad_p["p99"],
        "ttft_p50_ms": ttft_p["p50"],
        "ttft_p95_ms": ttft_p["p95"],
        "ttft_p99_ms": ttft_p["p99"],
        "ttft_samples": len(ttft),
        "itl_mean_ms": (sum(itl) / len(itl)) if itl else None,
        "itl_samples": len(itl),
        "status_counts": status_counts,
    }


def build_route_infos(items: list[dict[str, Any]]) -> list[RouteInfo]:
    """Label the two enabled routes A/B by ascending channel id."""
    enabled = [i for i in items if i.get("enabled")]
    enabled.sort(key=lambda i: (i.get("channel_id", 0), i.get("key_index", 0)))
    labels = ["A", "B"]
    routes: list[RouteInfo] = []
    for label, item in zip(labels, enabled):
        routes.append(
            RouteInfo(
                label=label,
                channel_id=int(item.get("channel_id", 0)),
                key_index=int(item.get("key_index", 0)),
                upstream_model=str(item.get("upstream_model", "")),
                static_weight=int(item.get("static_weight", 0)),
                base_weight=float(item.get("base_weight", 0.0)),
                ewma_quality=float(item.get("ewma_quality", 0.0)),
                health_multiplier=float(item.get("health_multiplier", 0.0)),
                share_correction=float(item.get("share_correction", 0.0)),
                final_score=float(item.get("final_score", 0.0)),
                sample_count=int(item.get("sample_count", 0)),
                raw=item,
            )
        )
    return routes


def traffic_windows(rows: list[dict[str, Any]], resource_windows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    """Merge per-window traffic counters onto the resource windows."""
    merged: list[dict[str, Any]] = []
    for w in resource_windows:
        start = w.get("window_start")
        end = w.get("window_end")
        merged.append(
            {
                "window_start": start,
                "window_end": end,
                "arrivals": "",
                "completions": "",
                "rps": "",
                "success": "",
                "failed": "",
                "p50": "",
                "p95": "",
                "p99": "",
                "cpu_user": w.get("cpu_user_avg", w.get("cpu_user_max", "")),
                "rss": w.get("mem_rss_max", ""),
                "disk_write_bps": w.get("disk_write_bps_max", ""),
                "net_err": w.get("net_error_max", ""),
                "samples": w.get("samples", 0),
            }
        )
    return merged


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--scenario", required=True, choices=["S1", "S2", "S3"])
    p.add_argument("--gateway-url", required=True, help="Gateway base URL without /v1 suffix")
    p.add_argument("--token", required=True, help="sk- API key used for relay traffic")
    p.add_argument("--admin-token", required=True, help="Admin JWT for topology/audit/option reads")
    p.add_argument("--admin-username", help="Admin username for auto token refresh on 401/403")
    p.add_argument("--admin-password", help="Admin password for auto token refresh on 401/403")
    p.add_argument("--alias", required=True, help="Public model alias under test")
    p.add_argument("--mock-url", required=True, help="Mock upstream base URL (healthz probe)")
    p.add_argument("--mock-file", type=Path, help="Mock upstream ndjson path")
    p.add_argument("--warmup", type=int, default=44, help="Warmup requests, excluded from share fitting")
    p.add_argument("--requests", type=int, required=True, help="Stat-phase request count")
    p.add_argument("--concurrency", type=int, default=8)
    p.add_argument("--stream-ratio", type=float, default=None, help="Override scenario default stream ratio")
    p.add_argument("--out-dir", type=Path, default=Path("runtime/scenario"))
    p.add_argument("--max-seconds", type=float, default=None, help="Advisory stat-phase budget, recorded in summary")
    args = p.parse_args()

    token_mgr = AdminTokenManager(
        initial_token=args.admin_token,
        gateway_url=args.gateway_url,
        username=args.admin_username,
        password=args.admin_password,
    )

    targets = lib_stats.scenario_targets()[args.scenario]
    stream_ratio = args.stream_ratio if args.stream_ratio is not None else (0.2 if args.scenario == "S1" else 1.0)
    out = args.out_dir
    out.mkdir(parents=True, exist_ok=True)
    mock_file = args.mock_file or Path("/tmp/mock_upstream.ndjson")

    summary: dict[str, Any] = {
        "scenario": args.scenario,
        "verdict": "ENVIRONMENT_INVALID",
        "config": {
            "gateway_url": args.gateway_url,
            "alias": args.alias,
            "warmup": args.warmup,
            "requests": args.requests,
            "concurrency": args.concurrency,
            "stream_ratio": stream_ratio,
            "max_seconds": args.max_seconds,
            "target": targets["target"],
            "tol_pp": targets["tol_pp"],
            "ci_bounds": list(targets["ci_bounds"]),
            "subject": targets["subject"],
            "min_samples": targets["min_samples"],
            "injection": targets["injection"],
        },
    }

    def fail(reason: str, verdict: str = "ENVIRONMENT_INVALID", code: int = 2) -> int:
        summary["verdict"] = verdict
        summary["environment_error"] = reason
        summary["admin_token_refreshes"] = token_mgr.refresh_count
        lib_report.write_summary(out / "summary.json", summary)
        (out / "report.md").write_text(lib_report.render_report_md(summary), encoding="utf-8")
        print(f"ERROR: {reason}", file=sys.stderr)
        print(f"      Final verdict: {verdict}")
        return code

    print(f"[1/8] Preflight: {args.gateway_url}")
    try:
        status = requests.get(f"{args.gateway_url.rstrip('/')}/api/status", timeout=10)
        if status.status_code != 200:
            return fail(f"gateway /api/status returned {status.status_code}")
    except Exception as exc:
        return fail(f"gateway unreachable: {exc}")
    try:
        health = requests.get(f"{args.mock_url.rstrip('/')}/healthz", timeout=10)
        if health.status_code != 200:
            return fail(f"mock /healthz returned {health.status_code}")
    except Exception as exc:
        return fail(f"mock upstream unreachable: {exc}")
    options = fetch_options(args.gateway_url, token_mgr)
    if options is None:
        return fail(
            "cannot read GET /api/option/ (admin token invalid or endpoint changed). "
            "可能是 admin JWT 过期，长跑请传 --admin-username/--admin-password 启用自动刷新"
        )
    affinity_on = options.get(AFFINITY_OPTION_KEY, "false").strip().lower() == "true"
    if affinity_on and not args.allow_affinity:
        return fail(
            "channel affinity is enabled: sticky routing bypasses weighted selection and "
            "invalidates share measurement. Disable it before running share scenarios "
            "(admin console, or PUT /api/option/ with "
            '{"key":"channel_affinity_setting.enabled","value":"false"}), '
            "or pass --allow-affinity to record the run as advisory only."
        )
    if affinity_on:
        summary["affinity_enabled_warning"] = (
            "channel_affinity_setting.enabled=true during this run; sticky routing biases the "
            "measured share, so the verdict is advisory and not a scheduler conclusion."
        )
        print("      WARNING: affinity enabled, continuing because --allow-affinity was passed")
    else:
        print("      OK (affinity disabled)")

    print(f"[3/8] Topology for alias={args.alias}")
    items = fetch_topology(args.gateway_url, token_mgr, args.alias)
    if items is None:
        return fail("route-unit topology response shape unexpected (missing data.items)")
    routes = build_route_infos(items)
    if len(routes) != 2:
        return fail(f"expected exactly 2 enabled route units, got {len([i for i in items if i.get('enabled')])}")
    (out / "route-before.json").write_text(json.dumps(items, indent=2, ensure_ascii=False), encoding="utf-8")
    for r in routes:
        print(f"      Route {r.label}: channel_id={r.channel_id} key_index={r.key_index} model={r.upstream_model}")

    injection = targets["injection"]
    mode = injection[targets["subject"]] if len(set(injection.values())) == 1 else injection["A"]
    if len(set(injection.values())) > 1:
        summary["injection_limitation"] = (
            f"scenario asks for per-route modes {injection}, but both routes share one mock endpoint and "
            "the mode is chosen by the client header, so per-route latency skew cannot be injected from "
            f"the client. This run used '{mode}' for every request; to inject a real skew give A and B "
            "different upstream base_url or upstream_model so each maps to its own mock behaviour."
        )
        print(f"      NOTE: per-route injection unavailable, using mode={mode} for all requests")

    print(f"[4/8] Warmup: {args.warmup} requests (excluded from share fitting)")
    warmup_rows, _ = run_phase(
        args.gateway_url, args.token, args.alias, mode, args.warmup, args.concurrency, stream_ratio, "warmup"
    )
    write_ndjson(out / "warmup.ndjson", warmup_rows)
    print(f"      Done, {sum(1 for r in warmup_rows if r['status'] == 200)}/{len(warmup_rows)} succeeded")

    print("[5/8] Resource sampling + stats phase")
    sampler = lib_resources.ResourceSampler(interval=1.0, out_path=out / "resources.ndjson")
    sampler.start()
    phase_started = time.time()
    stat_rows, stat_ids = run_phase(
        args.gateway_url, args.token, args.alias, mode, args.requests, args.concurrency, stream_ratio, "stats"
    )
    elapsed = time.time() - phase_started
    sampler.stop()
    write_ndjson(out / "requests.ndjson", stat_rows)
    print(f"      Done in {elapsed:.1f}s, {sum(1 for r in stat_rows if r['status'] == 200)}/{len(stat_rows)} succeeded")

    print("[6/8] Post-run snapshots")
    items_after = fetch_topology(args.gateway_url, token_mgr, args.alias)
    if items_after is not None:
        (out / "route-after.json").write_text(json.dumps(items_after, indent=2, ensure_ascii=False), encoding="utf-8")
    routes_after = build_route_infos(items_after) if items_after else routes
    stat_id_set = set(stat_ids)
    # The relay records its attempt after the response body is flushed, so the
    # last few requests can still be in flight when the client returns. Poll
    # until the audit ring carries every stat-phase id, or the budget expires.
    settle_deadline = time.time() + 15.0
    attempts_all: list[dict[str, Any]] | None = None
    while True:
        attempts_all = fetch_audit(args.gateway_url, token_mgr)
        if attempts_all is None:
            return fail("cannot read GET /api/route_unit/audit. 可能是 admin JWT 过期，长跑请传 --admin-username/--admin-password 启用自动刷新")
        seen = {a.get("client_request_id") for a in attempts_all}
        if stat_id_set <= seen or time.time() >= settle_deadline:
            break
        time.sleep(0.5)
    attempts = [a for a in attempts_all if a.get("client_request_id") in stat_id_set]
    write_ndjson(out / "gateway-attempts.ndjson", attempts)
    upstream_rows = read_ndjson(mock_file)
    write_ndjson(out / "upstream-received.ndjson", upstream_rows)
    print(f"      audit total={len(attempts_all)} scoped={len(attempts)} upstream_rows={len(upstream_rows)}")

    audited_ids = {a.get("client_request_id") for a in attempts}
    missing_ids = stat_id_set - audited_ids
    ring_overflow = bool(missing_ids) and len(attempts_all) >= AUDIT_RING_CAPACITY
    if ring_overflow:
        summary["audit_ring_overflow"] = True
        summary["audit_ring_note"] = (
            f"audit ring returned {len(attempts_all)} records (capacity {AUDIT_RING_CAPACITY}) and "
            f"{len(missing_ids)} stat-phase requests are missing: the ring overflowed. Raise the audit "
            "ring capacity or lower --requests."
        )

    print("[7/8] Reconciling")
    rec = lib_reconcile.reconcile(
        attempts, upstream_rows, expected_requests=args.requests, expected_request_ids=stat_id_set
    )
    summary["reconcile"] = rec.to_summary()
    print(f"      Reconcile verdict: {rec.verdict}")

    print("[8/8] Share evaluation")
    opportunities = len(attempts)
    per_route: dict[tuple, int] = {}
    for a in attempts:
        key = (a.get("channel_id"), a.get("key_index"), a.get("upstream_model"))
        per_route[key] = per_route.get(key, 0) + 1
    after_by_identity = {r.identity(): r for r in routes_after}

    share_rows: list[dict[str, Any]] = []
    share_eval: dict[str, dict[str, Any]] = {}
    for r in routes:
        snap = after_by_identity.get(r.identity(), r)
        selections = per_route.get(r.identity(), 0)
        stats = lib_stats.share_stats(selections, opportunities)
        is_subject = r.label == targets["subject"]
        row = {
            "route": r.label,
            "channel_id": r.channel_id,
            "key_index": r.key_index,
            "upstream_model": r.upstream_model,
            "selections": selections,
            "attempts": opportunities,
            "observed_share": stats["point"],
            "ci_low": stats["ci_low"],
            "ci_high": stats["ci_high"],
            "target": targets["target"] if is_subject else None,
            "tol_pp": targets["tol_pp"] if is_subject else None,
            "base_weight": snap.base_weight,
            "ewma_quality": snap.ewma_quality,
            "health_multiplier": snap.health_multiplier,
            "share_correction": snap.share_correction,
            "final_score": snap.final_score,
            "sample_count": snap.sample_count,
        }
        share_rows.append(row)
        if is_subject:
            share_eval[r.label] = lib_stats.evaluate_share(
                stats["point"],
                (stats["ci_low"], stats["ci_high"]),
                targets["target"],
                targets["tol_pp"],
                targets["ci_bounds"],
            )
        else:
            share_eval[r.label] = {"ok": True, "reasons": []}
    summary["shares"] = share_rows
    summary["share_eval"] = share_eval

    summary["service_quality"] = service_quality(stat_rows, elapsed)
    resource_rows = read_ndjson(out / "resources.ndjson")
    windows = lib_resources.aggregate_windows(resource_rows, window_s=10)
    summary["resources"] = {
        "samples": len(resource_rows),
        "windows": len(windows),
        "peak_cpu_user": max((w.get("cpu_user_max", 0) or 0 for w in windows), default=None),
        "peak_rss": max((w.get("mem_rss_max", 0) or 0 for w in windows), default=None),
        "peak_disk_write_bps": max((w.get("disk_write_bps_max", 0) or 0 for w in windows), default=None),
        "peak_net_error": max((w.get("net_error_max", 0) or 0 for w in windows), default=None),
    }

    lib_report.write_shares_csv(out / "shares.csv", share_rows)
    lib_report.write_windows_csv(out / "windows.csv", traffic_windows(stat_rows, windows))
    lib_report.write_resources_csv(out / "resources.csv", resource_rows)

    subject_ok = share_eval[targets["subject"]]["ok"]
    effective = summary["service_quality"]["succeeded"]
    if rec.verdict != "PASS" or effective < args.requests:
        verdict, code = "DATA_INVALID", 1
    elif args.requests < targets["min_samples"] and not subject_ok:
        verdict, code = "UNDERPOWERED", 1
        summary["sample_size_warning"] = (
            f"--requests={args.requests} is below the feasible floor n={targets['min_samples']} for the "
            f"±{targets['tol_pp'] * 100:.0f}pp CI criterion, so the CI cannot be tight enough to pass. "
            "Treat this run as structural validation, not a scheduler verdict."
        )
    elif not subject_ok:
        verdict, code = "PRODUCT_FAIL", 1
    else:
        verdict, code = "PASS", 0
    summary["verdict"] = verdict
    summary["admin_token_refreshes"] = token_mgr.refresh_count

    lib_report.write_summary(out / "summary.json", summary)
    (out / "report.md").write_text(lib_report.render_report_md(summary), encoding="utf-8")

    subject_row = next(r for r in share_rows if r["route"] == targets["subject"])
    print(
        f"      Subject {targets['subject']}: share={subject_row['observed_share']:.4f} "
        f"CI=[{subject_row['ci_low']:.4f},{subject_row['ci_high']:.4f}] target={targets['target']:.3f}"
    )
    print(f"      Share eval: {share_eval}")
    print(f"      Final verdict: {verdict}")
    print(f"      Summary: {out / 'summary.json'}")
    print(f"      Report: {out / 'report.md'}")
    return code


if __name__ == "__main__":
    sys.exit(main())
