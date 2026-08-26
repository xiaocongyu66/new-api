#!/usr/bin/env python3
"""Report artifact writers for route-unit EWMA stress scenarios (S1–S3).

Pure Python stdlib — no numpy/pandas. Functions write CSV, JSON summary, and Markdown report.
"""
from __future__ import annotations

import csv
import json
from pathlib import Path
from typing import Any


NOT_AVAILABLE = "NOT_AVAILABLE"


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
    cpu_user,rss,disk_write_bps,net_err
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
                "arrivals": w.get("samples", 0),
                "completions": w.get("samples", 0),
                "rps": w.get("samples", 0) / 10.0 if w.get("samples", 0) else 0,
                "success": 0,
                "failed": 0,
                "p50": "",
                "p95": "",
                "p99": "",
                "cpu_user": cpu_user if cpu_user != NOT_AVAILABLE else "",
                "rss": rss if rss != NOT_AVAILABLE else "",
                "disk_write_bps": disk_write_bps if disk_write_bps != NOT_AVAILABLE else "",
                "net_err": net_err if net_err != NOT_AVAILABLE else "",
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