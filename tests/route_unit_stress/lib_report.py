#!/usr/bin/env python3
"""Report artifact writers for route-unit EWMA stress scenarios (S1–S3).

Pure Python stdlib — no numpy/pandas. Functions write CSV, JSON summary, and Markdown report.
"""
from __future__ import annotations

import csv
import json
import math
from pathlib import Path
from typing import Any

NOT_AVAILABLE = "NOT_AVAILABLE"


def _percentiles(values: list[float], ps: list[float]) -> dict[str, float]:
    """Local percentile computation (linear interpolation, NumPy default method).

    Avoids importing lib_stats to keep modules decoupled.
    """
    result: dict[str, float] = {}
    if not values:
        for p in ps:
            key = f"p{int(p)}" if p == int(p) else f"p{p}"
            result[key] = None
        result["max"] = None
        return result

    sorted_vals = sorted(values)
    n = len(sorted_vals)

    for p in ps:
        if p < 0 or p > 100:
            key = f"p{int(p)}" if p == int(p) else f"p{p}"
            result[key] = None
            continue
        idx = (n - 1) * p / 100.0
        lo = int(math.floor(idx))
        hi = int(math.ceil(idx))
        if lo == hi:
            val = sorted_vals[lo]
        else:
            weight = idx - lo
            val = sorted_vals[lo] * (1 - weight) + sorted_vals[hi] * weight
        key = f"p{int(p)}" if p == int(p) else f"p{p}"
        result[key] = val

    result["max"] = sorted_vals[-1]
    return result


def build_traffic_windows(
    request_rows: list[dict[str, Any]],
    resource_windows: list[dict[str, Any]],
    window_s: int = 10,
    phase_marks: dict[str, float] | None = None,
) -> list[dict[str, Any]]:
    """Aggregate request rows and resource windows into fixed time buckets.

    Args:
        request_rows: List of request dicts with keys:
            request_id, phase, stream, mode, status, latency_ms, ttft_ms,
            itl_ms, error, start_ts, end_ts (epoch seconds).
            If start_ts missing but end_ts + latency_ms present, start_ts is inferred.
            Rows missing both start_ts and (end_ts + latency_ms) are skipped.
        resource_windows: List of resource window dicts with window_start and
            nested cpu/mem/disk/net metrics (same structure as existing windows).
        window_s: Window size in seconds (default 10).
        phase_marks: Optional dict with keys "warmup_end", "step_at", "cooldown_start"
            (epoch seconds). Used to label each window's phase.

    Returns:
        List of window dicts with keys:
            window_start, window_end, arrivals, completions, rps, success, failed,
            p50, p95, p99, samples, cpu_user, rss, disk_write_bps, net_err, phase.
    """
    # Bucket requests by window
    arrivals_by_window: dict[int, list[dict]] = {}
    completions_by_window: dict[int, list[dict]] = {}

    def _is_success(status: Any) -> bool:
        """Check if status indicates success.
        Accepts integer 200, string '200', or string 'success' (case-insensitive).
        """
        if status is None:
            return False
        try:
            return int(status) == 200
        except (ValueError, TypeError):
            return str(status).strip().lower() == "success"

    for row in request_rows:
        start_ts = row.get("start_ts")
        end_ts = row.get("end_ts")
        latency_ms = row.get("latency_ms", 0)

        # Infer start_ts if missing
        if start_ts is None and end_ts is not None and latency_ms is not None:
            start_ts = end_ts - latency_ms / 1000.0

        if start_ts is None:
            # Cannot determine arrival window, skip
            continue

        # Arrival window by start_ts
        arrival_window = int(math.floor(start_ts / window_s)) * window_s
        if arrival_window not in arrivals_by_window:
            arrivals_by_window[arrival_window] = []
        arrivals_by_window[arrival_window].append(row)

        # Completion window by end_ts
        if end_ts is not None:
            completion_window = int(math.floor(end_ts / window_s)) * window_s
            if completion_window not in completions_by_window:
                completions_by_window[completion_window] = []
            completions_by_window[completion_window].append(row)

    # Determine all window start times that have data
    all_window_starts = set(arrivals_by_window.keys()) | set(completions_by_window.keys())
    if resource_windows:
        for rw in resource_windows:
            ws = rw.get("window_start")
            if ws is not None:
                all_window_starts.add(int(ws))

    if not all_window_starts:
        return []

    # Build resource lookup by window_start
    resource_by_window: dict[int, dict] = {}
    for rw in resource_windows:
        ws = rw.get("window_start")
        if ws is not None:
            resource_by_window[int(ws)] = rw

    # Phase labeling helper
    def get_phase_label(ws: int) -> str:
        if phase_marks is None:
            return "steady"
        we = ws + window_s
        # Window overlaps with phase boundaries; label by window midpoint
        mid = ws + window_s / 2.0
        if mid < phase_marks.get("warmup_end", float("inf")):
            return "warmup"
        elif mid < phase_marks.get("step_at", float("inf")):
            return "steady"
        elif mid < phase_marks.get("cooldown_start", float("inf")):
            return "fault"  # step_at to cooldown = fault/recovery phase
        else:
            return "cooldown"

    # Build output windows
    result = []
    for ws in sorted(all_window_starts):
        we = ws + window_s

        # Arrivals (by start_ts)
        arrival_rows = arrivals_by_window.get(ws, [])
        arrivals = len(arrival_rows)

        # Completions (by end_ts)
        completion_rows = completions_by_window.get(ws, [])
        completions = len(completion_rows)

        # Success/failed from completion rows
        success = sum(1 for r in completion_rows if _is_success(r.get("status")))
        failed = completions - success

        # Latency percentiles from successful completions
        success_latencies = [r.get("latency_ms", 0) for r in completion_rows if _is_success(r.get("status")) and r.get("latency_ms") is not None]
        pct = _percentiles(success_latencies, [50, 95, 99])

        # Resource merge
        rw = resource_by_window.get(ws, {})
        cpu_user = rw.get("cpu", {}).get("avg", {}).get("user", NOT_AVAILABLE)
        rss = rw.get("mem", {}).get("avg", {}).get("rss", NOT_AVAILABLE)
        disk_write_bps = rw.get("disk", {}).get("avg", {}).get("write_bps", NOT_AVAILABLE)
        net_err = rw.get("net", {}).get("avg", {}).get("error", NOT_AVAILABLE)

        result.append({
            "window_start": ws,
            "window_end": we,
            "arrivals": arrivals,
            "completions": completions,
            "rps": completions / window_s if window_s > 0 else 0.0,
            "success": success,
            "failed": failed,
            "p50": pct.get("p50"),
            "p95": pct.get("p95"),
            "p99": pct.get("p99"),
            "samples": completions,
            "cpu_user": cpu_user if cpu_user != NOT_AVAILABLE else "",
            "rss": rss if rss != NOT_AVAILABLE else "",
            "disk_write_bps": disk_write_bps if disk_write_bps != NOT_AVAILABLE else "",
            "net_err": net_err if net_err != NOT_AVAILABLE else "",
            "phase": get_phase_label(ws),
            "route_shares": "",
            "corr_p99": "",
            "ewma": "",
        })

    return result


def fill_per_window_route_shares(
    windows: list[dict[str, Any]],
    attempts: list[dict[str, Any]],
    end_ts_by_id: dict[str, float],
    label_by_identity: dict[tuple, str],
    post_corr_p99: float | str,
    post_ewma: dict[str, Any],
    window_s: int = 10,
) -> list[dict[str, Any]]:
    """Fill each window's route_shares/corr_p99/ewma columns from audit attempts.

    route_shares is recomputed per window by bucketing each attempt against its
    request's completion time and attributing it to the route identity that served
    it. corr_p99 and post_ewma are post-run per-route values repeated across
    windows, because only S6/S7 poll the gateway mid-run; they are NOT mid-window
    samples and must not be read as a time series.

    Returns the same list (mutated in place) for call-chaining convenience.
    """
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
        inner = per_window_counts.setdefault(bucket, {})
        inner[label] = inner.get(label, 0) + 1

    for tw in windows:
        counts = per_window_counts.get(tw["window_start"])
        if counts:
            total = sum(counts.values())
            tw["route_shares"] = json.dumps(
                {k: (v / total) for k, v in sorted(counts.items())}
            )
        tw["corr_p99"] = post_corr_p99
        tw["ewma"] = json.dumps(post_ewma)
    return windows

def write_shares_csv(path: Path, rows: list[dict[str, Any]]) -> None:
    """Write per-route share evaluation CSV.

    Columns: route,channel_id,key_index,upstream_model,selections,attempts,
    observed_share,ci_low,ci_high,target,tol_pp,base_weight,ewma_quality,
    health_multiplier,share_correction,final_score,sample_count

    Semantics: selections = times this route was chosen (stat-phase attempts for this route);
    attempts = total opportunities = sum of all routes' attempts in the stat phase.
    target/tol_pp are None for non-subject routes (written as empty columns).
    """
    path.parent.mkdir(parents=True, exist_ok=True)
    fieldnames = [
        "route",
        "channel_id",
        "key_index",
        "upstream_model",
        "selections",
        "attempts",
        "observed_share",
        # expected_share is the scheduler's own entitlement for this route
        # (base_weight normalised across the pool). Reported next to
        # observed_share so a reviewer can see the gap without recomputing it.
        "expected_share",
        "ci_low",
        "ci_high",
        "target",
        "tol_pp",
        "base_weight",
        "ewma_quality",
        "health_multiplier",
        "share_correction",
        "final_score",
        "sample_count",
    ]
    with path.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        for row in rows:
            out = {}
            for k in fieldnames:
                v = row.get(k)
                out[k] = "" if v is None else v
            writer.writerow(out)


def write_windows_csv(path: Path, windows: list[dict[str, Any]]) -> None:
    """Write 10-second aggregated windows CSV.

    Columns: window_start,arrivals,completions,rps,success,failed,p50,p95,p99,
    cpu_user,rss,disk_write_bps,net_err,window_end,samples,phase,route_shares,corr_p99,ewma
    """
    path.parent.mkdir(parents=True, exist_ok=True)
    fieldnames = [
        "window_start",
        "arrivals",
        "completions",
        "rps",
        "success",
        "failed",
        "p50",
        "p95",
        "p99",
        "cpu_user",
        "rss",
        "disk_write_bps",
        "net_err",
        "window_end",
        "samples",
        "phase",
        "route_shares",
        "corr_p99",
        "ewma",
    ]
    with path.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        for w in windows:
            cpu_user = w.get("cpu", {}).get("avg", {}).get("user", NOT_AVAILABLE)
            rss = w.get("mem", {}).get("avg", {}).get("rss", NOT_AVAILABLE)
            disk_write_bps = w.get("disk", {}).get("avg", {}).get("write_bps", NOT_AVAILABLE)
            net_err = w.get("net", {}).get("avg", {}).get("error", NOT_AVAILABLE)

            writer.writerow({
                "window_start": w.get("window_start", ""),
                "arrivals": w.get("arrivals", w.get("samples", 0)),
                "completions": w.get("completions", w.get("samples", 0)),
                "rps": w.get("rps", w.get("samples", 0) / 10.0 if w.get("samples", 0) else 0),
                "success": w.get("success", 0),
                "failed": w.get("failed", 0),
                "p50": w.get("p50", ""),
                "p95": w.get("p95", ""),
                "p99": w.get("p99", ""),
                "cpu_user": cpu_user if cpu_user != NOT_AVAILABLE else "",
                "rss": rss if rss != NOT_AVAILABLE else "",
                "disk_write_bps": disk_write_bps if disk_write_bps != NOT_AVAILABLE else "",
                "net_err": net_err if net_err != NOT_AVAILABLE else "",
                "window_end": w.get("window_end", ""),
                "samples": w.get("samples", w.get("completions", w.get("arrivals", 0))),
                "phase": w.get("phase", ""),
                "route_shares": w.get("route_shares", ""),
                "corr_p99": w.get("corr_p99", ""),
                "ewma": w.get("ewma", ""),
            })


def write_resources_csv(path: Path, rows: list[dict[str, Any]]) -> None:
    """Write raw resource samples CSV (flat keys)."""
    if not rows:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("")
        return

    # Collect all possible keys (flattened)
    all_keys = set()
    flat_rows = []
    for row in rows:
        flat = _flatten_dict(row)
        all_keys.update(flat.keys())
        flat_rows.append(flat)

    fieldnames = sorted(all_keys)
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", newline="", encoding="utf-8") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        for flat in flat_rows:
            writer.writerow(flat)


def _flatten_dict(d: dict[str, Any], prefix: str = "") -> dict[str, Any]:
    """Flatten nested dict with dot notation."""
    result = {}
    for k, v in d.items():
        key = f"{prefix}{k}"
        if isinstance(v, dict):
            result.update(_flatten_dict(v, f"{key}."))
        else:
            result[key] = v
    return result


def write_summary(path: Path, summary: dict[str, Any]) -> None:
    """Write JSON summary file."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(summary, indent=2, ensure_ascii=False))


def render_report_md(summary: dict[str, Any]) -> str:
    """Render Markdown report from summary dict."""
    lines: list[str] = []

    # Header
    lines.append(f"# Route-Unit EWMA Stress Scenario Report")
    lines.append("")
    lines.append(f"**Scenario:** {summary.get('scenario', 'N/A')}")
    lines.append(f"**Verdict:** {summary.get('verdict', 'N/A')}")
    lines.append("")
    # Sample size warning
    warning = summary.get("sample_size_warning")
    if warning:
        lines.append(f"> ⚠️ **{summary.get('verdict', 'N/A')}:** {warning}")
        lines.append("")

    # Config
    cfg = summary.get("config", {})
    lines.append("## Configuration")
    lines.append("")
    lines.append("| Parameter | Value |")
    lines.append("|-----------|-------|")
    for k, v in cfg.items():
        lines.append(f"| {k} | {v} |")
    lines.append("")

    # Reconciliation
    rec = summary.get("reconcile", {})
    lines.append("## Reconciliation")
    lines.append("")
    lines.append(f"**Verdict:** {rec.get('verdict', 'N/A')}")
    lines.append(f"**Total Requests:** {rec.get('total_requests', 0)}")
    lines.append(f"**Matched Pairs:** {rec.get('matched_pairs', 0)}")
    lines.append(f"**Missing in Audit:** {len(rec.get('missing_in_audit', []))}")
    lines.append(f"**Missing in Mock:** {len(rec.get('missing_in_mock', []))}")
    lines.append(f"**Identity Mismatches:** {len(rec.get('identity_mismatch', []))}")
    lines.append(f"**Attempt Gaps:** {len(rec.get('attempt_gaps', []))}")
    lines.append(f"**Scoped Records:** {rec.get('scoped_records', 0)}")
    lines.append("")

    # Share evaluation
    shares = summary.get("shares", [])
    share_eval = summary.get("share_eval", {})
    lines.append("## Share Evaluation")
    lines.append("")
    if shares:
        lines.append("| Route | Channel | Key | Model | Selections | Attempts | Observed | CI Low | CI High | Target | Tol ±pp | Base W | EWMA Q | Health | Share Corr | Final Score | Samples | Eval |")
        lines.append("|-------|---------|-----|-------|------------|----------|----------|--------|---------|--------|---------|--------|--------|--------|------------|-------------|---------|------|")
        for s in shares:
            route = s.get("route", "")
            eval_r = share_eval.get(route, {})
            eval_str = "OK" if eval_r.get("ok") else f"FAIL: {', '.join(eval_r.get('reasons', []))}"
            target_val = s.get("target")
            tol_val = s.get("tol_pp")
            target_str = f"{target_val:.3f}" if target_val is not None else "n/a"
            tol_str = f"{tol_val:.3f}" if tol_val is not None else "n/a"
            lines.append(
                f"| {route} | {s.get('channel_id', '')} | {s.get('key_index', '')} | "
                f"{s.get('upstream_model', '')} | {s.get('selections', 0)} | {s.get('attempts', 0)} | "
                f"{s.get('observed_share', 0):.4f} | {s.get('ci_low', 0):.4f} | {s.get('ci_high', 0):.4f} | "
                f"{target_str} | {tol_str} | {s.get('base_weight', '')} | "
                f"{s.get('ewma_quality', '')} | {s.get('health_multiplier', '')} | "
                f"{s.get('share_correction', '')} | {s.get('final_score', '')} | {s.get('sample_count', 0)} | {eval_str} |"
            )
    lines.append("")

    # Service quality
    sq = summary.get("service_quality", {})
    lines.append("## Service Quality")
    lines.append("")
    lines.append("| Metric | Value |")
    lines.append("|--------|-------|")
    for k, v in sq.items():
        if v is not None:
            if isinstance(v, float):
                lines.append(f"| {k} | {v:.2f} |")
            else:
                lines.append(f"| {k} | {v} |")
        else:
            lines.append(f"| {k} | N/A |")
    lines.append("")

    # Resource peaks
    res = summary.get("resources", {})
    lines.append("## Resource Peaks (10s windows)")
    lines.append("")
    lines.append("| Metric | Peak |")
    lines.append("|--------|------|")
    peak_cpu = res.get("peak_cpu_user")
    peak_rss = res.get("peak_rss")
    peak_disk = res.get("peak_disk_write_bps")
    peak_net_err = res.get("peak_net_error")
    cpu_str = f"{peak_cpu:.2f}" if peak_cpu is not None else "N/A"
    rss_str = str(peak_rss) if peak_rss is not None else "N/A"
    disk_str = str(peak_disk) if peak_disk is not None else "N/A"
    net_str = str(peak_net_err) if peak_net_err is not None else "N/A"
    lines.append(f"| CPU User % | {cpu_str} |")
    lines.append(f"| RSS (bytes) | {rss_str} |")
    lines.append(f"| Disk Write (B/s) | {disk_str} |")
    lines.append(f"| Net Errors | {net_str} |")

    # Process Windows (S6 stability)
    proc = summary.get("process_stability")
    if proc:
        lines.append("## Process Windows")
        lines.append("")
        lines.append(f"**Verdict:** {'OK' if proc.get('ok') else 'FAIL'}")
        lines.append(f"**Windows Analyzed:** {proc.get('windows_analyzed', 0)}")
        lines.append(f"**Insufficient Windows:** {proc.get('insufficient_windows', 0)}")
        lines.append(f"**Share StdDev:** {proc.get('share_stddev_pp', 0):.2f} pp")
        lines.append(f"**Max Consecutive Breach:** {proc.get('max_consecutive_breach', 0)}")
        lines.append(f"**Corr P99 Headroom:** {proc.get('corr_p99_headroom', 0):.2f}")
        lines.append("")
        reasons = proc.get("reasons", [])
        if reasons:
            lines.append("**Reasons:**")
            for r in reasons:
                lines.append(f"- {r}")
        else:
            lines.append("- All stability checks passed")
        lines.append("")

    # Window summary if present
    win_summary = summary.get("windows")
    if win_summary:
        lines.append("## Window Summary")
        lines.append("")
        lines.append(f"**Total Windows:** {win_summary.get('total_windows', 0)}")
        lines.append("")
        phase_dist = win_summary.get("phase_distribution", {})
        if phase_dist:
            lines.append("**Phase Distribution:**")
            for phase, count in phase_dist.items():
                lines.append(f"- {phase}: {count}")
            lines.append("")
        lines.append(f"**Max Window RPS:** {win_summary.get('max_rps', 0):.2f}")
        lines.append(f"**Min Window RPS:** {win_summary.get('min_rps', 0):.2f}")
        lines.append("")

     # Verdict details
    lines.append("## Verdict Details")
    lines.append("")
    reasons = summary.get("reasons", [])
    if reasons:
        for r in reasons:
            lines.append(f"- {r}")
    else:
        lines.append("- All checks passed")
    lines.append("")

    return "\n".join(lines)


if __name__ == "__main__":
    # Quick smoke test
    test_shares = [{
        "route": "A",
        "channel_id": 1,
        "key_index": 0,
        "upstream_model": "mock-ok",
        "selections": 48,
        "attempts": 100,
        "observed_share": 0.48,
        "ci_low": 0.385,
        "ci_high": 0.577,
        "target": 0.5,
        "tol_pp": 0.02,
        "base_weight": 100,
        "ewma_quality": 1.0,
        "health_multiplier": 1.0,
        "share_correction": 1.0,
        "final_score": 1.0,
        "sample_count": 10,
    }]
    write_shares_csv(Path("/tmp/test_shares.csv"), test_shares)
    print("Shares CSV written")

    test_windows = [{
        "window_start": 0,
        "window_end": 10,
        "samples": 5,
        "cpu": {"avg": {"user": 12.5}, "max": {"user": 15.0}},
        "mem": {"avg": {"rss": 1024000}, "max": {"rss": 1100000}},
        "disk": {"avg": {"write_bps": 1024}, "max": {"write_bps": 2048}},
        "net": {"avg": {"error": 0}, "max": {"error": 0}},
    }]
    write_windows_csv(Path("/tmp/test_windows.csv"), test_windows)
    print("Windows CSV written")

    test_resources = [{"ts": 1.0, "cpu": {"user": 10}}, {"ts": 2.0, "cpu": {"user": 12}}]
    write_resources_csv(Path("/tmp/test_resources.csv"), test_resources)
    print("Resources CSV written")

    test_summary = {"scenario": "S1", "verdict": "PASS", "config": {}, "reconcile": {}, "shares": [], "service_quality": {}, "resources": {}}
    write_summary(Path("/tmp/test_summary.json"), test_summary)
    print("Summary JSON written")

    md = render_report_md(test_summary)
    Path("/tmp/test_report.md").write_text(md)
    print("Report MD written")
    print("All smoke tests passed")