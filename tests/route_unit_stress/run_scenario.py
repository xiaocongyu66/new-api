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
import os
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
# Retries happen inside the gateway, so without audit rows the count is not
# observable at all. #418 requires unavailable metrics to say so explicitly
# rather than defaulting to a misleading 0.

# Per-process run tag mixed into every generated request id. The audit ring
# persists across runs against the same gateway, so ids like "s11-weighted-1"
# from an earlier run collide with this one and inflate any attempts-per-request
# derivation (it reported 300 phantom retries for 300 clean requests).
RUN_TAG = uuid.uuid4().hex[:8]
NOT_AVAILABLE_RETRY = "NOT_AVAILABLE: retry count needs gateway audit rows"


def detect_gateway_pid(gateway_url: str) -> int | None:
    """Find the local PID listening on the gateway's port.

    Walks /proc/net/tcp to map the listening port to an inode, then scans
    /proc/*/fd for the owning process. Returns None when the gateway is remote or
    the mapping cannot be made — the caller records NOT_AVAILABLE rather than
    guessing, because a wrong PID would make restart detection lie.
    """
    try:
        from urllib.parse import urlparse
        parsed = urlparse(gateway_url)
        port = parsed.port
        host = parsed.hostname or ""
        if port is None or host not in ("127.0.0.1", "localhost", "::1", "0.0.0.0"):
            return None
    except Exception:
        return None

    target_inodes: set[str] = set()
    for proto in ("tcp", "tcp6"):
        try:
            with open(f"/proc/net/{proto}", "r", encoding="utf-8") as f:
                next(f, None)
                for line in f:
                    parts = line.split()
                    if len(parts) < 10:
                        continue
                    local = parts[1]
                    # state 0A == LISTEN
                    if parts[3] != "0A":
                        continue
                    try:
                        if int(local.rsplit(":", 1)[1], 16) == port:
                            target_inodes.add(parts[9])
                    except (ValueError, IndexError):
                        continue
        except (OSError, StopIteration):
            continue
    if not target_inodes:
        return None

    for proc in Path("/proc").iterdir():
        if not proc.name.isdigit():
            continue
        fd_dir = proc / "fd"
        try:
            for fd in fd_dir.iterdir():
                try:
                    link = os.readlink(fd)
                except OSError:
                    continue
                if link.startswith("socket:[") and link[8:-1] in target_inodes:
                    return int(proc.name)
        except OSError:
            continue  # process vanished or not ours
    return None


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


def fetch_share_snapshot(gateway_url: str, token_mgr: AdminTokenManager) -> list[dict[str, Any]] | None:
    """Return the per-pool share window snapshots, or None on failure.

    Same endpoint as fetch_audit; the payload carries both "attempts" and
    "shares", and S11 needs the window side to prove what did and did not
    enter the share window.
    """
    for _ in range(2):
        try:
            r = requests.get(
                f"{gateway_url.rstrip('/')}/api/route_unit/audit",
                headers=token_mgr.auth_header(),
                timeout=30,
            )
            if r.status_code == 200:
                shares = r.json().get("shares")
                return shares if isinstance(shares, list) else []
            if token_mgr.maybe_refresh(r.status_code):
                continue
            return None
        except Exception:
            return None
    return None


def summarise_window_paths(
    shares: list[dict[str, Any]],
    attempts: list[dict[str, Any]],
) -> tuple[list[str], int]:
    """Map share-window selections back to the paths that produced them.

    Schema verified against pkg/routestats/audit.go SharePoolSnapshot on a live
    gateway: each pool is {"pool": {...}, "window": [{"selected": {...},
    "targets": [...]}]}. The key is "window" (NOT "entries"), and "selected" is
    the route that actually took the slot while "targets" is the candidate set
    with its entitlements. Only "selected" represents traffic that entered the
    window, so the path join uses it; counting "targets" would count every
    candidate of every request.

    The window stores route identities, not path labels, so labels come from
    joining on the identity quadruple carried by the audit attempts.

    Probe isolation is the asymmetry S11 checks: probes go through
    SelectedRouteForProbe (recordShare=false), so they never call RecordAttempt
    AND never take a window slot. A selected identity that matches no audited
    attempt therefore indicates a probe (or other unaudited traffic) leaking
    into the window.

    Returns:
        (path labels seen in the window, window slots with no matching attempt)
    """
    labels_by_identity: dict[tuple, set[str]] = {}
    for a in attempts:
        identity = (a.get("channel_id"), a.get("key_index"), a.get("upstream_model"))
        path = (a.get("path") or "").strip()
        if path:
            labels_by_identity.setdefault(identity, set()).add(path)

    window_paths: list[str] = []
    probe_opportunities = 0
    for pool in shares:
        for entry in pool.get("window", []) or []:
            selected = entry.get("selected") or {}
            identity = (
                selected.get("channel_id"),
                selected.get("key_index"),
                selected.get("upstream_model"),
            )
            found = labels_by_identity.get(identity)
            if found:
                window_paths.extend(sorted(found))
            else:
                probe_opportunities += 1
    return window_paths, probe_opportunities


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
        # error_kind classifies a failure as http_status / timeout / cancelled /
        # transport so #418's separate timeout and cancel counts are derivable.
        "error_kind": None,
        "prompt_tokens": None,
        "completion_tokens": None,
        "total_tokens": None,
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
            # Streaming frames stand in for completion tokens: the mock emits one
            # frame per token, so the frame count is the only token signal the
            # client can observe without trusting a usage block it did not verify.
            row["completion_tokens"] = len(frame_times)
        else:
            body = r.content  # drain so latency covers the whole response
            try:
                usage = json.loads(body).get("usage") or {}
                row["prompt_tokens"] = usage.get("prompt_tokens")
                row["completion_tokens"] = usage.get("completion_tokens")
                row["total_tokens"] = usage.get("total_tokens")
            except Exception:
                pass  # non-JSON or usage-less body: leave token fields unset
        row["latency_ms"] = (time.perf_counter() - started) * 1000.0
        if r.status_code != 200:
            row["error"] = r.text[:200]
            row["error_kind"] = "http_status"
    except requests.exceptions.Timeout as exc:
        # Distinguished from a generic transport error because #418 asks for a
        # separate timeout count, and a timeout is a saturation signal.
        row["latency_ms"] = (time.perf_counter() - started) * 1000.0
        row["error"] = str(exc)[:200]
        row["error_kind"] = "timeout"
    except (requests.exceptions.ConnectionError, requests.exceptions.ChunkedEncodingError) as exc:
        # A mid-stream disconnect lands here: the request was cancelled by the
        # peer rather than answered, which is not the same as an HTTP failure.
        row["latency_ms"] = (time.perf_counter() - started) * 1000.0
        row["error"] = str(exc)[:200]
        row["error_kind"] = "cancelled"
    except Exception as exc:
        row["latency_ms"] = (time.perf_counter() - started) * 1000.0
        row["error"] = str(exc)[:200]
        row["error_kind"] = "transport"
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


def service_quality(
    rows: list[dict[str, Any]],
    elapsed_s: float,
    attempts: list[dict[str, Any]] | None = None,
    requested: int | None = None,
) -> dict[str, Any]:
    """Latency/throughput metrics with success and failure kept separate.

    Args:
        rows: client-side request rows.
        elapsed_s: stat-phase wall time.
        attempts: gateway audit rows; used to derive the retry count, which is
            only visible gateway-side (the client sees one response per request).
        requested: how many requests the phase intended to send, so a generator
            that could not keep up is reported instead of silently ignored.
    """
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

    # Error kinds: #418 wants timeout and cancel counted apart from HTTP failures.
    kind_counts: dict[str, int] = {}
    for r in rows:
        k = r.get("error_kind")
        if k:
            kind_counts[k] = kind_counts.get(k, 0) + 1

    # Retries are gateway-side: one user request can span several attempts, so the
    # retry count is attempts-minus-requests, floored at 0. Only attempts belonging
    # to a request we actually sent may be counted: S11 fires administrative probes
    # that create attempts with no client row, and counting those reported 300
    # phantom retries for 300 clean requests. Without audit rows the number is
    # genuinely unobservable rather than zero.
    if attempts is None:
        retry_count: Any = NOT_AVAILABLE_RETRY
    else:
        own_ids = {r.get("request_id") for r in rows if r.get("request_id")}
        own_attempts = sum(1 for a in attempts if a.get("client_request_id") in own_ids)
        retry_count = max(0, own_attempts - len(rows))

    completion_tokens = [r.get("completion_tokens") for r in rows if r.get("completion_tokens") is not None]
    total_completion = sum(completion_tokens) if completion_tokens else 0

    # "Dropped iterations" in the k6 sense: requests the phase meant to issue but
    # never did, which is the generator-saturation signal #418 asks for.
    dropped = None
    if requested is not None:
        dropped = max(0, requested - len(rows))

    # A 503 carrying new-api's own overload code came from the gateway's
    # SystemPerformanceCheck middleware (middleware/performance.go), which sheds
    # load when host CPU/memory/disk crosses performance_setting.monitor_*
    # thresholds. It never reached the upstream, so attributing it to mock capacity
    # is wrong: S2's archived 720 503s had ~19ms latency and produced zero upstream
    # rows. Count these separately so the report can name the real cause.
    shed_codes = ("system_cpu_overloaded", "system_memory_overloaded", "system_disk_overloaded")
    shed_counts: dict[str, int] = {}
    for r in bad:
        err = r.get("error") or ""
        for code in shed_codes:
            if code in err:
                shed_counts[code] = shed_counts.get(code, 0) + 1
                break
    gateway_shed_total = sum(shed_counts.values())

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
        "error_kind_counts": kind_counts,
        "timeout_count": kind_counts.get("timeout", 0),
        "cancelled_count": kind_counts.get("cancelled", 0),
        "transport_error_count": kind_counts.get("transport", 0),
        "retry_count": retry_count,
        "completion_tokens_total": total_completion,
        "completion_tokens_samples": len(completion_tokens),
        "token_throughput_per_s": (total_completion / elapsed_s) if elapsed_s > 0 else 0.0,
        "requested": requested,
        "gateway_shed_load_503": gateway_shed_total,
        "gateway_shed_load_breakdown": shed_counts,
        "gateway_shed_load_note": (
            "503s emitted by the gateway's own SystemPerformanceCheck middleware "
            "(performance_setting.monitor_* thresholds). These never reached the upstream, so they "
            "are runner/host saturation, NOT upstream mock capacity."
        ) if gateway_shed_total else None,
        "dropped_iterations": dropped,
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
                req_id = f"s6-{RUN_TAG}-{next_req_id}"
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


def collect_corr_snapshots(
    gateway_url: str,
    token_mgr: AdminTokenManager,
    alias: str,
    routes: list[RouteInfo],
    duration_s: int,
    interval_s: float,
) -> tuple[list[dict[str, list[float]]], list[dict[str, Any]]]:
    """S7: poll topology at high frequency collecting share_correction per route.

    Returns:
        corr_snapshots: list of {"label": [corr_val, ...]} per poll cycle
        route_snapshots: raw topology items per poll (for after-snapshot)
    """
    corr_snapshots: list[dict[str, list[float]]] = []
    route_snapshots: list[list[dict[str, Any]]] = []
    identity_map = {r.identity(): r.label for r in routes}
    deadline = time.time() + duration_s
    poll_count = 0
    last_progress = time.time()

    while time.time() < deadline:
        items = fetch_topology(gateway_url, token_mgr, alias)
        if items is not None:
            route_snapshots.append(items)
            snap: dict[str, list[float]] = {}
            for item in items:
                identity = (item.get("channel_id"), item.get("key_index"), item.get("upstream_model"))
                label = identity_map.get(identity)
                if label:
                    corr = item.get("share_correction", 0.0)
                    snap.setdefault(label, []).append(float(corr))
            corr_snapshots.append(snap)
            poll_count += 1

        now = time.time()
        if now - last_progress >= 30:
            total_vals = sum(len(v) for snap in corr_snapshots for v in snap.values())
            print(f"      S7: {now - (deadline - duration_s):.1f}s, {poll_count} polls, {total_vals} corr values")
            last_progress = now

        time.sleep(interval_s)

    return corr_snapshots, route_snapshots


def run_phase_s7(
    gateway_url: str,
    token: str,
    alias: str,
    mode: str,
    duration_s: int,
    concurrency: int,
    stream_ratio: float,
    token_mgr: AdminTokenManager,
    routes: list[RouteInfo],
    corr_interval_s: float,
    step_hook_cmd: str | None,
    step_at_ratio: float,
) -> tuple[list[dict[str, Any]], list[str], list[dict[str, list[float]]], dict | None]:
    """S7: run traffic + collect corr snapshots simultaneously.

    Returns:
        stat_rows, stat_ids, corr_snapshots, step_hook_result
    """
    import subprocess

    step_at_s = duration_s * step_at_ratio
    phase_started = time.time()
    deadline = phase_started + duration_s
    step_at_deadline = phase_started + step_at_s
    step_triggered = False
    step_hook_result = None

    stat_rows: list[dict[str, Any]] = []
    stat_ids: list[str] = []
    next_req_id = 0

    # Start corr snapshot collection in a background thread
    import threading
    corr_snapshots: list[dict[str, list[float]]] = []
    _route_snapshots: list[list[dict[str, Any]]] = []

    def _collect():
        nonlocal corr_snapshots, _route_snapshots
        corr_snapshots, _route_snapshots = collect_corr_snapshots(
            gateway_url, token_mgr, alias, routes, duration_s, corr_interval_s
        )

    collector_thread = threading.Thread(target=_collect, daemon=True)
    collector_thread.start()

    print(f"      S7: running {duration_s}s with corr polling at {corr_interval_s}s interval")
    last_progress = time.time()

    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures: set = set()
        stream = stream_ratio > 0.0

        while time.time() < deadline or futures:
            now = time.time()

            if not step_triggered and now >= step_at_deadline:
                step_triggered = True
                print(f"      S7: step transition at {now - phase_started:.1f}s")
                if step_hook_cmd:
                    hook_start = time.time()
                    try:
                        proc = subprocess.run(step_hook_cmd, shell=True, capture_output=True, text=True, timeout=30)
                        hook_duration = time.time() - hook_start
                        step_hook_result = {
                            "cmd": step_hook_cmd,
                            "exit_code": proc.returncode,
                            "duration_sec": hook_duration,
                        }
                    except Exception as e:
                        step_hook_result = {"cmd": step_hook_cmd, "exit_code": -1, "error": str(e)}
                else:
                    step_hook_result = {"skipped": True}

            while len(futures) < concurrency and time.time() < deadline:
                req_id = f"s7-{RUN_TAG}-{next_req_id}"
                next_req_id += 1
                fut = pool.submit(send_request, gateway_url, token, alias, req_id, mode, stream, "stats")
                futures.add(fut)

            done = {f for f in futures if f.done()}
            for f in done:
                row = f.result()
                stat_rows.append(row)
                stat_ids.append(row.get("request_id", ""))
                futures.discard(f)

            if now - last_progress >= 30:
                print(f"      S7: {now - phase_started:.1f}s, {len(stat_rows)} requests, step={'done' if step_triggered else 'pending'}")
                last_progress = now

            if futures and not done:
                time.sleep(0.05)

    collector_thread.join(timeout=5.0)
    return stat_rows, stat_ids, corr_snapshots, step_hook_result


def run_phase_s8(
    gateway_url: str,
    token: str,
    alias: str,
    mode: str,
    duration_s: int,
    concurrency: int,
    stream_ratio: float,
    token_mgr: AdminTokenManager,
    routes: list[RouteInfo],
) -> tuple[list[dict[str, Any]], list[str], list[dict[str, Any]]]:
    """S8: run steady-state traffic and collect memory/pool snapshots.

    Returns:
        stat_rows, stat_ids, memory_snapshots
    """
    stat_rows: list[dict[str, Any]] = []
    stat_ids: list[str] = []
    memory_snapshots: list[dict[str, Any]] = []
    phase_started = time.time()
    deadline = phase_started + duration_s
    next_req_id = 0
    last_snapshot = 0.0
    last_progress = time.time()

    print(f"      S8: running {duration_s}s steady state, {concurrency} concurrent")

    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures: set = set()
        stream = stream_ratio > 0.0

        while time.time() < deadline or futures:
            now = time.time()

            while len(futures) < concurrency and time.time() < deadline:
                req_id = f"s8-{RUN_TAG}-{next_req_id}"
                next_req_id += 1
                fut = pool.submit(send_request, gateway_url, token, alias, req_id, mode, stream, "stats")
                futures.add(fut)

            done = {f for f in futures if f.done()}
            for f in done:
                row = f.result()
                stat_rows.append(row)
                stat_ids.append(row.get("request_id", ""))
                futures.discard(f)

            # Collect memory snapshot every 30s
            if now - last_snapshot >= 30.0:
                items = fetch_topology(gateway_url, token_mgr, alias)
                active_pools = len([i for i in (items or []) if i.get("enabled") and i.get("channel_status") == 1])
                # ponytail: pool_count approximated from topology items (no SharePoolCount API)
                pool_count = active_pools  # indirect inference; SharePoolCount() not exposed via API
                rss = 0
                try:
                    # Read the gateway process RSS, not the runner's own /proc/self
                    # The gateway PID is found by matching the port from /proc/net/tcp + /proc/*/cmdline
                    # ponytail: fallback to gateway_url host:port -> pgrep -> /proc/<pid>/status
                    import subprocess as _sp
                    gw_pid_out = _sp.check_output(
                        ["pgrep", "-f", "newapi"],
                        stderr=_sp.DEVNULL,
                        timeout=5,
                    ).strip()
                    gw_pid = int(gw_pid_out.splitlines()[0])
                    status_content = Path(f"/proc/{gw_pid}/status").read_text()
                    for line in status_content.splitlines():
                        if line.startswith("VmRSS:"):
                            rss = int(line.split()[1]) * 1024
                            break
                except Exception:
                    pass
                # Try pprof heap if available
                heap = None
                try:
                    pprof_resp = requests.get(
                        "http://127.0.0.1:6060/debug/pprof/heap",
                        timeout=5,
                    )
                    if pprof_resp.status_code == 200:
                        # pprof returns text; extract alloc_bytes from top line
                        for line in pprof_resp.text.splitlines():
                            if "alloc" in line.lower():
                                # ponytail: parse first numeric after alloc
                                parts = line.split(":")
                                if len(parts) >= 2:
                                    val_str = parts[-1].strip().split()[0]
                                    heap = int(val_str.replace(",", ""))
                                    break
                except Exception:
                    pass  # pprof not available

                memory_snapshots.append({
                    "ts": now,
                    "rss": rss,
                    "heap": heap,
                    "pool_count": pool_count,
                    "active_pools": active_pools,
                })
                last_snapshot = now

            if now - last_progress >= 30:
                print(f"      S8: {now - phase_started:.1f}s, {len(stat_rows)} requests, {len(memory_snapshots)} snapshots")
                last_progress = now

            if futures and not done:
                time.sleep(0.05)

    return stat_rows, stat_ids, memory_snapshots


def run_phase_s9(
    gateway_url: str,
    token: str,
    alias: str,
    affinity_ratio: float,
    count: int,
    concurrency: int,
    phase: str,
) -> tuple[list[dict[str, Any]], list[str]]:
    """S9: send requests with controlled affinity ratio via prompt_cache_key.

    affinity_ratio fraction of requests use a fixed prompt_cache_key (pinned to route A);
    the rest use unique keys (weighted selection).

    Returns:
        stat_rows, stat_ids
    """
    affinity_count = int(round(count * affinity_ratio))
    pinned_key = "s9-pinned-affinity-key"
    stat_rows: list[dict[str, Any]] = []
    stat_ids: list[str] = []
    stream = False  # S9 uses non-streaming

    def send_affinity_request(idx: int) -> dict[str, Any]:
        req_id = f"s9-{RUN_TAG}-{phase}-{idx}"
        headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
            "X-Request-Id": req_id,
            "X-Mock-Mode": "ok",
        }
        if idx < affinity_count:
            payload = {
                "model": alias,
                "messages": [{"role": "user", "content": f"scenario {req_id[:8]}"}],
                "max_tokens": 16,
                "stream": False,
                "prompt_cache_key": pinned_key,
            }
        else:
            payload = {
                "model": alias,
                "messages": [{"role": "user", "content": f"scenario {req_id[:8]}"}],
                "max_tokens": 16,
                "stream": False,
                "prompt_cache_key": f"s9-unique-{idx}",
            }
        started = time.perf_counter()
        row: dict[str, Any] = {
            "request_id": req_id,
            "phase": phase,
            "stream": False,
            "mode": "ok",
            "status": 0,
            "latency_ms": None,
            "ttft_ms": None,
            "itl_ms": [],
            "error": None,
            "start_ts": time.time(),
            "end_ts": None,
            "retry_after_sec": None,
            "is_affinity": idx < affinity_count,
        }
        try:
            r = requests.post(
                f"{gateway_url.rstrip('/')}/v1/chat/completions",
                headers=headers,
                json=payload,
                timeout=60.0,
            )
            row["status"] = r.status_code
            if r.status_code != 200:
                row["error"] = r.text[:200]
            r.content
        except Exception as exc:
            row["error"] = str(exc)[:200]
        finally:
            row["latency_ms"] = (time.perf_counter() - started) * 1000.0
            row["end_ts"] = time.time()
        return row

    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures = [
            pool.submit(send_affinity_request, i)
            for i in range(count)
        ]
        for fut in as_completed(futures):
            row = fut.result()
            stat_rows.append(row)
            stat_ids.append(row["request_id"])

    return stat_rows, stat_ids


def run_phase_s11(
    gateway_url: str,
    token: str,
    alias: str,
    per_path: int,
    probe_count: int,
    concurrency: int,
    specific_channel_id: int | None,
    token_mgr: AdminTokenManager,
) -> tuple[list[dict[str, Any]], list[str], dict[str, int], int, list[dict[str, Any]]]:
    """S11: drive each labelled selection path, plus administrative probes.

    weighted  - plain request, no affinity key, no channel pin
    affinity  - prompt_cache_key pins the chain to one channel (gpt-* rule)
    specific  - Authorization "sk-xxx-<channel_id>" pins the channel directly;
                this is also the code path locked replay uses, so it has no
                separate label of its own
    probe     - POST /api/channel/test/<id>, admin JWT, never user traffic

    Returns:
        stat_rows, stat_ids, path_request_counts, probes_sent
    """
    stat_rows: list[dict[str, Any]] = []
    stat_ids: list[str] = []
    path_counts: dict[str, int] = {"weighted": 0, "affinity": 0, "specific": 0}

    def send_labelled(path: str, idx: int) -> dict[str, Any]:
        req_id = f"s11-{RUN_TAG}-{path}-{idx}"
        use_token = token
        payload: dict[str, Any] = {
            "model": alias,
            "messages": [{"role": "user", "content": f"s11 {path} {idx}"}],
            "max_tokens": 16,
            "stream": False,
        }
        if path == "affinity":
            # One shared key so every affinity request sticks to the same channel.
            payload["prompt_cache_key"] = "s11-affinity-pin"
        elif path == "specific":
            # The gateway reads the channel id from the key suffix and only
            # honours it for admin tokens.
            use_token = f"{token}-{specific_channel_id}"
        headers = {
            "Authorization": f"Bearer {use_token}",
            "Content-Type": "application/json",
            "X-Request-Id": req_id,
            "X-Mock-Mode": "ok",
        }
        row: dict[str, Any] = {
            "request_id": req_id,
            "phase": "stats",
            "stream": False,
            "mode": "ok",
            "status": 0,
            "latency_ms": None,
            "ttft_ms": None,
            "itl_ms": [],
            "error": None,
            "start_ts": time.time(),
            "end_ts": None,
            "retry_after_sec": None,
            "intended_path": path,
        }
        started = time.perf_counter()
        try:
            r = requests.post(
                f"{gateway_url.rstrip('/')}/v1/chat/completions",
                headers=headers, json=payload, timeout=60.0,
            )
            row["status"] = r.status_code
            r.content
            if r.status_code != 200:
                row["error"] = r.text[:200]
        except Exception as exc:
            row["error"] = str(exc)[:200]
        finally:
            row["latency_ms"] = (time.perf_counter() - started) * 1000.0
            row["end_ts"] = time.time()
        return row

    # The affinity path only exists once the cache holds an entry for the key: the
    # FIRST request with a given prompt_cache_key has nothing to stick to, so
    # middleware/distributor.go labels it "weighted" and creates the pin. Fired
    # concurrently, every request already in flight races past the empty cache, so a
    # plain batch of N yields fewer than N affinity-labelled attempts (measured
    # 95/100, with indices 0-4 labelled weighted).
    #
    # Send one pin-establishing request serially first, and exclude it from the
    # measured batch, so the N that follow can all take the affinity path. This is a
    # correct-by-construction fix rather than relaxing #418's per-path minimum.
    affinity_warm_rows: list[dict[str, Any]] = []
    if per_path > 0:
        affinity_warm_rows.append(send_labelled("affinity", -1))

    jobs: list[tuple[str, int]] = []
    for path in ("weighted", "affinity", "specific"):
        if path == "specific" and specific_channel_id is None:
            continue
        for i in range(per_path):
            jobs.append((path, i))

    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures = [pool.submit(send_labelled, p, i) for p, i in jobs]
        for fut in as_completed(futures):
            row = fut.result()
            stat_rows.append(row)
            stat_ids.append(row["request_id"])
            path_counts[row["intended_path"]] = path_counts.get(row["intended_path"], 0) + 1

    # Probes are administrative: they must produce EWMA signal but never enter
    # the share window. Fired serially; 20 of them is not a load concern.
    probes_sent = 0
    if probe_count > 0 and specific_channel_id is not None:
        for _ in range(probe_count):
            try:
                pr = requests.get(
                    f"{gateway_url.rstrip('/')}/api/channel/test/{specific_channel_id}",
                    headers=token_mgr.auth_header(),
                    params={"model": alias},
                    timeout=60,
                )
                if pr.status_code in (200, 400, 500):
                    probes_sent += 1
                elif token_mgr.maybe_refresh(pr.status_code):
                    continue
            except Exception:
                pass

    # The pin request is deliberately NOT in stat_rows/stat_ids: it is setup, and
    # counting it would put a weighted-labelled affinity request back into the
    # measured batch. It is returned so the summary can show the pin really ran.
    return stat_rows, stat_ids, path_counts, probes_sent, affinity_warm_rows


def run_phase_s12(
    gateway_url: str,
    token: str,
    alias: str,
    mode: str,
    count: int,
    concurrency: int,
    phase: str,
) -> tuple[list[dict[str, Any]], list[str]]:
    """S12: fixed-count non-streaming traffic; B fails once then succeeds on retry.

    Attempt counts are NOT derivable client-side (the gateway retries internally
    and returns one response), so no attempt_count field is invented here. The
    attempt-level view comes from the audit ring.
    """
    return run_phase(gateway_url, token, alias, mode, count, concurrency, 0.0, phase)

def run_phase_s10_restart(
    gateway_url: str,
    token: str,
    alias: str,
    mode: str,
    count: int,
    concurrency: int,
    token_mgr: AdminTokenManager,
    routes: list[RouteInfo],
    restart_hook: str | None,
    kill_switch_option: str,
    out: Path,
) -> tuple[list[dict[str, Any]], list[str], dict[str, Any], dict[str, Any], dict[str, Any]]:
    """S10_RESTART: prove the kill switch survives a real restart.

    Sequence: traffic -> snapshot -> write RouteStatsEnabled=false -> restart ->
    snapshot -> traffic. The before/after snapshots carry the option value, every
    route's correction, the share-window entry count and the gateway PID, because
    each of those is one half of a claim the scenario has to check:
      - option value  -> the write persisted across the restart
      - corrections   -> all exactly 1.0 (neutral) once disabled
      - window count  -> stopped growing
      - PID           -> a restart actually happened

    Returns:
        stat_rows, stat_ids, before_snapshot, after_snapshot, sweep_evidence
    """
    import subprocess

    def snapshot(label: str) -> dict[str, Any]:
        opts = fetch_options(gateway_url, token_mgr) or {}
        items = fetch_topology(gateway_url, token_mgr, alias) or []
        shares = fetch_share_snapshot(gateway_url, token_mgr) or []
        window_entries = sum(len(p.get("window", []) or []) for p in shares)
        corrections = [
            float(i.get("share_correction", 0.0) or 0.0)
            for i in items
            if i.get("enabled") and i.get("channel_status") == 1
        ]
        snap = {
            "label": label,
            "ts": time.time(),
            "option": opts.get(kill_switch_option),
            "corrections": corrections,
            "window_entries": window_entries,
            "pool_count": len(shares),
            "pid": str(detect_gateway_pid(gateway_url) or ""),
        }
        (out / f"kill-switch-{label}.json").write_text(
            json.dumps(snap, indent=2, ensure_ascii=False), encoding="utf-8"
        )
        print(f"      S10 {label}: option={snap['option']!r} corrections={corrections} "
              f"window_entries={window_entries} pid={snap['pid']}")
        return snap

    # Phase 1: traffic with route stats live, so the window has something in it.
    print(f"      S10 restart: {count} requests before the kill switch")
    rows_before, ids_before = run_phase(
        gateway_url, token, alias, mode, count, concurrency, 0.0, "stats"
    )
    before = snapshot("before")

    # Phase 2: flip the kill switch through the real admin API.
    if not set_option(gateway_url, token_mgr, kill_switch_option, "false"):
        print(f"      WARNING: failed to set {kill_switch_option}=false")
    else:
        wait_option_value(gateway_url, token_mgr, kill_switch_option, "false", timeout_s=30.0)
        print(f"      S10 {kill_switch_option}=false written")

    # Phase 3: restart. Without a hook the persistence claim is untestable, so
    # record that rather than pretending the restart happened.
    sweep_evidence: dict[str, Any] = {"pool_count_before": before["pool_count"]}
    if restart_hook:
        started = time.time()
        try:
            proc = subprocess.run(restart_hook, shell=True, capture_output=True, text=True, timeout=180)
            sweep_evidence["restart_hook"] = {
                "cmd": restart_hook,
                "exit_code": proc.returncode,
                "duration_sec": time.time() - started,
                "stdout": (proc.stdout or "")[-2000:],
                "stderr": (proc.stderr or "")[-2000:],
            }
            print(f"      S10 restart hook exit={proc.returncode} in {time.time() - started:.1f}s")
        except Exception as exc:
            sweep_evidence["restart_hook"] = {"cmd": restart_hook, "exit_code": -1, "stderr": str(exc)}
            print(f"      S10 restart hook failed: {exc}")
        # Wait for the gateway to answer again before snapshotting.
        deadline = time.time() + 90
        while time.time() < deadline:
            try:
                if requests.get(f"{gateway_url.rstrip('/')}/api/status", timeout=5).status_code == 200:
                    break
            except Exception:
                pass
            time.sleep(1.0)
    else:
        sweep_evidence["restart_hook"] = {
            "skipped": True,
            "note": "no --restart-hook: option persistence across restart is UNPROVEN",
        }
        print("      S10 restart hook skipped (persistence unproven)")

    after = snapshot("after")
    sweep_evidence["pool_count_after"] = after["pool_count"]

    # Phase 4: traffic after the restart, to show the window does not grow again.
    print(f"      S10 restart: {count} requests after the restart")
    rows_after, ids_after = run_phase(
        gateway_url, token, alias, mode, count, concurrency, 0.0, "stats"
    )
    post = snapshot("post-traffic")
    # The kill-switch check compares before vs post-traffic: growth here would mean
    # the window kept allocating despite being disabled.
    after["window_entries"] = post["window_entries"]
    after["corrections"] = post["corrections"]
    sweep_evidence["pool_count_post_traffic"] = post["pool_count"]
    sweep_evidence["sweep_note"] = (
        "The share-pool sweep ticker is time.Hour (apps/api/main.go), so a run shorter "
        "than an hour cannot produce a 'route stats sweep' log line. Per #418 that is a "
        "KNOWN LIMITATION for short runs, not a FAIL; pool counts before/after/post are "
        "recorded so a long CI run can be compared against them."
    )
    # The audit ring lives in process memory (pkg/routestats/audit.go), so the
    # restart necessarily discards every attempt recorded before it. Those request
    # ids can therefore never reconcile, and counting them as missing data would
    # report an expected property of this scenario as corruption. Name the
    # pre-restart ids so the caller can scope reconciliation to post-restart
    # traffic and still state the loss explicitly.
    sweep_evidence["pre_restart_request_ids"] = list(ids_before)
    sweep_evidence["audit_ring_reset_note"] = (
        f"{len(ids_before)} pre-restart requests cannot appear in the audit ring: the ring is "
        "in-process state and the restart cleared it. Reconciliation is scoped to the "
        f"{len(ids_after)} post-restart requests; the pre-restart batch remains in "
        "requests.ndjson and upstream-received.ndjson as raw evidence."
    )

    return rows_before + rows_after, ids_before + ids_after, before, after, sweep_evidence

def run_phase_s10_global(
    replica_urls: list[str],
    token: str,
    alias: str,
    mode: str,
    count: int,
    concurrency: int,
    phase: str,
) -> tuple[list[dict[str, Any]], list[str], dict[str, int]]:
    """S10_GLOBAL: spread traffic across replicas, recording which one served each request.

    Share windows are process-local, so a replica converges on its own view. The
    only sound global number comes from pooling raw attempts across replicas, and
    that requires knowing which replica produced each attempt — hence the
    round-robin plus a per-row `replica_url`.

    Returns:
        stat_rows, stat_ids, per_replica_request_counts
    """
    stat_rows: list[dict[str, Any]] = []
    stat_ids: list[str] = []
    per_replica: dict[str, int] = {u: 0 for u in replica_urls}

    def send_to_replica(idx: int) -> dict[str, Any]:
        url = replica_urls[idx % len(replica_urls)]
        rid = str(uuid.uuid4())
        row = send_request(url, token, alias, rid, mode, False, phase)
        row["replica_url"] = url
        return row

    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures = [pool.submit(send_to_replica, i) for i in range(count)]
        for fut in as_completed(futures):
            row = fut.result()
            stat_rows.append(row)
            stat_ids.append(row["request_id"])
            per_replica[row["replica_url"]] = per_replica.get(row["replica_url"], 0) + 1

    return stat_rows, stat_ids, per_replica


def fetch_audit_multi(
    replica_urls: list[str],
    token_mgr: AdminTokenManager,
) -> tuple[list[dict[str, Any]], dict[str, int]]:
    """Collect audit attempts from every replica, tagging each with its pod.

    Each replica keeps its own in-process ring, so the global picture needs all of
    them. The `pod` tag is what lets aggregate_global_share report a per-pod
    breakdown while still dividing only once, globally.
    """
    all_attempts: list[dict[str, Any]] = []
    per_pod: dict[str, int] = {}
    for url in replica_urls:
        rows = fetch_audit(url, token_mgr)
        if rows is None:
            print(f"      WARNING: could not read audit from replica {url}")
            continue
        for a in rows:
            a["pod"] = url
        all_attempts.extend(rows)
        per_pod[url] = len(rows)
    return all_attempts, per_pod


def run_phase_s13(
    gateway_url: str,
    token: str,
    alias: str,
    mode: str,
    concurrency: int,
    token_mgr: AdminTokenManager,
    routes: list[RouteInfo],
    subject_label: str,
    steady_s: int,
    fault_s: int,
    recovery_deadline_s: int,
    sample_interval_s: int,
    fault_hook: str | None,
    recover_hook: str | None,
    target: float,
    tolerance: float,
) -> tuple[list[dict[str, Any]], list[str], list[dict[str, Any]], dict[str, Any]]:
    """S13: steady -> fault -> recover on one traffic loop, sampling share and corr.

    Returns:
        stat_rows, stat_ids, samples, hook_results
    """
    import subprocess

    stat_rows: list[dict[str, Any]] = []
    stat_ids: list[str] = []
    samples: list[dict[str, Any]] = []
    hook_results: dict[str, Any] = {}
    next_req_id = 0
    identity_map = {r.identity(): r.label for r in routes}

    def run_hook(name: str, cmd: str | None) -> None:
        if not cmd:
            hook_results[name] = {"skipped": True}
            return
        started = time.time()
        try:
            proc = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=30)
            hook_results[name] = {
                "cmd": cmd,
                "exit_code": proc.returncode,
                "duration_sec": time.time() - started,
                "stdout": (proc.stdout or "")[-500:],
                "stderr": (proc.stderr or "")[-500:],
            }
        except subprocess.TimeoutExpired:
            hook_results[name] = {"cmd": cmd, "exit_code": -1, "duration_sec": time.time() - started, "stderr": "timeout after 30s"}
        except Exception as exc:
            hook_results[name] = {"cmd": cmd, "exit_code": -1, "duration_sec": time.time() - started, "stderr": str(exc)}
        print(f"      S13: {name} hook -> {hook_results[name].get('exit_code')}")

    def take_sample(phase_name: str) -> dict[str, Any] | None:
        items = fetch_topology(gateway_url, token_mgr, alias)
        if items is None:
            return None
        corr: dict[str, float] = {}
        b_share = None
        for item in items:
            label = identity_map.get(
                (item.get("channel_id"), item.get("key_index"), item.get("upstream_model"))
            )
            if not label:
                continue
            corr[label] = float(item.get("share_correction", 0.0) or 0.0)
            if label == subject_label:
                # actual_share is the gateway's own windowed share; preferred over
                # a client-side recount because it is what the correction reacts to.
                if item.get("actual_share") is not None:
                    b_share = float(item["actual_share"])
        return {
            "ts": time.time(),
            "phase": phase_name,
            "b_share": b_share,
            "corr": corr,
            "samples": len(stat_rows),
        }

    print(f"      S13: steady {steady_s}s -> fault {fault_s}s -> recover (deadline {recovery_deadline_s}s)")
    phase_started = time.time()
    current_phase = "steady"
    phase_deadline = phase_started + steady_s
    recover_started = None
    last_sample = 0.0
    recovered = False

    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures: set = set()
        while True:
            now = time.time()

            if now >= phase_deadline:
                if current_phase == "steady":
                    run_hook("fault", fault_hook)
                    current_phase = "fault"
                    phase_deadline = now + fault_s
                elif current_phase == "fault":
                    run_hook("recover", recover_hook)
                    current_phase = "recover"
                    recover_started = now
                    phase_deadline = now + recovery_deadline_s
                elif current_phase == "recover":
                    break

            # Recovery ends early once the subject is back inside tolerance.
            if current_phase == "recover" and recovered:
                break

            while len(futures) < concurrency:
                req_id = f"s13-{RUN_TAG}-{next_req_id}"
                next_req_id += 1
                futures.add(pool.submit(send_request, gateway_url, token, alias, req_id, mode, False, "stats"))

            done = {f for f in futures if f.done()}
            for f in done:
                row = f.result()
                stat_rows.append(row)
                stat_ids.append(row.get("request_id", ""))
                futures.discard(f)

            if now - last_sample >= sample_interval_s:
                s = take_sample(current_phase)
                if s:
                    samples.append(s)
                    if current_phase == "recover" and s["b_share"] is not None:
                        if abs(s["b_share"] - target) <= tolerance:
                            recovered = True
                            print(f"      S13: recovered at {now - (recover_started or now):.1f}s into recover phase")
                last_sample = now

            if futures and not done:
                time.sleep(0.05)

        for f in list(futures):
            try:
                row = f.result(timeout=60)
                stat_rows.append(row)
                stat_ids.append(row.get("request_id", ""))
            except Exception:
                pass

    final = take_sample(current_phase)
    if final:
        samples.append(final)

    return stat_rows, stat_ids, samples, hook_results


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
    p.add_argument("--replica", type=int, default=None, help="S10: replica count; global share is re-aggregated from raw attempts, never averaged per pod")
    p.add_argument("--kill-switch", action="store_true", help="S10_RESTART: write RouteStatsEnabled=false, expect a restart, then verify corrections are neutral")
    p.add_argument("--restart-hook", default=None, help="S10_RESTART: shell command that restarts the gateway between the before/after request batches")
    p.add_argument("--retry-times", type=int, default=None, help="S12/S13: gateway RetryTimes to apply for the run (restored afterwards)")
    p.add_argument("--specific-channel-id", type=int, default=None, help="S11: channel id used for the specific-channel path and administrative probes")
    p.add_argument("--fault-hook", default=None, help="S13: shell command flipping the subject route's mock to a failing mode")
    p.add_argument("--recover-hook", default=None, help="S13: shell command flipping the subject route's mock back to ok")
    p.add_argument("--replica-url", action="append", default=[], help="S10_GLOBAL: additional gateway replica base URL (repeatable). Traffic is spread across --gateway-url plus these, and each replica's audit is collected separately so 'pod' identity is real rather than assumed.")
    p.add_argument("--steady-seconds", type=int, default=60, help="S13: steady phase duration")
    p.add_argument("--fault-seconds", type=int, default=120, help="S13: fault phase duration")
    p.add_argument("--recovery-deadline-seconds", type=int, default=None, help="S13: max seconds to wait for recovery (default from scenario)")
    p.add_argument("--pprof-url", default=None, help="Gateway pprof base URL (e.g. http://127.0.0.1:6060) for Go heap/GC sampling; omitted = NOT_AVAILABLE with reason")
    p.add_argument("--gateway-pid", type=int, default=None, help="Gateway PID for process identity / restart detection")
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
    is_s7 = args.scenario.startswith("S7")
    is_s8 = args.scenario.startswith("S8")
    is_s9 = args.scenario.startswith("S9")
    # S10_* / S11_* / S12_* / S13_* use distinct prefixes; the only exact-name
    # comparison in this function is `args.scenario == "S1"`, so there is no
    # S1 vs S10 collision.
    is_s10 = args.scenario.startswith("S10")
    is_s11 = args.scenario.startswith("S11")
    is_s12 = args.scenario.startswith("S12")
    is_s13 = args.scenario.startswith("S13")
    # S13 runs by phase duration (steady/fault/recover), not a fixed request count.
    is_duration_based = is_s6 or is_s7 or is_s8 or is_s13

    # Duration-based scenarios (S6/S7/S8) share setup
    if is_duration_based:
        share_window_size = targets.get("share_window_size", 200)
        step_at_ratio = args.step_at_ratio if args.step_at_ratio is not None else targets.get("step_at_ratio", 0.5)
        tail_seconds = args.tail_seconds if args.tail_seconds is not None else targets.get("min_tail_seconds", 90)
        # S6: default 180s; S7: default from scenario max_duration_s (900s); S8: from steady_duration_s
        if args.duration_seconds is not None:
            duration = args.duration_seconds
        elif is_s7:
            duration = targets.get("max_duration_s", 900)
        elif is_s8:
            duration = targets.get("steady_duration_s", 2160)
        else:
            duration = 180
        step_hook_cmd = args.step_hook
        # S6/S7: streaming; S8: non-streaming (memory focus, not latency)
        if args.stream_ratio is not None:
            stream_ratio = args.stream_ratio
        elif is_s8:
            stream_ratio = 0.0
        else:
            stream_ratio = 1.0
    else:
        # S4 runs non-streaming; S1 uses mixed; S2/S3/S5/S9 default to streaming
        if args.stream_ratio is not None:
            stream_ratio = args.stream_ratio
        elif is_s4:
            stream_ratio = 0.0
        elif is_s9:
            stream_ratio = 0.0  # S9: non-streaming
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
            "injection": targets.get("injection"),
            "route_count": targets.get("route_count"),
            "share_window_size": targets.get("share_window_size"),
            # #418 requires the run's topology to be stated, and a two-replica run
            # must be distinguishable from a single-pod one in the artifact itself.
            "replica": args.replica,
            "replica_urls": list(args.replica_url or []),
            "replica_count_effective": 1 + len(args.replica_url or []),
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
    # Duration-based scenarios (S6/S7/S8) don't require --requests
    # S9 requires --requests (like S1-S5)
    # S11 derives its request count from min_per_path × the labelled paths, so it
    # needs no --requests either.
    if not is_duration_based and not is_s11 and args.requests is None:
        return fail("--requests is required for non-duration scenarios")

    # --replica is a claim about topology, so it must match the replicas actually
    # addressed. Without this check a single-pod run could be archived as a
    # multi-replica result, which is exactly the "local convergence passed off as
    # global" failure #418 warns about.
    effective_replicas = 1 + len(args.replica_url or [])
    if args.replica is not None and args.replica != effective_replicas:
        return fail(
            f"--replica {args.replica} does not match the {effective_replicas} gateway(s) actually "
            f"addressed (--gateway-url plus {len(args.replica_url or [])} --replica-url). Pass one "
            "--replica-url per additional replica so per-pod identity is real."
        )

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
    # S9 REQUIRES affinity enabled; other scenarios require it disabled
    if is_s9 and not affinity_on:
        return fail(
            "S9 requires channel_affinity_setting.enabled=true: affinity is the mechanism under test. "
            "Enable it via PUT /api/option/ with "
            '{"key":"channel_affinity_setting.enabled","value":"true"}, '
            "or use the admin console."
        )
    if not is_s9 and affinity_on and not args.allow_affinity:
        return fail(
            "channel affinity is enabled: sticky routing bypasses weighted selection and "
            "invalidates share measurement. Disable it before running share scenarios "
            "(admin console, or PUT /api/option/ with "
            '{"key":"channel_affinity_setting.enabled","value":"false"}), '
            "or pass --allow-affinity to record the run as advisory only."
        )
    if affinity_on and not is_s9:
        summary["affinity_enabled_warning"] = (
            "channel_affinity_setting.enabled=true during this run; sticky routing biases the "
            "measured share, so the verdict is advisory and not a scheduler conclusion."
        )
        print("      WARNING: affinity enabled, continuing because --allow-affinity was passed")
    elif is_s9:
        print("      OK (affinity enabled for S9)")
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
    # S8: topology is large-scale seeded externally; don't fail on count mismatch
    if is_s8:
        routes = build_route_infos(items, expected_count=None, bad_channel_id=None)
        print(f"      S8: {len(routes)} active routes (scale target: {expected_count})")
        if len(routes) < 8:
            return fail(
                f"S8 requires at least 8 active routes for pool candidate set; got {len(routes)}"
            )
    elif len(routes) != expected_count:
        return fail(
            f"expected {expected_count} enabled+channel_status=1 route units, "
            f"got {channel_enabled} (topology: total={total_rows}, route.enabled={route_rows_enabled}, channel_enabled={channel_enabled})"
        )
    if is_s5:
        bad_routes = [r for r in routes if r.label == "BAD"]
        if not bad_routes:
            return fail(f"S5 requires a BAD route with channel_id={args.bad_channel_id}, none found among enabled routes")
    (out / "route-before.json").write_text(json.dumps(items, indent=2, ensure_ascii=False), encoding="utf-8")
    # #418 done-when 3: the option map must be snapshotted before AND after the
    # run, so a reviewer can prove which scheduler settings were actually in
    # force and that the runner restored them.
    options_before = fetch_options(args.gateway_url, token_mgr)
    if options_before is not None:
        (out / "options-before.json").write_text(
            json.dumps(options_before, indent=2, ensure_ascii=False, sort_keys=True), encoding="utf-8"
        )
        print(f"      options-before.json written ({len(options_before)} keys)")
    else:
        print("      WARNING: could not snapshot options before the run")
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
    original_share_window_size = None
    share_window_applied = False
    share_window_restored = False
    # S6/S7/S9/S13: switch share window size before stat phase
    needs_window_switch = is_s6 or is_s7 or is_s9 or is_s13
    if needs_window_switch:
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

    # S12/S13: health transitions and retry chains only exist when RetryTimes >= 1.
    original_retry_times = None
    retry_times_applied = None
    original_upstream_threshold = None
    upstream_threshold_applied = None
    if is_s12 or is_s13:
        want_retry = args.retry_times if args.retry_times is not None else targets.get("retry_times", 1)
        if not want_retry or want_retry < 1:
            return fail(
                f"{args.scenario} requires RetryTimes >= 1 (got {want_retry}): without a retry the "
                "scenario cannot observe a retry chain or a health transition"
            )
        original_retry_times = get_option(args.gateway_url, token_mgr, "RetryTimes")
        print(f"      Original RetryTimes: {original_retry_times}")
        if not set_option(args.gateway_url, token_mgr, "RetryTimes", str(want_retry)):
            return fail(f"Failed to set RetryTimes={want_retry}")
        if not wait_option_value(args.gateway_url, token_mgr, "RetryTimes", str(want_retry), timeout_s=30.0):
            return fail(f"RetryTimes did not propagate to {want_retry} within 30s")
        retry_times_applied = want_retry
        print(f"      RetryTimes set to {want_retry} and confirmed")

    if is_s13:
        # A mock 503 is FailureSourceUpstream, so UpstreamFailureThreshold is the
        # knob that escalates the route; LocalFailureThreshold would never fire.
        want_threshold = targets.get("upstream_failure_threshold")
        if want_threshold is not None:
            original_upstream_threshold = get_option(args.gateway_url, token_mgr, "UpstreamFailureThreshold")
            if not set_option(args.gateway_url, token_mgr, "UpstreamFailureThreshold", str(want_threshold)):
                return fail(f"Failed to set UpstreamFailureThreshold={want_threshold}")
            upstream_threshold_applied = want_threshold
            print(f"      UpstreamFailureThreshold set to {want_threshold} (was {original_upstream_threshold})")
        if not args.fault_hook or not args.recover_hook:
            return fail(
                "S13 needs --fault-hook and --recover-hook to flip the subject route's mock mode "
                "at runtime; without them no degradation can be injected"
            )

    # Scenario-local collectors: bound up front so a verdict branch can never hit
    # an unbound name when its phase did not run.
    s11_path_counts: dict[str, int] = {}
    s11_probes = 0
    s13_samples: list[dict[str, Any]] = []
    s13_hooks: dict[str, Any] = {}

    # Resolve the gateway PID once: it feeds process-identity sampling and is the
    # only way S10_RESTART can prove a restart actually happened.
    gateway_pid = args.gateway_pid
    if gateway_pid is None:
        gateway_pid = detect_gateway_pid(args.gateway_url)
    summary["gateway_pid_before"] = gateway_pid
    if gateway_pid is None:
        print("      NOTE: gateway PID unknown; process identity will be NOT_AVAILABLE")
    else:
        print(f"      Gateway PID: {gateway_pid}")
    try:
        print("[5/8] Resource sampling + stats phase")
        sampler = lib_resources.ResourceSampler(
            interval=1.0,
            out_path=out / "resources.ndjson",
            pprof_url=args.pprof_url,
            gateway_pid=gateway_pid,
            # Lets the sampler recover process identity after S10_RESTART bounces
            # the gateway and the original PID disappears.
            _pid_resolver=lambda: detect_gateway_pid(args.gateway_url),
        )
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
        elif is_s7:
            corr_interval = targets.get("corr_snapshot_interval_s", 0.25)
            stat_rows, stat_ids, corr_snapshots, step_hook_result = run_phase_s7(
                args.gateway_url,
                args.token,
                args.alias,
                mode,
                duration,
                args.concurrency,
                stream_ratio,
                token_mgr,
                routes,
                corr_interval,
                step_hook_cmd,
                step_at_ratio,
            )
            elapsed = time.time() - phase_started
            phase_marks = {"warmup_end": warmup_end_ts}
            step_at_ts = None
            window_metrics = None
        elif is_s8:
            stat_rows, stat_ids, memory_snapshots = run_phase_s8(
                args.gateway_url,
                args.token,
                args.alias,
                mode,
                duration,
                args.concurrency,
                stream_ratio,
                token_mgr,
                routes,
            )
            elapsed = time.time() - phase_started
            phase_marks = {"warmup_end": warmup_end_ts}
            step_at_ts = None
            step_hook_result = None
            window_metrics = None
        elif is_s9:
            affinity_ratio = targets.get("affinity_ratio", 0.0)
            stat_rows, stat_ids = run_phase_s9(
                args.gateway_url,
                args.token,
                args.alias,
                affinity_ratio,
                args.requests,
                args.concurrency,
                "stats",
            )
            elapsed = time.time() - phase_started
            phase_marks = {"warmup_end": warmup_end_ts}
            step_at_ts = None
            step_hook_result = None
            window_metrics = None
        elif is_s10 and args.replica_url:
            replica_urls = [args.gateway_url] + list(args.replica_url)
            stat_rows, stat_ids, per_replica_requests = run_phase_s10_global(
                replica_urls,
                args.token,
                args.alias,
                mode,
                args.requests,
                args.concurrency,
                "stats",
            )
            summary["replica_urls"] = replica_urls
            summary["per_replica_requests"] = per_replica_requests
            print(f"      S10 global: {len(replica_urls)} replicas, requests per replica={per_replica_requests}")
            elapsed = time.time() - phase_started
            phase_marks = {"warmup_end": warmup_end_ts}
            step_at_ts = None
            step_hook_result = None
            window_metrics = None
        elif is_s10 and args.scenario == "S10_RESTART":
            stat_rows, stat_ids, ks_before, ks_after, sweep_evidence = run_phase_s10_restart(
                args.gateway_url,
                args.token,
                args.alias,
                mode,
                args.requests,
                args.concurrency,
                token_mgr,
                routes,
                args.restart_hook,
                targets.get("kill_switch_option", "RouteStatsEnabled"),
                out,
            )
            summary["kill_switch_before"] = ks_before
            summary["kill_switch_after"] = ks_after
            summary["sweep_evidence"] = sweep_evidence
            elapsed = time.time() - phase_started
            phase_marks = {"warmup_end": warmup_end_ts}
            step_at_ts = None
            step_hook_result = None
            window_metrics = None
        elif is_s11:
            per_path = targets.get("min_per_path", 100)
            probe_count = targets.get("min_probe", 20)
            stat_rows, stat_ids, s11_path_counts, s11_probes, s11_affinity_pin = run_phase_s11(
                args.gateway_url,
                args.token,
                args.alias,
                per_path,
                probe_count,
                args.concurrency,
                args.specific_channel_id,
                token_mgr,
            )
            elapsed = time.time() - phase_started
            phase_marks = {"warmup_end": warmup_end_ts}
            step_at_ts = None
            step_hook_result = None
            window_metrics = None
        elif is_s12:
            stat_rows, stat_ids = run_phase_s12(
                args.gateway_url,
                args.token,
                args.alias,
                mode,
                args.requests,
                args.concurrency,
                "stats",
            )
            elapsed = time.time() - phase_started
            phase_marks = {"warmup_end": warmup_end_ts}
            step_at_ts = None
            step_hook_result = None
            window_metrics = None
        elif is_s13:
            recovery_deadline = (
                args.recovery_deadline_seconds
                if args.recovery_deadline_seconds is not None
                else targets.get("recovery_deadline_s", 300)
            )
            stat_rows, stat_ids, s13_samples, s13_hooks = run_phase_s13(
                args.gateway_url,
                args.token,
                args.alias,
                mode,
                args.concurrency,
                token_mgr,
                routes,
                targets["subject"],
                args.steady_seconds,
                args.fault_seconds,
                recovery_deadline,
                targets.get("sample_interval_s", 10),
                args.fault_hook,
                args.recover_hook,
                targets.get("target", 0.5),
                targets.get("recovery_tolerance_pp", 0.03),
            )
            elapsed = time.time() - phase_started
            phase_marks = {"warmup_end": warmup_end_ts}
            step_at_ts = None
            step_hook_result = s13_hooks
            window_metrics = None
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
        # S6/S7/S9: restore original share window size
        if needs_window_switch and original_share_window_size is not None:
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
        summary["share_window_size_target"] = share_window_size if needs_window_switch else None
        # S12/S13: restore RetryTimes and UpstreamFailureThreshold
        if original_retry_times is not None:
            if set_option(args.gateway_url, token_mgr, "RetryTimes", original_retry_times):
                print(f"      RetryTimes restored to {original_retry_times}")
            else:
                print(f"      WARNING: Failed to restore RetryTimes to {original_retry_times}")
        if original_upstream_threshold is not None:
            if set_option(args.gateway_url, token_mgr, "UpstreamFailureThreshold", original_upstream_threshold):
                print(f"      UpstreamFailureThreshold restored to {original_upstream_threshold}")
            else:
                print(f"      WARNING: Failed to restore UpstreamFailureThreshold to {original_upstream_threshold}")
        summary["retry_times_applied"] = retry_times_applied
        summary["retry_times_original"] = original_retry_times
        summary["upstream_failure_threshold_applied"] = upstream_threshold_applied
        summary["upstream_failure_threshold_original"] = original_upstream_threshold
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
    options_after = fetch_options(args.gateway_url, token_mgr)
    if options_after is not None:
        (out / "options-after.json").write_text(
            json.dumps(options_after, indent=2, ensure_ascii=False, sort_keys=True), encoding="utf-8"
        )
        # Surface exactly which options the run changed, so "restored" is a
        # checkable claim rather than a log line.
        if options_before is not None:
            changed = {
                k: {"before": options_before.get(k), "after": v}
                for k, v in options_after.items()
                if options_before.get(k) != v
            }
            summary["options_changed"] = changed
            print(f"      options-after.json written; {len(changed)} option(s) differ from before")
    else:
        print("      WARNING: could not snapshot options after the run")
    routes_after = build_route_infos(items_after) if items_after else routes
    stat_id_set = set(stat_ids)
    # S10_RESTART deliberately restarts the gateway mid-run, which clears the
    # in-process audit ring. Its pre-restart requests can never reconcile, so
    # scope the join to post-restart traffic instead of reporting an expected
    # restart property as missing data. The exclusion is recorded in the summary.
    reconcile_excluded_ids: list[str] = []
    sweep_evidence_s10 = summary.get("sweep_evidence") or {}
    if is_s10 and sweep_evidence_s10:
        reconcile_excluded_ids = list(sweep_evidence_s10.get("pre_restart_request_ids") or [])
        if reconcile_excluded_ids:
            stat_id_set -= set(reconcile_excluded_ids)
            summary["reconcile_scope"] = {
                "excluded_request_count": len(reconcile_excluded_ids),
                "reason": sweep_evidence_s10.get("audit_ring_reset_note"),
            }
    # The relay records its attempt after the response body is flushed, so the
    # last few requests can still be in flight when the client returns. Poll
    # until the audit ring carries every stat-phase id, or the budget expires.
    settle_deadline = time.time() + 15.0
    attempts_all: list[dict[str, Any]] | None = None
    # With replicas, each keeps its own in-process ring, so the global picture
    # needs every replica's audit — tagged with which one it came from.
    audit_urls = [args.gateway_url] + list(args.replica_url or [])
    while True:
        if len(audit_urls) > 1:
            attempts_all, per_pod_audit = fetch_audit_multi(audit_urls, token_mgr)
            summary["per_pod_audit_rows"] = per_pod_audit
        else:
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

    # Cross-side reconciliation needs the upstream to echo a correlation id. The
    # gateway forwards one only when the channel's header_override carries
    # {client_header:X-Request-Id} (relay/channel/api_request.go:
    # applyHeaderOverridePlaceholders -> applyHeaderOverrideToRequest); the relay
    # does not forward it by default. Verified on a live gateway: with the override
    # configured the mock records the client id, without it every row is empty.
    # An unconfigured rig therefore makes the join structurally impossible, which
    # is a rig configuration gap, not a data-integrity failure and not a product
    # limitation.
    upstream_ids = {str(r.get("request_id") or "") for r in upstream_rows}
    if upstream_rows and upstream_ids == {""}:
        summary["upstream_correlation_unavailable"] = (
            f"all {len(upstream_rows)} upstream rows carry an empty request_id: no channel in this "
            "topology forwards the client X-Request-Id upstream, so gateway-to-mock reconciliation "
            "cannot match. Audit-side identity checks remain valid. Fix the rig by setting each "
            'channel header_override to {"X-Request-Id": "{client_header:X-Request-Id}"} '
            "(README 环境前提); this needs no production change."
        )

    print("[7/8] Reconciling")
    # S6: skip reconcile expected_requests check (duration-based, not count-based).
    # The expected count must follow the scoped id set, otherwise S10_RESTART's
    # deliberately excluded pre-restart batch would still fail the count check.
    expected_for_reconcile = args.requests if args.requests is not None else 0
    if reconcile_excluded_ids:
        expected_for_reconcile = len(stat_id_set)
    rec = lib_reconcile.reconcile(
        attempts, upstream_rows,
        expected_requests=expected_for_reconcile,
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

    # expected_share: each route's entitlement from the scheduler's own post-run
    # final_score, normalised across the pool. Falls back to base_weight when
    # final_score is unavailable, and to an equal split when neither is usable,
    # so the column is always populated rather than silently empty.
    def _expected_shares() -> dict[tuple, float]:
        scores: dict[tuple, float] = {}
        for r in routes:
            snap = after_by_identity.get(r.identity(), r)
            score = snap.final_score if snap.final_score else snap.base_weight
            scores[r.identity()] = float(score or 0.0)
        total = sum(scores.values())
        if total <= 0:
            equal = (1.0 / len(routes)) if routes else 0.0
            return {k: equal for k in scores}
        return {k: v / total for k, v in scores.items()}

    expected_by_identity = _expected_shares()

    share_rows: list[dict[str, Any]] = []
    share_eval: dict[str, dict[str, Any]] = {}
    # Some scenarios name an aggregate subject rather than a route: S6 "STABLE",
    # S7 "CORR", S8 "MEMORY", S10_RESTART "KILLSWITCH", S11 "PATHS". Those judge a
    # whole-pool property, so the per-route subject falls back to "A" and the real
    # verdict comes from the scenario's own evaluator.
    AGGREGATE_SUBJECTS = {"STABLE", "CORR", "MEMORY", "KILLSWITCH", "PATHS"}
    subject_route_label = "A" if targets["subject"] in AGGREGATE_SUBJECTS else targets["subject"]
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
            "expected_share": expected_by_identity.get(r.identity()),
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
        # Only point-estimate scenarios have a numeric target; process/aggregate
        # scenarios (S6/S7/S8/S10_RESTART/S11) carry target=None and are judged by
        # their own evaluator, so calling evaluate_share here would compare to None.
        if is_subject and targets.get("target") is not None and targets.get("tol_pp") is not None:
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

    # #418 done-when 4: always emit BOTH the per-pod breakdown and the global
    # share re-aggregated from raw attempt quadruples. Single-replica runs report
    # one pod so the shape is identical at any replica count, and the global
    # number is never a mean of pod percentages.
    per_route_global: dict[str, dict[str, Any]] = {}
    for r in routes:
        agg = lib_stats.aggregate_global_share(attempts, r.identity())
        per_route_global[r.label] = {
            "global_share": agg["global_share"],
            "global_selections": agg["global_selections"],
            "global_total": agg["global_total"],
            "per_pod": agg["per_pod"],
            "pods_seen": agg["pods_seen"],
        }
    summary["per_route_global_aggregation"] = per_route_global
    summary["pods_seen"] = sorted({str(a.get("pod", "single")) for a in attempts}) if attempts else []
    summary["aggregation_note"] = (
        "global_share is one division over pooled raw attempts; per_pod is reporting only. "
        "Averaging per-pod percentages is forbidden by #418 and is not done anywhere here."
    )

    # S5: compute healthy_share_sum (sum of GOOD routes' observed shares)
    if is_s5:
        healthy_share_sum = sum(
            row["observed_share"] for row in share_rows if row["route"].startswith("GOOD")
        )
        summary["healthy_share_sum"] = healthy_share_sum
        print(f"      S5 healthy_share_sum (sum of GOOD routes): {healthy_share_sum:.4f}")

    # Pass audit rows so the retry count is real, and the intended request count so
    # generator saturation (dropped iterations) is visible.
    summary["service_quality"] = service_quality(stat_rows, elapsed, attempts=attempts, requested=args.requests)
    # Track user requests vs total attempts for S4
    summary["user_requests"] = args.requests
    summary["total_attempts"] = opportunities
    resource_rows = read_ndjson(out / "resources.ndjson")
    windows = lib_resources.aggregate_windows(resource_rows, window_s=10)
    summary["resources"] = {
        "samples": len(resource_rows),
        "windows": len(windows),
        # aggregate_windows() nests as {"cpu": {"avg": {...}, "max": {...}}, "mem": ...}.
        # The old flat keys (cpu_user_max / mem_rss_max) never existed, so every peak
        # silently reported 0 — which is precisely the resource evidence #418 asks for.
        "peak_cpu_user": max((w.get("cpu", {}).get("max", {}).get("user", 0) or 0 for w in windows), default=None),
        "peak_cpu_system": max((w.get("cpu", {}).get("max", {}).get("system", 0) or 0 for w in windows), default=None),
        "peak_cpu_iowait": max((w.get("cpu", {}).get("max", {}).get("iowait", 0) or 0 for w in windows), default=None),
        "peak_rss": max((w.get("mem", {}).get("max", {}).get("rss", 0) or 0 for w in windows), default=None),
        "peak_disk_write_bps": max((w.get("disk", {}).get("max", {}).get("write_bps", 0) or 0 for w in windows), default=None),
        "peak_net_error": max((w.get("net", {}).get("max", {}).get("error", 0) or 0 for w in windows), default=None),
        "peak_net_retransmit": max((w.get("net", {}).get("max", {}).get("retransmit", 0) or 0 for w in windows), default=None),
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
    elif traffic_windows_data:
        # #418 done-when 7: every scenario must fill the per-window route share /
        # corr / EWMA columns, not just the ones that poll topology live (S6/S7).
        # Only S6/S7 sample the gateway mid-run, so for the rest the per-window
        # share is recomputed from the audit attempts bucketed by their request's
        # own window, and corr/EWMA are the post-run per-route values — labelled as
        # such so nobody mistakes them for a mid-window sample.
        end_ts_by_id = {
            r.get("request_id"): r.get("end_ts")
            for r in stat_rows
            if r.get("request_id") and r.get("end_ts") is not None
        }
        label_by_identity = {r.identity(): r.label for r in routes}
        window_s = 10
        per_window_counts: dict[int, dict[str, int]] = {}
        for a in attempts:
            ts = end_ts_by_id.get(a.get("client_request_id"))
            if ts is None:
                continue
            label = label_by_identity.get(
                (a.get("channel_id"), a.get("key_index"), a.get("upstream_model"))
            )
            if not label:
                continue
            bucket = int(ts // window_s) * window_s
            per_window_counts.setdefault(bucket, {})[label] = (
                per_window_counts.setdefault(bucket, {}).get(label, 0) + 1
            )

        post_corr = {r.label: r.share_correction for r in routes_after}
        post_ewma = {r.label: r.ewma_quality for r in routes_after}
        corr_values = [v for v in post_corr.values() if v]
        post_corr_p99 = compute_percentile(corr_values, 0.99) if corr_values else ""

        for tw in traffic_windows_data:
            counts = per_window_counts.get(tw["window_start"])
            if counts:
                total = sum(counts.values())
                tw["route_shares"] = json.dumps(
                    {k: (v / total) for k, v in sorted(counts.items())}
                )
            # corr/EWMA are end-of-run values repeated per window: the gateway is
            # not polled mid-run outside S6/S7, and inventing a per-window value
            # would be fabrication.
            tw["corr_p99"] = post_corr_p99
            tw["ewma"] = json.dumps(post_ewma)
        summary["window_metrics_note"] = (
            "route_shares is recomputed per 10s window from audit attempts joined to each "
            "request's completion time. corr_p99 and ewma are POST-RUN per-route values repeated "
            "across windows, because only S6/S7 poll the gateway mid-run; they are not mid-window "
            "samples and must not be read as a time series."
        )

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
    elif is_s7:
        # S7: corr headroom evaluation
        corr_result = lib_stats.evaluate_corr_headroom(corr_snapshots, targets)
        summary["corr_evaluation"] = corr_result
        summary["step_hook_result"] = step_hook_result
        print(f"      S7 corr: p99={corr_result['corr_p99']:.3f} headroom={corr_result['headroom']:.2%} "
              f"snapshots={corr_result['total_snapshots']} per_route={corr_result['per_route_counts']}")
        if rec.verdict != "PASS":
            verdict, code = "DATA_INVALID", 1
        elif not corr_result["ok"]:
            # DATA_INVALID if insufficient snapshots, PRODUCT_FAIL if headroom fails
            if any("DATA_INVALID" in r for r in corr_result["reasons"]):
                verdict, code = "DATA_INVALID", 1
            else:
                verdict, code = "PRODUCT_FAIL", 1
        else:
            verdict, code = "PASS", 0
    elif is_s8:
        # S8: memory scaling evaluation
        mem_result = lib_stats.evaluate_memory_scaling(memory_snapshots, targets)
        summary["memory_evaluation"] = mem_result
        print(f"      S8 memory: heap_peak={mem_result['heap_peak']} rss_peak={mem_result['rss_peak']} "
              f"pool_match={mem_result['active_pool_match']} growth={mem_result['monotonic_growth']}")
        if rec.verdict != "PASS":
            verdict, code = "DATA_INVALID", 1
        elif not mem_result["ok"]:
            verdict, code = "PRODUCT_FAIL", 1
        else:
            verdict, code = "PASS", 0
    elif is_s9:
        # S9: affinity ratio scan — single combo per run
        # share_eval already computed for subject route A
        subject_row = next(r for r in share_rows if r["route"] == "A")
        affinity_ratio = targets.get("affinity_ratio", 0.0)
        wsize = targets.get("share_window_size", 0)
        summary["s9_result"] = {
            "affinity_ratio": affinity_ratio,
            "window_size": wsize,
            "A_share": subject_row["observed_share"],
            "A_ci_low": subject_row["ci_low"],
            "A_ci_high": subject_row["ci_high"],
            "A_selections": subject_row["selections"],
        }
        print(f"      S9: affinity={affinity_ratio:.0%} W={wsize} A_share={subject_row['observed_share']:.4f}")
        if rec.verdict != "PASS" or effective < args.requests:
            verdict, code = "DATA_INVALID", 1
        elif not subject_ok:
            verdict, code = "PRODUCT_FAIL", 1
        else:
            verdict, code = "PASS", 0
    elif is_s10:
        # S10: global share must be re-aggregated from raw attempts. Averaging
        # per-pod percentages is what #418 forbids and this branch avoids.
        subject_route = next((r for r in routes if r.label == targets["subject"]), None)
        agg = lib_stats.aggregate_global_share(
            attempts, subject_route.identity() if subject_route else (None, None, None)
        )
        summary["global_share"] = agg
        print(f"      S10 global: share={agg['global_share']:.4f} "
              f"({agg['global_selections']}/{agg['global_total']}) pods={agg['pods_seen']}")

        if args.kill_switch or args.scenario == "S10_RESTART":
            ks = lib_stats.evaluate_kill_switch(
                summary.get("kill_switch_before", {}),
                summary.get("kill_switch_after", {}),
                targets,
            )
            summary["kill_switch_evaluation"] = ks
            print(f"      S10 kill switch: {ks['reasons'] or 'ok'}")
            if rec.verdict != "PASS":
                verdict, code = "DATA_INVALID", 1
            elif any("DATA_INVALID" in r for r in ks["reasons"]):
                verdict, code = "DATA_INVALID", 1
            elif not ks["ok"]:
                verdict, code = "PRODUCT_FAIL", 1
            else:
                verdict, code = "PASS", 0
        else:
            # Race / global-share sub-runs judge the re-aggregated global number.
            target_share = targets.get("target")
            tol = targets.get("tol_pp", 0.03)
            within = target_share is None or abs(agg["global_share"] - target_share) <= tol
            if rec.verdict != "PASS":
                verdict, code = "DATA_INVALID", 1
            elif agg["global_total"] < targets.get("min_samples", 0):
                verdict, code = "DATA_INVALID", 1
                summary["sample_size_warning"] = (
                    f"global attempts {agg['global_total']} below required "
                    f"{targets.get('min_samples')}"
                )
            elif not within:
                verdict, code = "PRODUCT_FAIL", 1
            else:
                verdict, code = "PASS", 0
    elif is_s11:
        # S11: path labels, probe isolation, bypass-in-window.
        window_paths: list[str] = []
        probe_opportunities = 0
        shares_snapshot = fetch_share_snapshot(args.gateway_url, token_mgr)
        if shares_snapshot is not None:
            window_paths, probe_opportunities = summarise_window_paths(shares_snapshot, attempts)
        path_result = lib_stats.evaluate_path_audit(
            attempts, probe_opportunities, window_paths, targets
        )
        summary["path_audit"] = path_result
        # Naming matters here: per_path_counts inside path_audit comes from the
        # GATEWAY audit attempts and is the evidence. s11_client_intended_requests
        # is what the client meant to send and is corroboration only — #418 forbids
        # substituting it for the audit-side label.
        summary["s11_client_intended_requests"] = s11_path_counts
        summary["s11_path_evidence_source"] = (
            "path_audit.per_path_counts is derived from gateway audit attempt.path; "
            "s11_client_intended_requests is client-side intent and is not evidence."
        )
        summary["s11_probes_sent"] = s11_probes
        summary["s11_window_paths"] = sorted(set(window_paths))
        summary["s11_affinity_pin"] = {
            "requests": len(s11_affinity_pin),
            "statuses": [r.get("status") for r in s11_affinity_pin],
            "note": (
                "Serial pin-establishing request(s) sent before the measured batch and excluded "
                "from it. The first request for a prompt_cache_key has no affinity entry to stick "
                "to, so middleware/distributor.go labels it 'weighted' while creating the pin; "
                "without this warm-up a concurrent batch of N yields fewer than N affinity-labelled "
                "attempts."
            ),
        }
        print(f"      S11 paths: attempts={path_result['per_path_counts']} "
              f"probes_sent={s11_probes} probe_opportunities={probe_opportunities} "
              f"window_paths={sorted(set(window_paths))}")
        if rec.verdict != "PASS":
            verdict, code = "DATA_INVALID", 1
        elif any("DATA_INVALID" in r for r in path_result["reasons"]):
            verdict, code = "DATA_INVALID", 1
        elif not path_result["ok"]:
            verdict, code = "PRODUCT_FAIL", 1
        else:
            verdict, code = "PASS", 0
    elif is_s12:
        # S12: did the retry-absorbed failure reach quality AND the window?
        subject_label_s12 = targets["subject"]
        subject_route = next((r for r in routes if r.label == subject_label_s12), None)
        subject_after = next((r for r in routes_after if r.label == subject_label_s12), None)
        subject_identity = subject_route.identity() if subject_route else (None, None, None)

        b_attempts = [
            a for a in attempts
            if (a.get("channel_id"), a.get("key_index"), a.get("upstream_model")) == subject_identity
        ]
        # outcome 0 == success; anything else is a failed attempt against B.
        b_attempt_failures = sum(1 for a in b_attempts if a.get("outcome") not in (0, None))
        b_user_failures = sum(1 for r in stat_rows if r.get("status") not in (200, None))

        # Window evidence: a counting bound, not an alignment. Only a selection
        # creates a window slot, so at most B's successful attempts can account for
        # B's slots and the remainder must be failed attempts:
        #   failed_attempts_in_window >= b_window_slots_final - b_successes
        #
        # Positional alignment was tried and rejected: the window is ordered by
        # selection time and the audit ring by completion time, so under concurrency
        # "the last N of each" are different event sets and the ratio exceeded 1.0
        # (measured 1.8 and 2.67 against this gateway).
        window_snapshot = fetch_share_snapshot(args.gateway_url, token_mgr) or []
        window_total_slots = 0
        b_window_slots_final = 0
        for pool_snap in window_snapshot:
            for entry in pool_snap.get("window", []) or []:
                selected = entry.get("selected") or {}
                window_total_slots += 1
                if (
                    selected.get("channel_id"),
                    selected.get("key_index"),
                    selected.get("upstream_model"),
                ) == subject_identity:
                    b_window_slots_final += 1

        attempt_stats = {
            "b_attempt_failures": b_attempt_failures,
            "b_user_failures": b_user_failures,
            "b_ewma_before": subject_route.ewma_quality if subject_route else 0.0,
            "b_ewma_after": subject_after.ewma_quality if subject_after else 0.0,
            "b_total_attempts": len(b_attempts),
            "b_window_slots_final": b_window_slots_final,
            "window_total_slots": window_total_slots,
            # sample_count is the EWMA sample counter, NOT the share window. Kept
            # only as a labelled diagnostic so nobody mistakes it for window
            # evidence again.
            "b_ewma_sample_count_after": subject_after.sample_count if subject_after else 0,
        }
        retry_result = lib_stats.evaluate_retry_attribution(attempt_stats, targets)
        summary["retry_attribution"] = retry_result
        summary["retry_attempt_stats"] = attempt_stats
        print(f"      S12 attribution={retry_result['attribution']} "
              f"quality={retry_result['quality_observed']} window={retry_result['window_observed']} "
              f"ewma_delta={retry_result['ewma_delta']:.4f} failures={b_attempt_failures} "
              f"window_slots={b_window_slots_final}/{window_total_slots} "
              f"failed_in_window_min={retry_result['failed_attempts_in_window_min']}")
        if rec.verdict != "PASS":
            verdict, code = "DATA_INVALID", 1
        elif any("DATA_INVALID" in r for r in retry_result["reasons"]):
            verdict, code = "DATA_INVALID", 1
        elif not retry_result["ok"]:
            verdict, code = "PRODUCT_FAIL", 1
        else:
            verdict, code = "PASS", 0
    elif is_s13:
        # S13: corr stayed clamped through degradation, and share recovered.
        recovery_result = lib_stats.evaluate_recovery(s13_samples, targets)
        summary["recovery_evaluation"] = recovery_result
        summary["s13_samples"] = s13_samples
        summary["s13_hooks"] = s13_hooks
        print(f"      S13 recovery: recovered={recovery_result['recovered']} "
              f"seconds={recovery_result['recovery_seconds']} "
              f"corr_in_clamp={recovery_result['corr_in_clamp']} "
              f"steady={recovery_result['steady_share']} fault={recovery_result['fault_share']}")
        if rec.verdict != "PASS":
            verdict, code = "DATA_INVALID", 1
        elif any("DATA_INVALID" in r for r in recovery_result["reasons"]):
            verdict, code = "DATA_INVALID", 1
        elif not recovery_result["ok"]:
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

    # Host saturation outranks every scheduler conclusion: if the gateway shed load
    # on its own CPU/memory/disk thresholds, a meaningful fraction of the intended
    # traffic never reached selection at all, so neither PASS nor PRODUCT_FAIL is
    # honest. #418 requires generator/host saturation to preserve the data but block
    # a product verdict.
    shed = summary["service_quality"].get("gateway_shed_load_503") or 0
    shed_share = shed / len(stat_rows) if stat_rows else 0.0
    if shed:
        summary["gateway_shed_load"] = {
            "count": shed,
            "share_of_requests": shed_share,
            "breakdown": summary["service_quality"].get("gateway_shed_load_breakdown"),
            "cause": (
                "middleware/performance.go SystemPerformanceCheck rejected these requests before "
                "relay, using performance_setting.monitor_cpu/memory/disk_threshold. The upstream "
                "never saw them. Lower --concurrency or raise the thresholds for the run; do not "
                "attribute this to upstream mock capacity."
            ),
        }
        if shed_share > 0.01 and verdict == "PASS":
            verdict, code = "ENVIRONMENT_INVALID", 2
            summary["gateway_shed_load"]["verdict_override"] = (
                f"{shed_share:.1%} of requests were shed by the gateway's own overload check, so a "
                "PASS would rest on traffic that never reached the scheduler."
            )

    summary["verdict"] = verdict
    summary["admin_token_refreshes"] = token_mgr.refresh_count

    lib_report.write_summary(out / "summary.json", summary)
    (out / "report.md").write_text(lib_report.render_report_md(summary), encoding="utf-8")

    subject_row = next((r for r in share_rows if r["route"] == subject_route_label), None)
    if is_s6:
        print(f"      Subject {subject_route_label}: share={subject_row['observed_share']:.4f} "
              f"(process stability, no point-estimate target)")
        print(f"      Process stability: {summary.get('process_stability', {})}")
        print(f"      Step follow seconds: {summary.get('step_follow_seconds')}")
        print(f"      Step hook: {summary.get('step_hook_result')}")
    elif is_s7:
        print(f"      S7 corr: {summary.get('corr_evaluation', {})}")
    elif is_s8:
        print(f"      S8 memory: {summary.get('memory_evaluation', {})}")
    elif is_s9:
        print(f"      S9 result: {summary.get('s9_result', {})}")
    elif is_s10:
        print(f"      S10 global share: {summary.get('global_share', {})}")
        if summary.get("kill_switch_evaluation"):
            print(f"      S10 kill switch: {summary.get('kill_switch_evaluation')}")
    elif is_s11:
        print(f"      S11 path audit: {summary.get('path_audit', {})}")
    elif is_s12:
        print(f"      S12 retry attribution: {summary.get('retry_attribution', {})}")
    elif is_s13:
        print(f"      S13 recovery: {summary.get('recovery_evaluation', {})}")
        print(f"      S13 hooks: {summary.get('s13_hooks', {})}")
    elif subject_row:
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
