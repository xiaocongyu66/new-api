#!/usr/bin/env python3
"""Synthetic data self-tests for lib_report (S1–S3).

Assert-style tests verifying CSV headers/row counts, report.md verdict + CI numbers,
and summary missing-field error handling.
"""
from __future__ import annotations

import csv
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from lib_report import (
    write_shares_csv,
    write_windows_csv,
    write_resources_csv,
    write_summary,
    render_report_md,
    build_traffic_windows,
    fill_per_window_route_shares,
)


def make_synthetic_summary() -> dict:
    """Build a complete synthetic summary dict for S1."""
    return {
        "scenario": "S1",
        "verdict": "PASS",
        "reasons": [],
        "config": {
            "gateway_url": "http://localhost:3000",
            "alias": "test-alias",
            "warmup": 44,
            "requests": 100,
            "concurrency": 8,
            "stream_ratio": 0.2,
            "max_seconds": None,
        },
        "reconcile": {
            "verdict": "PASS",
            "total_requests": 100,
            "matched_pairs": 100,
            "missing_in_audit": [],
            "missing_in_mock": [],
            "duplicate_in_audit": [],
            "duplicate_in_mock": [],
            "identity_mismatch": [],
            "attempt_gaps": [],
            "total_records": 100,
            "scoped_records": 100,
        },
        "shares": [
            {
                "route": "A",
                "channel_id": 1,
                "key_index": 0,
                "upstream_model": "mock-ok",
                "selections": 48,
                "attempts": 100,
                "observed_share": 0.48,
                "ci_low": 0.3846,
                "ci_high": 0.5773,
                "target": 0.50,
                "tol_pp": 0.02,
                "base_weight": 100,
                "ewma_quality": 1.0,
                "health_multiplier": 1.0,
                "share_correction": 1.0,
                "final_score": 1.0,
                "sample_count": 10,
            },
            {
                "route": "B",
                "channel_id": 2,
                "key_index": 0,
                "upstream_model": "mock-ok",
                "selections": 52,
                "attempts": 100,
                "observed_share": 0.52,
                "ci_low": 0.4226,
                "ci_high": 0.6153,
                "target": None,
                "tol_pp": None,
                "base_weight": 100,
                "ewma_quality": 1.0,
                "health_multiplier": 1.0,
                "share_correction": 1.0,
                "final_score": 1.0,
                "sample_count": 10,
            },
        ],
        "share_eval": {
            "A": {"ok": True, "reasons": []},
            "B": {"ok": True, "reasons": []},
        },
        "service_quality": {
            "total_requests": 100,
            "success": 100,
            "failed": 0,
            "success_rate": 1.0,
            "latency_p50": 120.5,
            "latency_p95": 180.3,
            "latency_p99": 210.7,
            "ttft_p50": 50.2,
            "ttft_p95": 60.1,
            "ttft_p99": 65.3,
            "itl_p50": 10.0,
            "itl_p95": 15.0,
            "itl_p99": 20.0,
        },
        "resources": {
            "windows": [],
            "peak_cpu_user": 12.5,
            "peak_rss": 1100000,
            "peak_disk_write_bps": 2048,
            "peak_net_error": 0,
        },
    }


def make_synthetic_windows() -> list[dict]:
    """Build synthetic 10s windows."""
    return [
        {
            "window_start": 0,
            "window_end": 10,
            "samples": 5,
            "cpu": {"avg": {"user": 12.5}, "max": {"user": 15.0}},
            "mem": {"avg": {"rss": 1024000}, "max": {"rss": 1100000}},
            "disk": {"avg": {"write_bps": 1024}, "max": {"write_bps": 2048}},
            "net": {"avg": {"error": 0}, "max": {"error": 0}},
        },
        {
            "window_start": 10,
            "window_end": 20,
            "samples": 4,
            "cpu": {"avg": {"user": 10.0}, "max": {"user": 13.0}},
            "mem": {"avg": {"rss": 1000000}, "max": {"rss": 1050000}},
            "disk": {"avg": {"write_bps": 512}, "max": {"write_bps": 1024}},
            "net": {"avg": {"error": 1}, "max": {"error": 2}},
        },
    ]


def make_synthetic_resources() -> list[dict]:
    """Build synthetic resource samples."""
    return [
        {"ts": 1.0, "cpu": {"user": 10.0, "system": 2.0}, "mem": {"rss": 1024000}},
        {"ts": 2.0, "cpu": {"user": 12.0, "system": 3.0}, "mem": {"rss": 1100000}},
    ]


def test_shares_csv_headers_and_rows(tmp: Path) -> bool:
    """Shares CSV has correct headers and one row per route."""
    shares = make_synthetic_summary()["shares"]
    path = tmp / "shares.csv"
    write_shares_csv(path, shares)

    with path.open() as f:
        reader = csv.reader(f)
        headers = next(reader)
        rows = list(reader)

    expected_headers = [
        "route", "channel_id", "key_index", "upstream_model",
        "selections", "attempts", "observed_share", "expected_share",
        "ci_low", "ci_high",
        "target", "tol_pp", "base_weight", "ewma_quality",
        "health_multiplier", "share_correction", "final_score", "sample_count",
    ]
    assert headers == expected_headers, f"headers mismatch: {headers} != {expected_headers}"
    assert len(rows) == 2, f"expected 2 rows, got {len(rows)}"
    assert rows[0][0] == "A", f"first row route should be A, got {rows[0][0]}"
    assert rows[1][0] == "B", f"second row route should be B, got {rows[1][0]}"
    # A row lacking expected_share must leave the cell empty rather than crash or
    # emit "None": #418 reviewers read this CSV directly.
    exp_idx = expected_headers.index("expected_share")
    assert rows[0][exp_idx] == "", f"absent expected_share should be empty, got {rows[0][exp_idx]!r}"
    print("  OK: shares CSV headers and rows correct")
    return True

def test_windows_csv_headers_and_rows(tmp: Path) -> bool:
    """Windows CSV has correct headers and one row per window."""
    windows = make_synthetic_windows()
    path = tmp / "windows.csv"
    write_windows_csv(path, windows)

    with path.open() as f:
        reader = csv.reader(f)
        headers = next(reader)
        rows = list(reader)

    expected_headers = [
        "window_start", "arrivals", "completions", "rps", "success", "failed",
        "p50", "p95", "p99", "cpu_user", "rss", "disk_write_bps", "net_err",
        "window_end", "samples", "phase", "route_shares", "corr_p99", "ewma",
    ]
    assert headers == expected_headers, f"headers mismatch: {headers} != {expected_headers}"
    assert len(rows) == 2, f"expected 2 rows, got {len(rows)}"
    assert rows[0][0] == "0", f"first row window_start should be 0, got {rows[0][0]}"
    assert rows[1][0] == "10", f"second row window_start should be 10, got {rows[1][0]}"
    # Check new columns have values from synthetic data
    assert rows[0][headers.index("window_end")] == "10", f"window_end should be 10, got {rows[0][headers.index('window_end')]}"
    assert rows[1][headers.index("window_end")] == "20", f"window_end should be 20, got {rows[1][headers.index('window_end')]}"
    assert rows[0][headers.index("samples")] == "5", f"samples should be 5, got {rows[0][headers.index('samples')]}"
    assert rows[1][headers.index("samples")] == "4", f"samples should be 4, got {rows[1][headers.index('samples')]}"
    # phase, route_shares, corr_p99, ewma should be empty (not in synthetic data)
    assert rows[0][headers.index("phase")] == "", "phase should be empty for old format"
    assert rows[0][headers.index("route_shares")] == "", "route_shares should be empty for old format"
    assert rows[0][headers.index("corr_p99")] == "", "corr_p99 should be empty for old format"
    assert rows[0][headers.index("ewma")] == "", "ewma should be empty for old format"
    print("  OK: windows CSV headers and rows correct (with new columns)")
    return True


def test_resources_csv(tmp: Path) -> bool:
    """Resources CSV has rows for each sample."""
    resources = make_synthetic_resources()
    path = tmp / "resources.csv"
    write_resources_csv(path, resources)

    with path.open() as f:
        reader = csv.reader(f)
        headers = next(reader)
        rows = list(reader)

    assert len(rows) == 2, f"expected 2 rows, got {len(rows)}"
    assert "ts" in headers, f"ts missing from headers: {headers}"
    print("  OK: resources CSV rows correct")
    return True


def test_summary_json(tmp: Path) -> bool:
    """Summary JSON is valid and parseable."""
    summary = make_synthetic_summary()
    path = tmp / "summary.json"
    write_summary(path, summary)

    data = json.loads(path.read_text())
    assert data["scenario"] == "S1", f"scenario mismatch: {data['scenario']}"
    assert data["verdict"] == "PASS", f"verdict mismatch: {data['verdict']}"
    print("  OK: summary JSON valid")
    return True


def test_report_md_contains_verdict_and_ci(tmp: Path) -> bool:
    """Report.md contains the verdict and CI numbers."""
    summary = make_synthetic_summary()
    md = render_report_md(summary)

    path = tmp / "report.md"
    path.write_text(md)

    # Must contain verdict
    assert "PASS" in md, "verdict PASS not found in report.md"
    # Must contain CI numbers from shares (ci_low=0.3846, ci_high=0.5773)
    assert "0.3846" in md or "0.385" in md, f"ci_low not found in report.md"
    assert "0.5773" in md or "0.577" in md, f"ci_high not found in report.md"
    # Must contain scenario name
    assert "S1" in md, "scenario S1 not found in report.md"
    # Must contain share evaluation section
    assert "Share Evaluation" in md, "Share Evaluation section not found"
    # Must contain service quality section
    assert "Service Quality" in md, "Service Quality section not found"
    print("  OK: report.md contains verdict and CI numbers")
    return True


def test_report_md_product_fail(tmp: Path) -> bool:
    """Report.md renders PRODUCT_FAIL verdict correctly."""
    summary = make_synthetic_summary()
    summary["verdict"] = "PRODUCT_FAIL"
    summary["reasons"] = ["share A: point 0.6500 outside 0.500±0.02"]
    summary["share_eval"]["A"] = {"ok": False, "reasons": ["point 0.6500 outside 0.500±0.02"]}
    md = render_report_md(summary)

    assert "PRODUCT_FAIL" in md, "verdict PRODUCT_FAIL not found in report.md"
    assert "point 0.6500 outside 0.500±0.02" in md, "failure reason not found in report.md"
    print("  OK: report.md renders PRODUCT_FAIL verdict")
    return True


def test_report_md_underpowered(tmp: Path) -> bool:
    """Report.md renders UNDERPOWERED verdict with warning and sample sizes."""
    summary = make_synthetic_summary()
    summary["verdict"] = "UNDERPOWERED"
    summary["reasons"] = ["share A: CI out of bounds (underpowered: n=100 < min=13000)"]
    summary["sample_size_warning"] = "样本量低于判据可行下限 n=13000，CI 判据不可达，本次结果仅供结构验证"
    md = render_report_md(summary)

    assert "UNDERPOWERED" in md, "verdict UNDERPOWERED not found in report.md"
    assert "13000" in md, "min_samples not found in report.md"
    assert "100" in md, "actual n not found in report.md"
    assert "样本量低于判据可行下限" in md, "warning text not found in report.md"
    print("  OK: report.md renders UNDERPOWERED verdict with warning")
    return True


def make_synthetic_request_rows() -> list[dict]:
    """Build 25 synthetic request rows across 3 windows (0-10, 10-20, 20-30s).

    Window 0-10: 9 arrivals, 8 completions (1 completes in window 1), 1 failed
    Window 10-20: 8 arrivals, 8 completions, 1 failed
    Window 20-30: 8 arrivals, 7 completions, 1 failed
    Total: 25 rows, 3 failures.
    """
    rows = []
    rid = 0

    # Window 0-10: 9 requests start in window 0
    # 8 complete within window 0 (start_ts 1-8, end_ts = start+0.5)
    for i in range(8):
        rid += 1
        rows.append({
            "request_id": f"req-{rid:03d}",
            "phase": "steady",
            "stream": False,
            "mode": "mock-ok",
            "status": "success",
            "latency_ms": 500 + i * 10,
            "ttft_ms": 100,
            "itl_ms": 400,
            "error": "",
            "start_ts": float(i + 1),        # 1..8
            "end_ts": float(i + 1) + 0.5,     # 1.5..8.5
        })
    # 1 request starts in window 0 but completes in window 1
    rid += 1
    rows.append({
        "request_id": f"req-{rid:03d}",
        "phase": "steady",
        "stream": False,
        "mode": "mock-ok",
        "status": "success",
        "latency_ms": 2000,
        "ttft_ms": 100,
        "itl_ms": 1900,
        "error": "",
        "start_ts": 9.0,
        "end_ts": 11.0,   # completes in window 1
    })

    # Window 10-20: 8 requests start in window 1
    # 1 failed
    for i in range(8):
        rid += 1
        status = "failed" if i == 0 else "success"
        rows.append({
            "request_id": f"req-{rid:03d}",
            "phase": "steady",
            "stream": False,
            "mode": "mock-ok",
            "status": status,
            "latency_ms": 300 + i * 5,
            "ttft_ms": 80,
            "itl_ms": 220,
            "error": "" if status == "success" else "timeout",
            "start_ts": 11.0 + i,
            "end_ts": 11.0 + i + 0.3,
        })

    # Window 20-30: 8 requests start in window 2
    # 1 failed
    for i in range(8):
        rid += 1
        status = "failed" if i == 7 else "success"
        rows.append({
            "request_id": f"req-{rid:03d}",
            "phase": "steady",
            "stream": False,
            "mode": "mock-ok",
            "status": status,
            "latency_ms": 200 + i * 10,
            "ttft_ms": 50,
            "itl_ms": 150,
            "error": "" if status == "success" else "connection_reset",
            "start_ts": 21.0 + i,
            "end_ts": 21.0 + i + 0.2,
        })

    return rows


def test_build_traffic_windows_basics(tmp: Path) -> bool:
    """build_traffic_windows: bucketing, arrivals vs completions, success/failed, p50."""
    rows = make_synthetic_request_rows()
    windows = build_traffic_windows(rows, [], window_s=10)

    assert len(windows) == 3, f"expected 3 windows, got {len(windows)}"

    # Window 0-10: arrivals=9, completions=8 (1 completes in window 1)
    w0 = windows[0]
    assert w0["window_start"] == 0, f"w0 start={w0['window_start']}"
    assert w0["window_end"] == 10, f"w0 end={w0['window_end']}"
    assert w0["arrivals"] == 9, f"w0 arrivals={w0['arrivals']}, expected 9"
    assert w0["completions"] == 8, f"w0 completions={w0['completions']}, expected 8"
    assert w0["success"] == 8, f"w0 success={w0['success']}, expected 8"
    assert w0["failed"] == 0, f"w0 failed={w0['failed']}, expected 0"
    assert w0["samples"] == 8, f"w0 samples={w0['samples']}"
    assert w0["rps"] == 0.8, f"w0 rps={w0['rps']}, expected 0.8"

    # Window 10-20: arrivals=8, completions=9 (1 from window 0 completes here + 8 local)
    w1 = windows[1]
    assert w1["window_start"] == 10, f"w1 start={w1['window_start']}"
    assert w1["arrivals"] == 8, f"w1 arrivals={w1['arrivals']}, expected 8"
    assert w1["completions"] == 9, f"w1 completions={w1['completions']}, expected 9"
    assert w1["success"] == 8, f"w1 success={w1['success']}, expected 8 (1 failed)"
    assert w1["failed"] == 1, f"w1 failed={w1['failed']}, expected 1"

    # Window 20-30: arrivals=8, completions=8, 1 failed
    w2 = windows[2]
    assert w2["window_start"] == 20, f"w2 start={w2['window_start']}"
    assert w2["arrivals"] == 8, f"w2 arrivals={w2['arrivals']}, expected 8"
    assert w2["completions"] == 8, f"w2 completions={w2['completions']}, expected 8"
    assert w2["success"] == 7, f"w2 success={w2['success']}, expected 7"
    assert w2["failed"] == 1, f"w2 failed={w2['failed']}, expected 1"

    # p50 for window 0: 8 success latencies 500,510,...,570 → sorted, median interpolation
    # sorted: [500,510,520,530,540,550,560,570], n=8, idx=7*50/100=3.5 → (530+540)/2=535
    assert w0["p50"] == 535.0, f"w0 p50={w0['p50']}, expected 535.0"

    print("  OK: build_traffic_windows bucketing and counts correct")
    return True


def test_build_traffic_windows_phase_marks(tmp: Path) -> bool:
    """build_traffic_windows: phase_marks assigns correct phase per window."""
    rows = make_synthetic_request_rows()
    phase_marks = {
        "warmup_end": 5.0,      # before 5s = warmup
        "step_at": 15.0,        # 5-15s = steady, 15s+ = fault
        "cooldown_start": 25.0, # 15-25s = fault, 25s+ = cooldown
    }
    windows = build_traffic_windows(rows, [], window_s=10, phase_marks=phase_marks)

    assert len(windows) == 3
    # Window 0-10: midpoint=5, warmup_end=5, 5 < 5 is false → steady (mid not < warmup_end)
    # Actually midpoint=5, warmup_end=5, 5 < 5 is false → not warmup
    # step_at=15, 5 < 15 → steady
    assert windows[0]["phase"] == "steady", f"w0 phase={windows[0]['phase']}, expected steady"
    # Window 10-20: midpoint=15, step_at=15, 15 < 15 false → not steady, cooldown_start=25, 15 < 25 → fault
    assert windows[1]["phase"] == "fault", f"w1 phase={windows[1]['phase']}, expected fault"
    # Window 20-30: midpoint=25, cooldown_start=25, 25 < 25 false → cooldown
    assert windows[2]["phase"] == "cooldown", f"w2 phase={windows[2]['phase']}, expected cooldown"

    # Without phase_marks, all should be "steady"
    windows_no_phase = build_traffic_windows(rows, [], window_s=10)
    for w in windows_no_phase:
        assert w["phase"] == "steady", f"phase should be steady without phase_marks, got {w['phase']}"

    print("  OK: build_traffic_windows phase_marks correct")
    return True


def test_build_traffic_windows_ts_inference(tmp: Path) -> bool:
    """build_traffic_windows: start_ts inference from end_ts+latency, skip rows missing both."""
    rows = [
        # Row with only end_ts + latency_ms → start_ts inferred
        {
            "request_id": "r1",
            "status": "success",
            "latency_ms": 2000,
            "end_ts": 12.0,  # start_ts = 12 - 2 = 10 → arrival window 10-20
        },
        # Row with start_ts explicitly
        {
            "request_id": "r2",
            "status": "success",
            "latency_ms": 500,
            "start_ts": 1.0,
            "end_ts": 1.5,
        },
        # Row missing both start_ts and end_ts → skipped
        {
            "request_id": "r3",
            "status": "success",
            "latency_ms": 500,
        },
        # Row missing start_ts, has end_ts but no latency → start_ts = end_ts - 0 = end_ts → not None
        # Actually latency_ms default 0, so start_ts = end_ts - 0 = end_ts
        {
            "request_id": "r4",
            "status": "success",
            "latency_ms": 0,
            "end_ts": 22.0,
        },
    ]
    windows = build_traffic_windows(rows, [], window_s=10)

    # r2 arrives in window 0, completes in window 0
    # r1 arrives in window 10 (start_ts=10), completes in window 10 (end_ts=12)
    # r4 arrives in window 20 (start_ts=22), completes in window 20 (end_ts=22)
    # r3 skipped
    assert len(windows) == 3, f"expected 3 windows (r3 skipped), got {len(windows)}"

    w0 = windows[0]
    assert w0["window_start"] == 0
    assert w0["arrivals"] == 1, f"w0 arrivals={w0['arrivals']}"
    assert w0["completions"] == 1, f"w0 completions={w0['completions']}"

    w1 = windows[1]
    assert w1["window_start"] == 10
    assert w1["arrivals"] == 1, f"w1 arrivals={w1['arrivals']}"
    assert w1["completions"] == 1, f"w1 completions={w1['completions']}"

    w2 = windows[2]
    assert w2["window_start"] == 20
    assert w2["arrivals"] == 1, f"w2 arrivals={w2['arrivals']}"
    assert w2["completions"] == 1, f"w2 completions={w2['completions']}"

    print("  OK: build_traffic_windows ts inference and skip correct")
    return True


def test_build_traffic_windows_integer_status(tmp: Path) -> bool:
    """build_traffic_windows: integer status codes (200/429/503) correctly classified.

    Production data uses HTTP status codes as integers. This test verifies:
    - status=200 → success, status=429/503/others → failed
    - success/failed counts accurate
    - p50 computed only from successful (200) latencies
    """
    rows = [
        # Window 0-10: 3 requests, 2 success (200), 1 failed (429)
        {"request_id": "r1", "status": 200, "latency_ms": 100, "start_ts": 1.0, "end_ts": 1.1},
        {"request_id": "r2", "status": 429, "latency_ms": 200, "start_ts": 2.0, "end_ts": 2.2},
        {"request_id": "r3", "status": 200, "latency_ms": 300, "start_ts": 3.0, "end_ts": 3.3},
        # Window 10-20: 2 requests, 1 success (200), 1 failed (503)
        {"request_id": "r4", "status": 200, "latency_ms": 150, "start_ts": 11.0, "end_ts": 11.15},
        {"request_id": "r5", "status": 503, "latency_ms": 500, "start_ts": 12.0, "end_ts": 12.5},
        # Window 20-30: 1 request, success (200)
        {"request_id": "r6", "status": 200, "latency_ms": 250, "start_ts": 21.0, "end_ts": 21.25},
    ]
    windows = build_traffic_windows(rows, [], window_s=10)

    assert len(windows) == 3, f"expected 3 windows, got {len(windows)}"

    # Window 0: success=2 (latencies 100,300), failed=1, p50=(100+300)/2=200
    w0 = windows[0]
    assert w0["window_start"] == 0
    assert w0["arrivals"] == 3, f"w0 arrivals={w0['arrivals']}"
    assert w0["completions"] == 3, f"w0 completions={w0['completions']}"
    assert w0["success"] == 2, f"w0 success={w0['success']}, expected 2"
    assert w0["failed"] == 1, f"w0 failed={w0['failed']}, expected 1"
    assert w0["p50"] == 200.0, f"w0 p50={w0['p50']}, expected 200.0 (median of 100,300)"

    # Window 1: success=1 (latency 150), failed=1, p50=150
    w1 = windows[1]
    assert w1["window_start"] == 10
    assert w1["arrivals"] == 2, f"w1 arrivals={w1['arrivals']}"
    assert w1["completions"] == 2, f"w1 completions={w1['completions']}"
    assert w1["success"] == 1, f"w1 success={w1['success']}, expected 1"
    assert w1["failed"] == 1, f"w1 failed={w1['failed']}, expected 1"
    assert w1["p50"] == 150, f"w1 p50={w1['p50']}, expected 150"

    # Window 2: success=1 (latency 250), failed=0, p50=250
    w2 = windows[2]
    assert w2["window_start"] == 20
    assert w2["arrivals"] == 1, f"w2 arrivals={w2['arrivals']}"
    assert w2["completions"] == 1, f"w2 completions={w2['completions']}"
    assert w2["success"] == 1, f"w2 success={w2['success']}, expected 1"
    assert w2["failed"] == 0, f"w2 failed={w2['failed']}, expected 0"
    assert w2["p50"] == 250, f"w2 p50={w2['p50']}, expected 250"

    # Totals
    total_success = sum(w["success"] for w in windows)
    total_failed = sum(w["failed"] for w in windows)
    assert total_success == 4, f"total success={total_success}, expected 4"
    assert total_failed == 2, f"total failed={total_failed}, expected 2"

    print("  OK: build_traffic_windows integer status codes correct")
    return True





def test_windows_csv_backward_compat_old_dict(tmp: Path) -> bool:
    """write_windows_csv: old-format dicts (without new keys) still write, new columns empty."""
    old_windows = [
        {"window_start": 0, "samples": 5, "cpu": {"avg": {"user": 10}}, "mem": {"avg": {"rss": 1000}}},
        {"window_start": 10, "samples": 3, "cpu": {"avg": {"user": 12}}, "mem": {"avg": {"rss": 2000}}},
    ]
    path = tmp / "windows_old.csv"
    write_windows_csv(path, old_windows)

    with path.open() as f:
        reader = csv.DictReader(f)
        rows = list(reader)

    assert len(rows) == 2
    # Old-format: arrivals/completions fall back to samples
    assert rows[0]["arrivals"] == "5", f"old arrivals should fallback to samples, got {rows[0]['arrivals']}"
    assert rows[0]["completions"] == "5", f"old completions should fallback to samples, got {rows[0]['completions']}"
    # New columns empty
    assert rows[0]["window_end"] == "", f"window_end should be empty, got {rows[0]['window_end']}"
    assert rows[0]["phase"] == "", f"phase should be empty, got {rows[0]['phase']}"
    assert rows[0]["route_shares"] == "", f"route_shares should be empty"
    assert rows[0]["corr_p99"] == "", f"corr_p99 should be empty"
    assert rows[0]["ewma"] == "", f"ewma should be empty"

    print("  OK: write_windows_csv backward compat with old dicts")
    return True


def test_report_md_process_stability(tmp: Path) -> bool:
    """render_report_md: process_stability section renders reasons."""
    summary = make_synthetic_summary()
    summary["scenario"] = "S6"
    summary["verdict"] = "FAIL"
    summary["process_stability"] = {
        "ok": False,
        "windows_analyzed": 60,
        "insufficient_windows": 3,
        "share_stddev_pp": 4.5,
        "max_consecutive_breach": 3,
        "corr_p99_headroom": 0.15,
        "reasons": [
            "corr_p99 headroom 0.15 below 0.20 threshold",
            "share stddev 4.50pp exceeds 3.00pp limit (window>=200)",
        ],
    }
    summary["windows"] = {
        "total_windows": 60,
        "phase_distribution": {"warmup": 5, "steady": 40, "fault": 10, "cooldown": 5},
        "max_rps": 52.3,
        "min_rps": 48.1,
    }
    md = render_report_md(summary)

    assert "## Process Windows" in md, "Process Windows section not found"
    assert "corr_p99 headroom 0.15 below 0.20 threshold" in md, "reason not rendered"
    assert "share stddev 4.50pp exceeds 3.00pp limit" in md, "reason not rendered"
    assert "## Window Summary" in md, "Window Summary section not found"
    assert "60" in md, "total_windows not rendered"
    assert "52.30" in md, "max_rps not rendered"

    print("  OK: render_report_md process_stability renders reasons")
    return True

    md = render_report_md(summary)

    assert "UNDERPOWERED" in md, "verdict UNDERPOWERED not found in report.md"
    assert "13000" in md, "min_samples not found in report.md"
    assert "100" in md, "actual n not found in report.md"
    assert "样本量低于判据可行下限" in md, "warning text not found in report.md"
    print("  OK: report.md renders UNDERPOWERED verdict with warning")
    return True


def test_summary_missing_field_raises(tmp: Path) -> bool:
    """write_summary with missing required field should still write (no crash), but report raises on missing verdict."""
    # write_summary itself is lenient — it writes whatever dict is given.
    # render_report_md should handle missing verdict gracefully (renders N/A).
    incomplete = {"scenario": "S1"}  # missing verdict, shares, etc.
    path = tmp / "summary_incomplete.json"
    write_summary(path, incomplete)
    data = json.loads(path.read_text())
    assert data["scenario"] == "S1"

    # render_report_md with missing verdict should produce N/A, not crash
    md = render_report_md(incomplete)
    assert "N/A" in md, f"missing verdict should render N/A in report.md: {md[:200]}"
    print("  OK: summary missing field handled gracefully")
    return True


def test_resource_peaks_read_nested_window_shape(tmp: Path) -> bool:
    """Resource peaks must read aggregate_windows' nested shape, not flat keys.

    Regression: the summary previously read cpu_user_max / mem_rss_max, which
    aggregate_windows never emits, so every reported peak was silently 0 — the
    exact CPU/memory evidence #418 requires. This pins the real shape.
    """
    import lib_resources

    now = 1_700_000_000.0
    rows = [
        {
            "ts": now + i,
            "cpu": {"user": 10.0 + i, "system": 2.0, "iowait": 0.5, "steal": 0.0,
                    "load1": 1.0, "load5": 1.0, "load15": 1.0, "runqueue": 1},
            "mem": {"rss": 1000 + i, "available": 1, "total": 2},
            "disk": {"write_bps": 100 + i},
            "net": {"error": i, "retransmit": i},
        }
        for i in range(3)
    ]
    windows = lib_resources.aggregate_windows(rows, window_s=10)
    assert windows, "expected at least one window"
    w = windows[0]
    assert "cpu_user_max" not in w, "flat key must not exist; the nested shape is authoritative"
    peak_cpu = max((x.get("cpu", {}).get("max", {}).get("user", 0) or 0 for x in windows), default=None)
    peak_rss = max((x.get("mem", {}).get("max", {}).get("rss", 0) or 0 for x in windows), default=None)
    assert peak_cpu == 12.0, f"peak cpu user should be 12.0, got {peak_cpu}"
    assert peak_rss == 1002, f"peak rss should be 1002, got {peak_rss}"
    print("  OK: resource peaks read the nested window shape")
    return True


def test_fill_per_window_route_shares(tmp: Path) -> bool:
    """fill_per_window_route_shares buckets audit attempts to windows by request
    completion time and recomputes per-route shares, repeating post-run corr/EWMA."""
    # Three windows at 10s; two routes A (channel 1) and B (channel 2).
    windows = [
        {"window_start": 1000, "route_shares": "", "corr_p99": "", "ewma": ""},
        {"window_start": 1010, "route_shares": "", "corr_p99": "", "ewma": ""},
        {"window_start": 1020, "route_shares": "", "corr_p99": "", "ewma": ""},
    ]
    end_ts_by_id = {"r1": 1000.0, "r2": 1011.0, "r3": 1025.0, "r4": 1026.0}
    attempts = [
        # window 1000: one A
        {"client_request_id": "r1", "channel_id": 1, "key_index": 0, "upstream_model": "m"},
        # window 1010: one A + two B
        {"client_request_id": "r2", "channel_id": 1, "key_index": 0, "upstream_model": "m"},
        {"client_request_id": "x", "channel_id": 1, "key_index": 0, "upstream_model": "m"},  # no completion ts -> skipped
        {"client_request_id": "r2", "channel_id": 2, "key_index": 1, "upstream_model": "n"},
        {"client_request_id": "r2", "channel_id": 2, "key_index": 1, "upstream_model": "n"},
        # window 1020: two B
        {"client_request_id": "r3", "channel_id": 2, "key_index": 1, "upstream_model": "n"},
        {"client_request_id": "r4", "channel_id": 2, "key_index": 1, "upstream_model": "n"},
    ]
    label_by_identity = {(1, 0, "m"): "A", (2, 1, "n"): "B"}
    fill_per_window_route_shares(
        windows, attempts, end_ts_by_id, label_by_identity,
        post_corr_p99=1.17, post_ewma={"A": 1.0, "B": 0.8},
    )
    assert json.loads(windows[0]["route_shares"]) == {"A": 1.0}, \
        f"window 1000 should be all A, got {windows[0]['route_shares']}"
    assert json.loads(windows[1]["route_shares"]) == {"A": 1 / 3, "B": 2 / 3}, \
        f"window 1010 should be A=1/3 B=2/3, got {windows[1]['route_shares']}"
    assert json.loads(windows[2]["route_shares"]) == {"B": 1.0}, \
        f"window 1020 should be all B, got {windows[2]['route_shares']}"
    assert windows[0]["corr_p99"] == 1.17
    assert json.loads(windows[0]["ewma"]) == {"A": 1.0, "B": 0.8}
    print("  OK: fill_per_window_route_shares buckets and attributes correctly")
    return True

def run_all() -> int:
    tmp = Path("/tmp/route_unit_stress_report_test")
    tmp.mkdir(parents=True, exist_ok=True)
    tests = [
        ("shares_csv", test_shares_csv_headers_and_rows),
        ("windows_csv", test_windows_csv_headers_and_rows),
        ("resources_csv", test_resources_csv),
        ("summary_json", test_summary_json),
        ("report_md_verdict_ci", test_report_md_contains_verdict_and_ci),
        ("report_md_product_fail", test_report_md_product_fail),
        ("report_md_underpowered", test_report_md_underpowered),
        ("summary_missing_field", test_summary_missing_field_raises),
        ("build_traffic_windows_basics", test_build_traffic_windows_basics),
        ("build_traffic_windows_phase_marks", test_build_traffic_windows_phase_marks),
        ("build_traffic_windows_ts_inference", test_build_traffic_windows_ts_inference),
        ("build_traffic_windows_integer_status", test_build_traffic_windows_integer_status),
        ("windows_csv_backward_compat", test_windows_csv_backward_compat_old_dict),
        ("report_md_process_stability", test_report_md_process_stability),
        ("resource_peaks_nested_shape", test_resource_peaks_read_nested_window_shape),
        ("fill_per_window_route_shares", test_fill_per_window_route_shares),
    ]

    failed = 0
    for name, fn in tests:
        print(f"[test] {name}")
        try:
            ok = fn(tmp)
            if not ok:
                print(f"  FAIL: {name} returned False")
                failed += 1
        except Exception as e:
            print(f"  FAIL: {name} raised {type(e).__name__}: {e}")
            failed += 1

    print()
    if failed:
        print(f"FAILED: {failed}/{len(tests)} tests failed")
        return 1
    print(f"PASSED: {len(tests)}/{len(tests)} tests passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(run_all())