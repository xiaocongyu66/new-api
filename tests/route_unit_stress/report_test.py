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
        "selections", "attempts", "observed_share", "ci_low", "ci_high",
        "target", "tol_pp", "base_weight", "ewma_quality",
        "health_multiplier", "share_correction", "final_score", "sample_count",
    ]
    assert headers == expected_headers, f"headers mismatch: {headers} != {expected_headers}"
    assert len(rows) == 2, f"expected 2 rows, got {len(rows)}"
    assert rows[0][0] == "A", f"first row route should be A, got {rows[0][0]}"
    assert rows[1][0] == "B", f"second row route should be B, got {rows[1][0]}"
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
    ]
    assert headers == expected_headers, f"headers mismatch: {headers} != {expected_headers}"
    assert len(rows) == 2, f"expected 2 rows, got {len(rows)}"
    assert rows[0][0] == "0", f"first row window_start should be 0, got {rows[0][0]}"
    assert rows[1][0] == "10", f"second row window_start should be 10, got {rows[1][0]}"
    print("  OK: windows CSV headers and rows correct")
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