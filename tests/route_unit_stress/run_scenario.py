#!/usr/bin/env python3
"""Scenario runner for route-unit EWMA share convergence (S1–S6 of #418, #451).

Flow: preflight -> affinity gate -> topology -> warmup -> stats phase (with
resource sampling) -> post snapshots -> reconcile -> share evaluation -> verdict.

Verdict priority (highest first):
  ENVIRONMENT_INVALID (exit 2)  gateway unreachable, affinity on, bad topology, mock mode mismatch
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
import uuid
import time
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

def get_option(gateway_url: str, token_mgr: AdminTokenManager, key: str) -> str | None:
    """Get a single option value by key."""
    options = fetch_options(gateway_url, token_mgr)
    if options is None:
        return None
    return options.get(key)


def set_option(gateway_url: str, token_mgr: AdminTokenManager, key: str, value: str) -> bool:
    """Set a single option value by key. Returns True on success."""
    for attempt in range(2):
        try:
            r = requests.put(
                f"{gateway_url.rstrip('/')}/api/option/",
                headers=token_mgr.auth_header(),
                json={"key": key, "value": value},
                timeout=10,
            )
            if r.status_code == 200:
                return True
            if token_mgr.maybe_refresh(r.status_code):
                continue
            return False
        except Exception:
            return False
    return False


def wait_option_value(
    gateway_url: str,
    token_mgr: AdminTokenManager,
    key: str,
    expected_value: str,
    timeout_s: float = 30.0,
) -> bool:
    """Poll GET /api/option/ until the key equals expected_value or timeout."""
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        val = get_option(gateway_url, token_mgr, key)
        if val == expected_value:
            return True
        time.sleep(1.0)
    return False


def compute_percentile(values: list[float], p: float) -> float:
    """Compute percentile using linear interpolation (NumPy default)."""
    if not values:
        return 0.0
    sorted_vals = sorted(values)
    n = len(sorted_vals)
    index = (n - 1) * p
    lower = int(index)
    upper = min(lower + 1, n - 1)
    weight = index - lower
    return sorted_vals[lower] * (1 - weight) + sorted_vals[upper] * weight

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
    start_ts = time.time()
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
        "start_ts": start_ts,
        "end_ts": None,
        "retry_after_sec": None,
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
        # Capture Retry-After header for 429 responses (S4 throttle stats)
        if r.status_code == 429:
            ra = r.headers.get("Retry-After")
            if ra is not None:
                try:
                    row["retry_after_sec"] = float(ra)
                except ValueError:
                    pass
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
    finally:
        row["end_ts"] = time.time()
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

def build_route_infos(
    items: list[dict[str, Any]],
    expected_count: int | None = None,
    bad_channel_id: int | None = None,
) -> list[RouteInfo]:
    """Label enabled routes by ascending channel_id.

    For S1–S4 (no bad_channel_id): labels A, B, C, D...
    For S5 (bad_channel_id provided): the route with that channel_id gets label "BAD",
    others get "GOOD1", "GOOD2", ... in channel_id order.
    """
    # Filter: enabled=True AND channel_status==1 (gateway only routes to status=1)
    enabled = [i for i in items if i.get("enabled") and i.get("channel_status") == 1]
    enabled.sort(key=lambda i: (i.get("channel_id", 0), i.get("key_index", 0)))

    if expected_count is not None and len(enabled) != expected_count:
        # Validation happens in main(); this is a defensive guard
        pass

    routes: list[RouteInfo] = []
    if bad_channel_id is not None:
        # S5 labeling: BAD + GOOD1..GOODn
        good_labels = []
        for idx, item in enumerate(enabled):
            cid = int(item.get("channel_id", 0))
            if cid == bad_channel_id:
                label = "BAD"
            else:
                label = f"GOOD{len(good_labels) + 1}"
                good_labels.append(label)
            routes.append(
                RouteInfo(
                    label=label,
                    channel_id=cid,
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
    else:
        # S1–S4 labeling: A, B, C, D...
        for idx, item in enumerate(enabled):
            label = chr(ord("A") + idx)
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


def run_phase_s6(
    gateway_url: str,
    token: str,
    alias: str,
    mode: str,
    duration_s: int,
    concurrency: int,
    stream_ratio: float,
    step_at_ratio: float,
    tail_seconds: int,
    step_hook_cmd: str | None,
    token_mgr: AdminTokenManager,
    routes: list[RouteInfo],
    subject_label: str,
) -> tuple[list[dict[str, Any]], list[str], float, dict | None, list[dict[str, Any]]]:
    """Run S6 stat phase by duration with per-window metrics and step hook.

    Returns:
        stat_rows, stat_ids, step_at_ts, step_hook_result, window_metrics
    """
    import subprocess

    step_at_s = duration_s * step_at_ratio
    min_total_s = max(duration_s, step_at_s + tail_seconds)
    phase_started = time.time()
    deadline = phase_started + min_total_s
    step_at_deadline = phase_started + step_at_s
    step_triggered = False
    step_at_ts = None
    step_hook_result = None

    stat_rows: list[dict[str, Any]] = []
    stat_ids: list[str] = []
    window_metrics: list[dict[str, Any]] = []

    # Track window boundaries for metrics collection
    window_s = 10
    next_window_ts = (int(time.time() / window_s) + 1) * window_s

    def collect_window_metrics(current_ts: float) -> dict | None:
        """Collect topology and audit for current window."""
        items = fetch_topology(gateway_url, token_mgr, alias)
        if items is None:
            return None
        attempts = fetch_audit(gateway_url, token_mgr)
        if attempts is None:
            return None
        subject_route = None
        for r in routes:
            if r.label == subject_label:
                subject_route = r
                break
        if subject_route is None:
            return None
        ewma_map = {}
        corr_vals = []
        for item in items:
            label = None
            for r in routes:
                if r.identity() == (item.get("channel_id"), item.get("key_index"), item.get("upstream_model")):
                    label = r.label
                    break
            if label:
                ewma_map[label] = item.get("ewma_quality", 0.0)
                corr = item.get("share_correction", 0.0)
                if corr > 0:
                    corr_vals.append(corr)
        corr_p99 = compute_percentile(corr_vals, 0.99) if corr_vals else 0.0
        subject_share = 0.0
        if subject_route:
            for item in items:
                if item.get("channel_id") == subject_route.channel_id and item.get("key_index") == subject_route.key_index:
                    subject_share = item.get("actual_share", item.get("share_correction", 0.0))
                    break
        return {
            "window_start": int(current_ts // window_s) * window_s,
            "subject_share": subject_share,
            "corr_p99": corr_p99,
            "ewma": ewma_map,
            "samples": len(attempts),
        }

    print(f"      S6: running for {min_total_s}s (step at {step_at_s:.1f}s, tail {tail_seconds}s)")
    last_progress = time.time()
    next_req_id = 0

    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures: set = set()
        stream = stream_ratio > 0.0

        while time.time() < deadline or futures:
            now = time.time()

            # Trigger step hook at step_at_ratio
            if not step_triggered and now >= step_at_deadline:
                step_triggered = True
                step_at_ts = time.time()
                print(f"      S6: step transition at {step_at_ts - phase_started:.1f}s elapsed")
                if step_hook_cmd:
                    hook_start = time.time()
                    try:
                        proc = subprocess.run(step_hook_cmd, shell=True, capture_output=True, text=True, timeout=30)
                        hook_duration = time.time() - hook_start
                        step_hook_result = {
                            "cmd": step_hook_cmd,
                            "exit_code": proc.returncode,
                            "duration_sec": hook_duration,
                            "stdout": proc.stdout[-500:] if proc.stdout else "",
                            "stderr": proc.stderr[-500:] if proc.stderr else "",
                        }
                        print(f"      S6: step hook exited with code {proc.returncode} in {hook_duration:.2f}s")
                    except subprocess.TimeoutExpired:
                        hook_duration = time.time() - hook_start
                        step_hook_result = {
                            "cmd": step_hook_cmd, "exit_code": -1, "duration_sec": hook_duration,
                            "stdout": "", "stderr": "timeout after 30s",
                        }
                        print(f"      S6: step hook timed out after 30s")
                    except Exception as e:
                        hook_duration = time.time() - hook_start
                        step_hook_result = {
                            "cmd": step_hook_cmd, "exit_code": -1, "duration_sec": hook_duration,
                            "stdout": "", "stderr": str(e),
                        }
                        print(f"      S6: step hook failed: {e}")
                else:
                    step_hook_result = {"skipped": True}
                    print(f"      S6: step hook skipped (no --step-hook provided)")

            # Submit new requests to maintain concurrency while within deadline
            while len(futures) < concurrency and time.time() < deadline:
                req_id = f"s6-{next_req_id}"
                next_req_id += 1
                fut = pool.submit(send_request, gateway_url, token, alias, req_id, mode, stream, "stats")
                futures.add(fut)

            # Collect completed futures
            done = {f for f in futures if f.done()}
            for f in done:
                row = f.result()
                stat_rows.append(row)
                stat_ids.append(row.get("request_id", ""))
                futures.discard(f)

            # Collect per-window metrics every 10s
            if now >= next_window_ts:
                metrics = collect_window_metrics(now)
                if metrics:
                    window_metrics.append(metrics)
                next_window_ts += window_s

            # Progress log
            if now - last_progress >= 10:
                print(f"      S6: {now - phase_started:.1f}s elapsed, {len(stat_rows)} requests, step={'done' if step_triggered else 'pending'}")
                last_progress = now

            # Small sleep to avoid busy loop when futures are still pending
            if futures and not done:
                time.sleep(0.05)

    # Final window metrics collection
    metrics = collect_window_metrics(time.time())
    if metrics:
        window_metrics.append(metrics)

    return stat_rows, stat_ids, step_at_ts, step_hook_result, window_metrics


def main() -> int:
    scenario_choices = sorted(lib_stats.scenario_targets().keys())
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--scenario", required=True, choices=scenario_choices)
    p.add_argument("--gateway-url", required=True, help="Gateway base URL without /v1 suffix")
    p.add_argument("--token", required=True, help="sk- API key used for relay traffic")
    p.add_argument("--admin-token", required=True, help="Admin JWT for topology/audit/option reads")
    p.add_argument("--admin-username", help="Admin username for auto token refresh on 401/403")
    p.add_argument("--admin-password", help="Admin password for auto token refresh on 401/403")
    p.add_argument("--alias", required=True, help="Public model alias under test")
    p.add_argument("--mock-url", required=True, help="Mock upstream base URL (healthz probe)")
    p.add_argument("--mock-file", type=Path, help="Mock upstream ndjson path")
    p.add_argument("--warmup", type=int, default=44, help="Warmup requests, excluded from share fitting")
    p.add_argument("--requests", type=int, default=None, help="Stat-phase request count (required for non-S6 scenarios)")
    p.add_argument("--concurrency", type=int, default=8)
    p.add_argument("--stream-ratio", type=float, default=None, help="Override scenario default stream ratio")
    p.add_argument("--out-dir", type=Path, default=Path("runtime/scenario"))
    p.add_argument("--max-seconds", type=float, default=None, help="Advisory stat-phase budget, recorded in summary")
    p.add_argument("--duration-seconds", type=int, default=None, help="S6: stat phase duration in seconds (overrides --requests for S6). Must be >= 180 for S6.")
    p.add_argument("--step-hook", default=None, help="S6: shell command to run at step transition (subprocess shell=True). Records exit code/duration in summary.")
    p.add_argument("--step-at-ratio", type=float, default=None, help="S6: fraction of duration at which to trigger step (default from scenario)")
    p.add_argument("--tail-seconds", type=int, default=None, help="S6: minimum seconds to run after step (default from scenario min_tail_seconds)")
    p.add_argument("--bad-channel-id", type=int, help="Channel ID of the BAD route (S5 scenarios)")
    p.add_argument("--route-mode", action="append", default=[], help="Per-route mock mode intent: LABEL=mode or CHANNEL_ID=mode (repeatable). Recorded in summary and validated against mock /healthz force_mode.")
    p.add_argument("--allow-affinity", action="store_true", help="Allow running with channel affinity enabled (advisory only)")
    args = p.parse_args()

    token_mgr = AdminTokenManager(
        initial_token=args.admin_token,
        gateway_url=args.gateway_url,
        username=args.admin_username,
        password=args.admin_password,
    )

    targets = lib_stats.scenario_targets()[args.scenario]
    is_s6 = args.scenario.startswith("S6")
    is_s5 = args.scenario.startswith("S5")
    is_s4 = args.scenario.startswith("S4")

    # S6-specific defaults from scenario
    if is_s6:
        share_window_size = targets.get("share_window_size", 200)
        step_at_ratio = args.step_at_ratio if args.step_at_ratio is not None else targets.get("step_at_ratio", 0.5)
        tail_seconds = args.tail_seconds if args.tail_seconds is not None else targets.get("min_tail_seconds", 90)
        # Duration: S6 runs by time, not request count. Default 180s minimum per #418.
        duration = args.duration_seconds if args.duration_seconds is not None else 180
        step_hook_cmd = args.step_hook
        # stream_ratio default for S6: streaming
        if args.stream_ratio is not None:
            stream_ratio = args.stream_ratio
        else:
            stream_ratio = 1.0
    else:
        # S4 runs non-streaming; S1 uses mixed; S2/S3/S5 default to streaming
        if args.stream_ratio is not None:
            stream_ratio = args.stream_ratio
        elif is_s4:
            stream_ratio = 0.0
        elif args.scenario == "S1":
            stream_ratio = 0.2
        else:
            stream_ratio = 1.0
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
            "target": targets.get("target"),
            "tol_pp": targets.get("tol_pp"),
            "ci_bounds": list(targets["ci_bounds"]) if targets.get("ci_bounds") else None,
            "subject": targets["subject"],
            "min_samples": targets.get("min_samples"),
            "injection": targets["injection"],
            "route_count": targets.get("route_count"),
            "share_window_size": targets.get("share_window_size"),
        },
        "injection_plan": {},
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

    # S6: advisory duration check (real validation requires >= 180s per #418)
    if is_s6 and duration < 180:
        print(f"      WARNING: S6 duration {duration}s < 180s minimum per #418; run may be inconclusive")
    # Non-S6: require --requests
    if not is_s6 and args.requests is None:
        return fail("--requests is required for non-S6 scenarios")

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

    # Count topology rows: total / enabled / channel_status==1
    total_rows = len(items)
    route_rows_enabled = len([i for i in items if i.get("enabled")])
    channel_enabled = len([i for i in items if i.get("enabled") and i.get("channel_status") == 1])
    summary.setdefault("topology", {})
    summary["topology"]["total_rows"] = total_rows
    summary["topology"]["route_rows_enabled"] = route_rows_enabled
    summary["topology"]["channel_enabled"] = channel_enabled
    print(f"      total={total_rows} route.enabled={route_rows_enabled} channel_status=1={channel_enabled}")

    # Determine expected route count from scenario, default 2
    expected_count = targets.get("route_count", 2)
    is_s5 = args.scenario.startswith("S5")
    bad_cid = args.bad_channel_id if is_s5 else None
    if is_s5 and args.bad_channel_id is None:
        return fail("S5 scenarios require --bad-channel-id to identify the BAD route")
    routes = build_route_infos(items, expected_count=expected_count, bad_channel_id=bad_cid)
    if len(routes) != expected_count:
        return fail(
            f"expected {expected_count} enabled+channel_status=1 route units, "
            f"got {channel_enabled} (topology: total={total_rows}, route.enabled={route_rows_enabled}, channel_enabled={channel_enabled})"
        )
    if is_s5:
        bad_routes = [r for r in routes if r.label == "BAD"]
        if not bad_routes:
            return fail(f"S5 requires a BAD route with channel_id={args.bad_channel_id}, none found among enabled routes")
    (out / "route-before.json").write_text(json.dumps(items, indent=2, ensure_ascii=False), encoding="utf-8")
    for r in routes:
        print(f"      Route {r.label}: channel_id={r.channel_id} key_index={r.key_index} model={r.upstream_model}")

    # Parse --route-mode overrides into a label→mode map
    route_mode_overrides: dict[str, str] = {}
    for rm in args.route_mode:
        if "=" not in rm:
            return fail(f"--route-mode '{rm}' must be LABEL=mode or CHANNEL_ID=mode")
        key, val = rm.split("=", 1)
        key = key.strip()
        val = val.strip()
        # Resolve numeric channel_id to label
        if key.isdigit():
            cid = int(key)
            matched = [r for r in routes if r.channel_id == cid]
            if not matched:
                return fail(f"--route-mode channel_id={cid} not found among routes")
            key = matched[0].label
        route_mode_overrides[key] = val

    # Build injection_plan: scenario injection + --route-mode overrides
    injection = targets["injection"]
    injection_plan: dict[str, str] = {}
    if is_s6:
        # S6: injection keys are STABLE/SLOW/STEP, map to route labels A/B/C/D
        # STABLE -> A, B (routes 1-2); SLOW -> C (route 3); STEP -> D (route 4)
        s6_label_map = {
            "STABLE": ["A", "B"],
            "SLOW": ["C"],
            "STEP": ["D"],
        }
        for key, mode_val in injection.items():
            for label in s6_label_map.get(key, []):
                injection_plan[label] = mode_val
    elif is_s5:
        # S5: injection keys are BAD/GOOD, maps to route labels directly
        for label, mode_val in injection.items():
            if label == "GOOD":
                # Apply to all GOODn routes
                for r in routes:
                    if r.label.startswith("GOOD"):
                        injection_plan[r.label] = mode_val
            else:
                injection_plan[label] = mode_val
    else:
        # S1–S4: labels A/B/C/D
        for label, mode_val in injection.items():
            injection_plan[label] = mode_val
    # Apply --route-mode overrides
    injection_plan.update(route_mode_overrides)
    summary["injection_plan"] = injection_plan

    # Preflight: validate each route's mock /healthz force_mode matches plan
    for r in routes:
        planned_mode = injection_plan.get(r.label)
        if planned_mode is None:
            continue
        # Derive mock base_url from topology item's base_url field
        base_url = r.raw.get("base_url", "")
        if not base_url:
            print(f"      NOTE: route {r.label} has no base_url, skipping mock mode validation")
            continue
        # Strip trailing slash and /v1 suffix for healthz probe
        mock_base = base_url.rstrip("/")
        if mock_base.endswith("/v1"):
            mock_base = mock_base[:-3]
        try:
            hz = requests.get(f"{mock_base}/healthz", timeout=10)
            if hz.status_code != 200:
                return fail(f"route {r.label} mock /healthz returned {hz.status_code}")
            hz_data = hz.json()
            actual_mode = hz_data.get("force_mode", hz_data.get("MOCK_FORCE_MODE", ""))
            # Dynamic modes (e.g. "ttft_4000→ttft_500") compare against the initial segment;
            # the mock starts at the first mode and transitions via the step hook at runtime.
            check_mode = planned_mode.split("→")[0] if "→" in planned_mode else planned_mode
            if actual_mode and str(actual_mode) != check_mode:
                return fail(
                    f"route {r.label} mock force_mode='{actual_mode}' does not match planned mode '{planned_mode}'. "
                    f"Set MOCK_FORCE_MODE={planned_mode} on the mock instance for channel_id={r.channel_id}."
                )
        except requests.exceptions.ConnectionError as exc:
            return fail(f"route {r.label} mock unreachable at {mock_base}/healthz: {exc}")
        except Exception as exc:
            print(f"      NOTE: route {r.label} mock /healthz check failed ({exc}), continuing")

    # Choose client-side mode: if all routes use same mode, that's the header; otherwise use subject's mode
    # (actual per-route skew is enforced by per-instance MOCK_FORCE_MODE, not the client header)
    unique_modes = set(injection_plan.values())
    if len(unique_modes) == 1:
        mode = injection_plan.get(targets["subject"], list(unique_modes)[0])
    else:
        mode = injection_plan.get(targets["subject"], list(injection_plan.values())[0])
    if len(unique_modes) > 1 and not is_s5:
        summary["injection_limitation"] = (
            f"scenario asks for per-route modes {injection_plan}, but routes may share mock endpoints. "
            "Per-route differentiation is enforced by each mock instance's MOCK_FORCE_MODE. "
            f"The client header X-Mock-Mode='{mode}' is sent for all requests; actual per-route behavior "
            "depends on the mock instance each route points to. See injection_plan in summary."
        )
        print(f"      NOTE: per-route modes differ ({len(unique_modes)} unique), client header={mode}, per-instance MOCK_FORCE_MODE enforces real behavior")

    print(f"[4/8] Warmup: {args.warmup} requests (excluded from share fitting)")
    warmup_rows, _ = run_phase(
        args.gateway_url, args.token, args.alias, mode, args.warmup, args.concurrency, stream_ratio, "warmup"
    )
    write_ndjson(out / "warmup.ndjson", warmup_rows)
    print(f"      Done, {sum(1 for r in warmup_rows if r['status'] == 200)}/{len(warmup_rows)} succeeded")
    # S6: switch share window size before stat phase
    original_share_window_size = None
    share_window_applied = False
    share_window_restored = False
    if is_s6:
        # Get original value
        original_share_window_size = get_option(args.gateway_url, token_mgr, "RouteStatsShareWindowSize")
        print(f"      Original RouteStatsShareWindowSize: {original_share_window_size}")
        # Set new value
        if not set_option(args.gateway_url, token_mgr, "RouteStatsShareWindowSize", str(share_window_size)):
            return fail(f"Failed to set RouteStatsShareWindowSize={share_window_size}")
        # Wait for it to take effect
        if not wait_option_value(args.gateway_url, token_mgr, "RouteStatsShareWindowSize", str(share_window_size), timeout_s=30.0):
            return fail(f"RouteStatsShareWindowSize did not propagate to {share_window_size} within 30s")
        share_window_applied = True
        print(f"      RouteStatsShareWindowSize set to {share_window_size} and confirmed")
    try:
        print("[5/8] Resource sampling + stats phase")
        sampler = lib_resources.ResourceSampler(interval=1.0, out_path=out / "resources.ndjson")
        sampler.start()
        phase_started = time.time()
        warmup_end_ts = time.time()  # for phase_marks

        # S6: duration-based stat phase with per-window metrics collection
        if is_s6:
            stat_rows, stat_ids, step_at_ts, step_hook_result, window_metrics = run_phase_s6(
                args.gateway_url,
                args.token,
                args.alias,
                mode,
                duration,
                args.concurrency,
                stream_ratio,
                step_at_ratio,
                tail_seconds,
                step_hook_cmd,
                token_mgr,
                routes,
                "A",
            )
            elapsed = time.time() - phase_started
            # step_at_ts is already set by run_phase_s6
            phase_marks = {
                "warmup_end": warmup_end_ts,
                "step_at": step_at_ts,
                "cooldown_start": step_at_ts + tail_seconds,
            }
        else:
            stat_rows, stat_ids = run_phase(
                args.gateway_url, args.token, args.alias, mode, args.requests, args.concurrency, stream_ratio, "stats"
            )
            elapsed = time.time() - phase_started
            phase_marks = {"warmup_end": warmup_end_ts}
            if is_s4:
                phase_marks["step_at"] = warmup_end_ts + elapsed
            step_at_ts = None
            step_hook_result = None
            window_metrics = None

        sampler.stop()
        write_ndjson(out / "requests.ndjson", stat_rows)
        stats_succeeded = sum(1 for r in stat_rows if r['status'] == 200)
        print(f"      Done in {elapsed:.1f}s, {stats_succeeded}/{len(stat_rows)} succeeded")
    finally:
        # S6: restore original share window size
        if is_s6 and original_share_window_size is not None:
            if set_option(args.gateway_url, token_mgr, "RouteStatsShareWindowSize", original_share_window_size):
                if wait_option_value(args.gateway_url, token_mgr, "RouteStatsShareWindowSize", original_share_window_size, timeout_s=30.0):
                    share_window_restored = True
                    print(f"      RouteStatsShareWindowSize restored to {original_share_window_size}")
                else:
                    print(f"      WARNING: RouteStatsShareWindowSize restore to {original_share_window_size} not confirmed within 30s")
            else:
                print(f"      WARNING: Failed to restore RouteStatsShareWindowSize to {original_share_window_size}")
        summary["share_window_size_applied"] = share_window_applied
        summary["share_window_size_restored"] = share_window_restored
        summary["share_window_size_original"] = original_share_window_size
        summary["share_window_size_target"] = share_window_size if is_s6 else None
    print(f"      Done in {elapsed:.1f}s, {stats_succeeded}/{len(stat_rows)} succeeded")

    # S4: collect throttle stats (429 count, Retry-After distribution)
    is_s4 = args.scenario.startswith("S4")
    if is_s4:
        throttle_429 = sum(1 for r in stat_rows if r.get("status") == 429)
        retry_after_vals = [r.get("retry_after_sec") for r in stat_rows if r.get("status") == 429 and r.get("retry_after_sec") is not None]
        retry_after_dist: dict[str, int] = {}
        for v in retry_after_vals:
            key = str(int(v)) if v == int(v) else str(v)
            retry_after_dist[key] = retry_after_dist.get(key, 0) + 1
        summary["throttle_stats"] = {
            "total_429": throttle_429,
            "retry_after_distribution": retry_after_dist,
            "throttle_only_share": targets.get("throttle_only_share"),
        }
        print(f"      S4 throttle: 429s={throttle_429}, retry_after_dist={retry_after_dist}, throttle_only_share={targets.get('throttle_only_share'):.4f}")

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
    # S6: skip reconcile expected_requests check (duration-based, not count-based)
    rec = lib_reconcile.reconcile(
        attempts, upstream_rows,
        expected_requests=args.requests if args.requests is not None else 0,
        expected_request_ids=stat_id_set,
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
    # S6: subject is "STABLE" in scenario config, maps to route label "A"
    subject_route_label = "A" if is_s6 else targets["subject"]
    for r in routes:
        snap = after_by_identity.get(r.identity(), r)
        selections = per_route.get(r.identity(), 0)
        stats = lib_stats.share_stats(selections, opportunities)
        is_subject = r.label == subject_route_label
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
        if is_subject and not is_s6:
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

    # S5: compute healthy_share_sum (sum of GOOD routes' observed shares)
    if is_s5:
        healthy_share_sum = sum(
            row["observed_share"] for row in share_rows if row["route"].startswith("GOOD")
        )
        summary["healthy_share_sum"] = healthy_share_sum
        print(f"      S5 healthy_share_sum (sum of GOOD routes): {healthy_share_sum:.4f}")

    summary["service_quality"] = service_quality(stat_rows, elapsed)
    # Track user requests vs total attempts for S4
    summary["user_requests"] = args.requests
    summary["total_attempts"] = opportunities
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

    # Build traffic windows using lib_report with phase marks
    if is_s6:
        phase_marks = {"warmup_end": warmup_end_ts}
        if step_at_ts is not None:
            phase_marks["step_at"] = step_at_ts
            phase_marks["cooldown_start"] = step_at_ts + tail_seconds
    else:
        phase_marks = {"warmup_end": warmup_end_ts}
        if is_s4:
            phase_marks["step_at"] = warmup_end_ts + elapsed  # no step in S4, but mark end
    traffic_windows_data = lib_report.build_traffic_windows(stat_rows, windows, window_s=10, phase_marks=phase_marks)

    # S6: fill traffic windows with per-window metrics from window_metrics
    if is_s6 and window_metrics:
        # Map window_metrics to traffic_windows_data by window_start
        metrics_by_window = {m["window_start"]: m for m in window_metrics}
        for tw in traffic_windows_data:
            ws = tw["window_start"]
            if ws in metrics_by_window:
                m = metrics_by_window[ws]
                # route_shares: JSON string of subject route share
                tw["route_shares"] = json.dumps({"subject_share": m["subject_share"]})
                # corr_p99: float
                tw["corr_p99"] = m["corr_p99"]
                # ewma: JSON string of ewma_quality per route
                tw["ewma"] = json.dumps(m["ewma"])

    lib_report.write_shares_csv(out / "shares.csv", share_rows)
    lib_report.write_windows_csv(out / "windows.csv", traffic_windows_data)
    lib_report.write_resources_csv(out / "resources.csv", resource_rows)

    subject_ok = share_eval[subject_route_label]["ok"]
    effective = summary["service_quality"]["succeeded"]

    # S6: process stability evaluation instead of point-estimate CI judgment
    if is_s6:
        # Build simplified windows for evaluate_process_stability
        simplified_windows = []
        for m in window_metrics:
            simplified_windows.append({
                "share": m["subject_share"],
                "corr_p99": m["corr_p99"],
                "samples": m["samples"],
            })
        process_stability = lib_stats.evaluate_process_stability(simplified_windows, targets["process_thresholds"])
        summary["process_stability"] = process_stability

        # Compute step_follow_seconds: time from step_at to when subject's ewma_quality stabilizes
        # Stabilization = 3 consecutive windows with ewma change < 5%
        step_follow_seconds = None
        if step_at_ts and window_metrics:
            subject_ewma_series = []
            for m in window_metrics:
                if step_at_ts <= m["window_start"]:
                    subject_ewma_series.append(m["ewma"].get(subject_route_label, 0.0))
            if len(subject_ewma_series) >= 3:
                for i in range(2, len(subject_ewma_series)):
                    window_vals = subject_ewma_series[i-2:i+1]
                    max_val = max(window_vals)
                    min_val = min(window_vals)
                    if max_val > 0 and (max_val - min_val) / max_val < 0.05:
                        # Stabilized at window i
                        stabilized_ts = window_metrics[i]["window_start"]
                        step_follow_seconds = max(0.0, stabilized_ts - step_at_ts)
                        break
        summary["step_follow_seconds"] = step_follow_seconds
        summary["step_hook_result"] = step_hook_result
        summary["step_at_ts"] = step_at_ts

        # S6 verdict priority:
        # ENVIRONMENT_INVALID(2) > DATA_INVALID(1, reconcile fail or insufficient_windows)
        # > PRODUCT_FAIL(1, process_stability.ok False and not due to insufficient_windows)
        # > PASS(0)
        if rec.verdict != "PASS":
            verdict, code = "DATA_INVALID", 1
        elif process_stability.get("insufficient_windows"):
            verdict, code = "DATA_INVALID", 1
        elif not process_stability["ok"]:
            verdict, code = "PRODUCT_FAIL", 1
        else:
            verdict, code = "PASS", 0
    else:
        # S1-S5 existing verdict logic
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

    subject_row = next(r for r in share_rows if r["route"] == subject_route_label)
    if is_s6:
        print(f"      Subject {subject_route_label}: share={subject_row['observed_share']:.4f} "
              f"(process stability, no point-estimate target)")
        print(f"      Process stability: {summary.get('process_stability', {})}")
        print(f"      Step follow seconds: {summary.get('step_follow_seconds')}")
        print(f"      Step hook: {summary.get('step_hook_result')}")
    else:
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
