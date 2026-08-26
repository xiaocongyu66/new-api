#!/usr/bin/env python3
"""Synthetic data self-tests for lib_stats (S1–S6 stats).

Deterministic asserts, no external dependencies.
"""
from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from lib_stats import (
    wilson_ci,
    percentiles,
    share_stats,
    evaluate_share,
    scenario_targets,
    required_n,
    bad_route_target,
    throttle_target,
    evaluate_process_stability,
)


def assert_close(actual: float, expected: float, tol: float, msg: str) -> None:
    if abs(actual - expected) > tol:
        raise AssertionError(f"{msg}: got {actual:.6f}, expected {expected:.6f} ± {tol}")


def test_wilson_ci_known_value() -> bool:
    """3000/6000 = 0.5 with 95% CI should be ~[0.4874, 0.5126]."""
    low, high = wilson_ci(3000, 6000, z=1.96)
    assert_close(low, 0.4874, 1e-3, "wilson_ci lower bound")
    assert_close(high, 0.5126, 1e-3, "wilson_ci upper bound")
    return True


def test_wilson_ci_zero_total() -> bool:
    """total=0 returns (0.0, 1.0) — complete uncertainty."""
    low, high = wilson_ci(0, 0)
    assert low == 0.0 and high == 1.0, f"zero total: got ({low}, {high})"
    # Also test successes=0, total>0
    low, high = wilson_ci(0, 100)
    assert_close(low, 0.0, 1e-6, "wilson_ci zero successes lower")
    assert_close(high, 0.037, 1e-3, "wilson_ci zero successes upper")
    return True


def test_wilson_ci_all_success() -> bool:
    """All successes should give CI approaching 1.0 from below."""
    low, high = wilson_ci(100, 100)
    assert_close(high, 1.0, 1e-6, "wilson_ci all success upper")
    assert low < 1.0, "wilson_ci all success lower should be < 1"
    return True


def test_percentiles_small_sample() -> bool:
    """Percentiles on [1..10] with linear interpolation."""
    vals = list(range(1, 11))  # 1,2,...,10
    pct = percentiles(vals, [50, 90, 95, 99])
    # n=10, indices: p50 -> 4.5 -> (5+6)/2=5.5
    assert_close(pct["p50"], 5.5, 1e-9, "p50")
    # p90 -> 8.1 -> 9*0.1 + 10*0.9 = 9.1? Wait: idx=(9)*0.9=8.1, lo=8,hi=9 -> 9*0.9+10*0.1=9.1
    assert_close(pct["p90"], 9.1, 1e-9, "p90")
    # p95 -> idx=8.55 -> lo=8,hi=9 -> 9*0.45+10*0.55=9.55
    assert_close(pct["p95"], 9.55, 1e-9, "p95")
    # p99 -> idx=8.91 -> lo=8,hi=9 -> 9*0.09+10*0.91=9.91
    assert_close(pct["p99"], 9.91, 1e-9, "p99")
    assert pct["max"] == 10, "max"
    return True


def test_percentiles_empty() -> bool:
    """Empty input returns all None."""
    pct = percentiles([], [50, 90, 95, 99])
    assert pct["p50"] is None
    assert pct["p90"] is None
    assert pct["p95"] is None
    assert pct["p99"] is None
    assert pct["max"] is None
    return True


def test_percentiles_single_value() -> bool:
    """Single value returns that value for all percentiles."""
    pct = percentiles([42.0], [50, 90, 95, 99])
    for k in ["p50", "p90", "p95", "p99", "max"]:
        assert pct[k] == 42.0, f"{k}: got {pct[k]}"
    return True


def test_share_stats_basic() -> bool:
    """share_stats computes point and Wilson CI."""
    stats = share_stats(467, 1000)
    assert_close(stats["point"], 0.467, 1e-9, "share point")
    low, high = wilson_ci(467, 1000)
    assert_close(stats["ci_low"], low, 1e-9, "share ci_low")
    assert_close(stats["ci_high"], high, 1e-9, "share ci_high")
    return True


def test_share_stats_zero_opportunities() -> bool:
    """Zero opportunities returns point=0, CI=(0,1)."""
    stats = share_stats(0, 0)
    assert stats["point"] == 0.0
    assert stats["ci_low"] == 0.0
    assert stats["ci_high"] == 1.0
    return True


def test_evaluate_share_point_ok_ci_ok() -> bool:
    """Point within tolerance, CI within bounds -> ok."""
    res = evaluate_share(0.467, (0.45, 0.485), 0.467, 0.02, (0.447, 0.487))
    assert res["ok"] is True, f"expected ok, got {res}"
    assert res["reasons"] == [], f"unexpected reasons: {res['reasons']}"
    return True


def test_evaluate_share_point_ok_ci_out_of_bounds() -> bool:
    """Point ok but CI exceeds bounds -> fail with CI reason."""
    res = evaluate_share(0.467, (0.44, 0.49), 0.467, 0.02, (0.447, 0.487))
    assert res["ok"] is False, f"expected fail, got {res}"
    assert any("ci" in r.lower() for r in res["reasons"]), f"missing CI reason: {res['reasons']}"
    return True


def test_evaluate_share_point_out_of_tolerance() -> bool:
    """Point outside tolerance -> fail with point reason."""
    res = evaluate_share(0.44, (0.43, 0.45), 0.467, 0.02, (0.447, 0.487))
    assert res["ok"] is False, f"expected fail, got {res}"
    assert any("point" in r.lower() for r in res["reasons"]), f"missing point reason: {res['reasons']}"
    return True


def test_evaluate_share_both_fail() -> bool:
    """Both point and CI fail -> both reasons present."""
    res = evaluate_share(0.40, (0.38, 0.42), 0.467, 0.02, (0.447, 0.487))
    assert res["ok"] is False
    reasons_text = " ".join(res["reasons"]).lower()
    assert "point" in reasons_text and "ci" in reasons_text, f"missing reasons: {res['reasons']}"
    return True


def test_evaluate_share_ci_exactly_at_bounds() -> bool:
    """CI exactly at bounds should pass (inclusive)."""
    res = evaluate_share(0.467, (0.447, 0.487), 0.467, 0.02, (0.447, 0.487))
    assert res["ok"] is True, f"exact bounds should pass: {res}"
    return True


def test_scenario_targets_values() -> bool:
    """scenario_targets matches #418 specifications."""
    targets = scenario_targets()

    # S1: equivalence, subject=A, both ttft_2000
    s1 = targets["S1"]
    assert_close(s1["target"], 0.50, 1e-9, "S1 target")
    assert_close(s1["tol_pp"], 0.02, 1e-9, "S1 tol_pp")
    assert s1["ci_bounds"] == (0.48, 0.52), f"S1 ci_bounds: {s1['ci_bounds']}"
    assert s1["subject"] == "A", f"S1 subject: {s1['subject']}"
    assert s1["injection"] == {"A": "ttft_2000", "B": "ttft_2000"}, f"S1 injection: {s1['injection']}"
    assert s1["min_samples"] == 13000, f"S1 min_samples: {s1['min_samples']}"
    assert "S1" in s1["description"]
    assert "n≥13000" in s1["description"]

    # S2: latency sensitivity, subject=B, A=ttft_2000 B=ttft_4000 (B 2× slower)
    s2 = targets["S2"]
    assert_close(s2["target"], 0.467, 1e-9, "S2 target")
    assert_close(s2["tol_pp"], 0.02, 1e-9, "S2 tol_pp")
    assert s2["ci_bounds"] == (0.447, 0.487), f"S2 ci_bounds: {s2['ci_bounds']}"
    assert s2["subject"] == "B", f"S2 subject: {s2['subject']}"
    assert s2["injection"] == {"A": "ttft_2000", "B": "ttft_4000"}, f"S2 injection: {s2['injection']}"
    assert s2["min_samples"] == 13000, f"S2 min_samples: {s2['min_samples']}"
    assert "n≥13000" in s2["description"]

    # S3: reverse latency advantage, subject=B, A=ttft_2000 B=ttft_500 (B 4× faster)
    s3 = targets["S3"]
    assert_close(s3["target"], 0.529, 1e-9, "S3 target")
    assert_close(s3["tol_pp"], 0.02, 1e-9, "S3 tol_pp")
    assert s3["ci_bounds"] == (0.509, 0.549), f"S3 ci_bounds: {s3['ci_bounds']}"
    assert s3["subject"] == "B", f"S3 subject: {s3['subject']}"
    assert s3["injection"] == {"A": "ttft_2000", "B": "ttft_500"}, f"S3 injection: {s3['injection']}"
    assert s3["min_samples"] == 13000, f"S3 min_samples: {s3['min_samples']}"
    assert "n≥13000" in s3["description"]


def test_required_n() -> bool:
    """required_n returns ceil((z+margin_z)^2 * p(1-p) / tol^2) with defaults z=1.96, margin_z=2.576."""
    from lib_stats import required_n

    # z=1.96, margin_z=2.576 => z_sum = 4.536
    # S1: target=0.5, tol=0.02 => n = ceil(4.536^2 * 0.25 / 0.0004) = ceil(20.575 * 0.25 / 0.0004) = ceil(12859.5) = 12860
    n1 = required_n(0.50, 0.02)
    assert abs(n1 - 12860) <= 5, f"required_n(0.5, 0.02) = {n1}, expected ~12860"

    # S2: target=0.467, tol=0.02 => n = ceil(4.536^2 * 0.467*0.533 / 0.0004)
    n2 = required_n(0.467, 0.02)
    assert abs(n2 - 12804) <= 5, f"required_n(0.467, 0.02) = {n2}, expected ~12804"

    # S3: target=0.529, tol=0.02 => n = ceil(4.536^2 * 0.529*0.471 / 0.0004)
    n3 = required_n(0.529, 0.02)
    assert abs(n3 - 12816) <= 5, f"required_n(0.529, 0.02) = {n3}, expected ~12816"

    # min_samples >= required_n for each scenario
    targets = scenario_targets()
    for name in ["S1", "S2", "S3"]:
        t = targets[name]
        req = required_n(t["target"], t["tol_pp"])
        assert t["min_samples"] >= req, f"{name}: min_samples {t['min_samples']} < required_n {req}"

    return True


def test_bad_route_target() -> bool:
    """bad_route_target computes exact share for bad route in pool."""
    assert_close(bad_route_target(2, 0.5), 1/3, 1e-9, "bad_route_target 2 routes")
    assert_close(bad_route_target(4, 0.5), 1/7, 1e-9, "bad_route_target 4 routes")
    assert_close(bad_route_target(8, 0.5), 1/15, 1e-9, "bad_route_target 8 routes")
    # Test with different bad_quality
    assert_close(bad_route_target(3, 0.25), 0.25/(0.25+2), 1e-9, "bad_route_target quality 0.25")
    return True


def test_throttle_target() -> bool:
    """throttle_target returns q/(1+q) for ThrottledObservation q."""
    assert_close(throttle_target(0.7), 0.7/1.7, 1e-9, "throttle_target 0.7")
    # 0.7/1.7 = 0.4117647058823529
    assert_close(throttle_target(0.7), 0.4117647058823529, 1e-9, "throttle_target exact")
    assert_close(throttle_target(1.0), 0.5, 1e-9, "throttle_target 1.0")
    assert_close(throttle_target(0.5), 0.5/1.5, 1e-9, "throttle_target 0.5")
    return True


def test_scenario_targets_s4_s5_s6() -> bool:
    """S4, S5, S6 scenario targets match #418 specifications."""
    targets = scenario_targets()

    # S4 variants
    for variant in ["S4_NORETRY", "S4_RETRY5", "S4_RETRY10"]:
        s = targets[variant]
        assert_close(s["target"], 0.38, 1e-9, f"{variant} target")
        assert_close(s["tol_pp"], 0.03, 1e-9, f"{variant} tol_pp")
        assert s["ci_bounds"] == (0.35, 0.41), f"{variant} ci_bounds: {s['ci_bounds']}"
        assert s["subject"] == "B", f"{variant} subject: {s['subject']}"
        assert s["min_samples"] == 5400, f"{variant} min_samples: {s['min_samples']}"
        assert s["throttle_only_share"] == 0.4117647058823529, f"{variant} throttle_only_share: {s['throttle_only_share']}"
        assert "PRODUCT_FAIL" in s["description"], f"{variant} description missing PRODUCT_FAIL"

    # Check injections
    assert targets["S4_NORETRY"]["injection"] == {"A": "ok", "B": "ratelimit_missing"}
    assert targets["S4_RETRY5"]["injection"] == {"A": "ok", "B": "ratelimit_5s"}
    assert targets["S4_RETRY10"]["injection"] == {"A": "ok", "B": "ratelimit_10s"}

    # S5 pools
    s5_2 = targets["S5_POOL2"]
    assert_close(s5_2["target"], 1/3, 1e-9, "S5_POOL2 target")
    assert_close(s5_2["tol_pp"], 0.03, 1e-9, "S5_POOL2 tol_pp")
    assert s5_2["ci_bounds"] == (0.303, 0.363), f"S5_POOL2 ci_bounds: {s5_2['ci_bounds']}"
    assert s5_2["subject"] == "BAD"
    assert s5_2["route_count"] == 2
    assert s5_2["injection"] == {"BAD": "q05", "GOOD": "ok"}
    assert s5_2["min_samples"] == 5100

    s5_4 = targets["S5_POOL4"]
    assert_close(s5_4["target"], 1/7, 1e-9, "S5_POOL4 target")
    assert_close(s5_4["tol_pp"], 0.03, 1e-9, "S5_POOL4 tol_pp")
    assert s5_4["ci_bounds"] == (0.113, 0.173), f"S5_POOL4 ci_bounds: {s5_4['ci_bounds']}"
    assert s5_4["subject"] == "BAD"
    assert s5_4["route_count"] == 4
    assert s5_4["min_samples"] == 2800

    s5_8 = targets["S5_POOL8"]
    assert_close(s5_8["target"], 1/15, 1e-9, "S5_POOL8 target")
    assert_close(s5_8["tol_pp"], 0.02, 1e-9, "S5_POOL8 tol_pp")
    assert s5_8["ci_bounds"] == (0.0467, 0.0867), f"S5_POOL8 ci_bounds: {s5_8['ci_bounds']}"
    assert s5_8["subject"] == "BAD"
    assert s5_8["route_count"] == 8
    assert s5_8["min_samples"] == 3300

    # S6 windows - no target/tol_pp/ci_bounds (or None)
    for w_name, w_size, stddev_max in [
        ("S6_W50", 50, 0.06),
        ("S6_W200", 200, 0.03),
        ("S6_W1000", 1000, 0.03),
    ]:
        s = targets[w_name]
        assert s["target"] is None, f"{w_name} target should be None"
        assert s["tol_pp"] is None, f"{w_name} tol_pp should be None"
        assert s["ci_bounds"] is None, f"{w_name} ci_bounds should be None"
        assert s["subject"] == "STABLE"
        assert s["route_count"] == 4
        assert s["share_window_size"] == w_size
        assert s["process_thresholds"]["share_stddev_max_pp"] == stddev_max
        assert s["process_thresholds"]["corr_p99_headroom_min"] == 0.20
        assert s["process_thresholds"]["consecutive_breach_max"] == 2
        assert s["process_thresholds"]["min_window_samples"] == 100
        assert s["process_thresholds"]["corr_p99_max"] == 2.0
        assert s["injection"] == {"STABLE": "ok", "SLOW": "ttft_4000", "STEP": "ttft_4000→ttft_500"}
        assert s["step_at_ratio"] == 0.5
        assert s["min_tail_seconds"] == 90

    return True


def test_min_samples_vs_required_n() -> bool:
    """min_samples >= required_n for S4 and S5 scenarios (S6 skipped)."""
    targets = scenario_targets()
    for name in [
        "S4_NORETRY", "S4_RETRY5", "S4_RETRY10",
        "S5_POOL2", "S5_POOL4", "S5_POOL8",
    ]:
        t = targets[name]
        req = required_n(t["target"], t["tol_pp"])
        assert t["min_samples"] >= req, f"{name}: min_samples {t['min_samples']} < required_n {req}"
    return True


def test_evaluate_process_stability() -> bool:
    """evaluate_process_stability evaluates S6 process stability criteria."""
    thresholds = {
        "corr_p99_headroom_min": 0.20,
        "share_stddev_max_pp": 0.06,
        "consecutive_breach_max": 2,
        "min_window_samples": 100,
        "corr_p99_max": 2.0,
    }

    # Case 1: all windows pass
    windows_ok = [
        {"share": 0.33, "corr_p99": 1.0, "samples": 200},
        {"share": 0.34, "corr_p99": 1.1, "samples": 200},
        {"share": 0.32, "corr_p99": 0.9, "samples": 200},
        {"share": 0.33, "corr_p99": 1.0, "samples": 200},
        {"share": 0.34, "corr_p99": 1.0, "samples": 200},
    ]
    res = evaluate_process_stability(windows_ok, thresholds)
    assert res["ok"] is True, f"all pass should be ok: {res}"
    assert res["reasons"] == [], f"unexpected reasons: {res['reasons']}"

    # Case 2: share stddev exceeds threshold (6pp = 0.06)
    windows_high_std = [
        {"share": 0.20, "corr_p99": 1.0, "samples": 200},
        {"share": 0.40, "corr_p99": 1.0, "samples": 200},
        {"share": 0.20, "corr_p99": 1.0, "samples": 200},
        {"share": 0.40, "corr_p99": 1.0, "samples": 200},
        {"share": 0.20, "corr_p99": 1.0, "samples": 200},
    ]
    res = evaluate_process_stability(windows_high_std, thresholds)
    assert res["ok"] is False, f"high stddev should fail: {res}"
    assert any("stddev" in r.lower() for r in res["reasons"]), f"missing stddev reason: {res['reasons']}"

    # Case 3: consecutive breach > 2 (with low stddev)
    # Use 10 windows: 7 at 0.33, then 3 at 0.40 (breach = |share - median| > 0.06)
    # median = 0.33, breach_threshold = 0.06
    # 0.40 is |0.40-0.33| = 0.07 > 0.06 → breach
    # Stddev: mean=0.351, stddev≈0.032 (3.2pp < 6pp)
    windows_consecutive = [
        {"share": 0.33, "corr_p99": 1.0, "samples": 200},
        {"share": 0.33, "corr_p99": 1.0, "samples": 200},
        {"share": 0.33, "corr_p99": 1.0, "samples": 200},
        {"share": 0.33, "corr_p99": 1.0, "samples": 200},
        {"share": 0.33, "corr_p99": 1.0, "samples": 200},
        {"share": 0.33, "corr_p99": 1.0, "samples": 200},
        {"share": 0.33, "corr_p99": 1.0, "samples": 200},
        {"share": 0.40, "corr_p99": 1.0, "samples": 200},
        {"share": 0.40, "corr_p99": 1.0, "samples": 200},
        {"share": 0.40, "corr_p99": 1.0, "samples": 200},
    ]
    res = evaluate_process_stability(windows_consecutive, thresholds)
    assert res["ok"] is False, f"consecutive breach should fail: {res}"
    assert any("consecutive" in r.lower() for r in res["reasons"]), f"missing consecutive reason: {res['reasons']}"
    assert res["max_consecutive_breach"] >= 3, f"max_consecutive_breach should be >=3: {res['max_consecutive_breach']}"
    # Verify stddev is within bounds so only consecutive breach triggers
    assert res["share_stddev_pp"] < 6.0, f"stddev should be <6pp: {res['share_stddev_pp']}"

    # Case 4: corr_p99 headroom < 20% (e.g., corr_p99=1.9, headroom=5%)
    windows_corr = [
        {"share": 0.33, "corr_p99": 1.9, "samples": 200},
        {"share": 0.33, "corr_p99": 1.9, "samples": 200},
        {"share": 0.33, "corr_p99": 1.9, "samples": 200},
    ]
    res = evaluate_process_stability(windows_corr, thresholds)
    assert res["ok"] is False, f"low corr headroom should fail: {res}"
    assert any("headroom" in r.lower() for r in res["reasons"]), f"missing headroom reason: {res['reasons']}"
    assert res["corr_p99_headroom"] < 0.20, f"headroom should be <0.20: {res['corr_p99_headroom']}"

    # Case 5: insufficient samples (< 100)
    windows_insufficient = [
        {"share": 0.33, "corr_p99": 1.0, "samples": 200},
        {"share": 0.33, "corr_p99": 1.0, "samples": 50},   # DATA_INVALID
        {"share": 0.33, "corr_p99": 1.0, "samples": 200},
    ]
    res = evaluate_process_stability(windows_insufficient, thresholds)
    assert res["ok"] is False, f"insufficient samples should fail: {res}"
    assert any("data_invalid" in r.lower() or "samples" in r.lower() for r in res["reasons"]), f"missing insufficient reason: {res['reasons']}"
    assert res["insufficient_windows"] == [1], f"insufficient_windows should be [1]: {res['insufficient_windows']}"

    return True

def run_all() -> int:
    tests = [
        ("wilson_ci_known_value", test_wilson_ci_known_value),
        ("wilson_ci_zero_total", test_wilson_ci_zero_total),
        ("wilson_ci_all_success", test_wilson_ci_all_success),
        ("percentiles_small_sample", test_percentiles_small_sample),
        ("scenario_targets_values", test_scenario_targets_values),
        ("required_n", test_required_n),
        ("percentiles_single_value", test_percentiles_single_value),
        ("share_stats_basic", test_share_stats_basic),
        ("share_stats_zero_opportunities", test_share_stats_zero_opportunities),
        ("evaluate_share_point_ok_ci_ok", test_evaluate_share_point_ok_ci_ok),
        ("evaluate_share_point_ok_ci_out_of_bounds", test_evaluate_share_point_ok_ci_out_of_bounds),
        ("evaluate_share_point_out_of_tolerance", test_evaluate_share_point_out_of_tolerance),
        ("evaluate_share_both_fail", test_evaluate_share_both_fail),
        ("evaluate_share_ci_exactly_at_bounds", test_evaluate_share_ci_exactly_at_bounds),
        ("bad_route_target", test_bad_route_target),
        ("throttle_target", test_throttle_target),
        ("scenario_targets_s4_s5_s6", test_scenario_targets_s4_s5_s6),
        ("min_samples_vs_required_n", test_min_samples_vs_required_n),
        ("evaluate_process_stability", test_evaluate_process_stability),
    ]

    passed = 0
    failed = 0
    for name, fn in tests:
        try:
            fn()
            print(f"  PASS {name}")
            passed += 1
        except AssertionError as e:
            print(f"  FAIL {name}: {e}")
            failed += 1
        except Exception as e:
            print(f"  ERROR {name}: {e}")
            failed += 1

    print(f"\n=== {passed} passed, {failed} failed ===")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(run_all())