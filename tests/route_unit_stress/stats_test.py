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
    evaluate_corr_headroom,
    evaluate_memory_scaling,
    evaluate_affinity_scan,
    aggregate_global_share,
    evaluate_kill_switch,
    evaluate_path_audit,
    evaluate_retry_attribution,
    evaluate_recovery,
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

def test_scenario_targets_s7_s8_s9() -> bool:
    """S7, S8, S9 scenario targets match #418 specifications."""
    targets = scenario_targets()

    # S7: three window sizes
    for w_name, w_size in [("S7_W50", 50), ("S7_W200", 200), ("S7_W1000", 1000)]:
        s = targets[w_name]
        assert s["target"] is None, f"{w_name} target should be None"
        assert s["subject"] == "CORR", f"{w_name} subject: {s['subject']}"
        assert s["route_count"] == 4, f"{w_name} route_count: {s['route_count']}"
        assert s["share_window_size"] == w_size, f"{w_name} window: {s['share_window_size']}"
        assert s["min_corr_snapshots"] == 10000, f"{w_name} min_corr: {s['min_corr_snapshots']}"
        assert s["corr_p99_headroom_min"] == 0.20, f"{w_name} headroom_min: {s['corr_p99_headroom_min']}"
        assert s["corr_p99_max"] == 2.0, f"{w_name} corr_max: {s['corr_p99_max']}"
        assert s["corr_snapshot_interval_s"] == 0.25, f"{w_name} interval: {s['corr_snapshot_interval_s']}"
        assert s["max_duration_s"] == 900, f"{w_name} max_duration: {s['max_duration_s']}"

    # S8: two scale points
    s8_3k = targets["S8_3K"]
    assert s8_3k["subject"] == "MEMORY"
    assert s8_3k["route_count"] == 3000
    assert s8_3k["share_window_size"] == 200
    assert s8_3k["scale"] == {"channels": 50, "aliases": 20, "groups": 3}
    assert s8_3k["steady_duration_s"] == 2160
    assert s8_3k["candidates_per_pool"] == 8

    s8_50k = targets["S8_50K"]
    assert s8_50k["route_count"] == 50000
    assert s8_50k["scale"] == {"channels": 200, "aliases": 50, "groups": 5}
    assert s8_50k["candidates_per_pool"] == 8

    # S9: 8 combos (4 ratios × 2 window sizes)
    for ratio in [0.10, 0.30, 0.50, 0.70]:
        for wsize in [0, 200]:
            key = f"S9_A{int(ratio*100)}_W{wsize}"
            s = targets[key]
            assert s["subject"] == "A", f"{key} subject: {s['subject']}"
            assert s["route_count"] == 3, f"{key} route_count: {s['route_count']}"
            assert s["share_window_size"] == wsize, f"{key} window: {s['share_window_size']}"
            assert s["affinity_ratio"] == ratio, f"{key} ratio: {s['affinity_ratio']}"
            assert s["min_samples"] == 1200, f"{key} min_samples: {s['min_samples']}"
            assert s["injection"] == {"A": "ok", "B": "ok", "C": "ok"}, f"{key} injection: {s['injection']}"

    return True


def test_evaluate_corr_headroom_pass() -> bool:
    """corr headroom passes when p99 is well below ShareCorrMax."""
    # Generate 12000 corr values per route, all near 1.0 (well below 2.0)
    import random
    random.seed(42)
    snapshots = []
    for _ in range(100):
        snap = {"A": [1.0 + random.uniform(-0.1, 0.1) for _ in range(120)],
                "B": [1.0 + random.uniform(-0.1, 0.1) for _ in range(120)]}
        snapshots.append(snap)

    targets = {"corr_p99_max": 2.0, "corr_p99_headroom_min": 0.20, "min_corr_snapshots": 10000}
    res = evaluate_corr_headroom(snapshots, targets)
    assert res["ok"] is True, f"should pass: {res}"
    assert res["total_snapshots"] == 24000, f"total: {res['total_snapshots']}"
    assert res["headroom"] > 0.20, f"headroom: {res['headroom']}"
    return True


def test_evaluate_corr_headroom_insufficient() -> bool:
    """corr headroom fails with DATA_INVALID when < min_snapshots."""
    snapshots = [{"A": [1.0] * 500, "B": [1.0] * 500}]  # only 500 per route
    targets = {"corr_p99_max": 2.0, "corr_p99_headroom_min": 0.20, "min_corr_snapshots": 10000}
    res = evaluate_corr_headroom(snapshots, targets)
    assert res["ok"] is False, f"should fail: {res}"
    assert any("DATA_INVALID" in r for r in res["reasons"]), f"reasons: {res['reasons']}"
    return True


def test_evaluate_corr_headroom_low_headroom() -> bool:
    """corr headroom fails when p99 near ShareCorrMax."""
    # 12000 values per route near 1.9 (headroom only 5%)
    snapshots = [{"A": [1.9] * 12000, "B": [1.9] * 12000}]
    targets = {"corr_p99_max": 2.0, "corr_p99_headroom_min": 0.20, "min_corr_snapshots": 10000}
    res = evaluate_corr_headroom(snapshots, targets)
    assert res["ok"] is False, f"should fail: {res}"
    assert any("headroom" in r.lower() for r in res["reasons"]), f"reasons: {res['reasons']}"
    assert res["headroom"] < 0.20, f"headroom: {res['headroom']}"
    return True


def test_evaluate_memory_scaling_pass() -> bool:
    """memory scaling passes when pool count matches and no growth."""
    snapshots = [
        {"rss": 100_000_000, "heap": 50_000_000, "pool_count": 10, "active_pools": 10, "ts": 0.0},
        {"rss": 120_000_000, "heap": 55_000_000, "pool_count": 10, "active_pools": 10, "ts": 30.0},
        {"rss": 130_000_000, "heap": 56_000_000, "pool_count": 10, "active_pools": 10, "ts": 60.0},
    ]
    res = evaluate_memory_scaling(snapshots, {})
    assert res["ok"] is True, f"should pass: {res}"
    assert res["active_pool_match"] is True
    assert res["monotonic_growth"] is False
    return True


def test_evaluate_memory_scaling_pool_mismatch() -> bool:
    """memory scaling fails when pool_count != active_pools."""
    snapshots = [
        {"rss": 100, "heap": 50, "pool_count": 10, "active_pools": 10, "ts": 0.0},
        {"rss": 100, "heap": 50, "pool_count": 10, "active_pools": 8, "ts": 30.0},  # mismatch
    ]
    res = evaluate_memory_scaling(snapshots, {})
    assert res["ok"] is False, f"should fail: {res}"
    assert any("pool_count" in r for r in res["reasons"]), f"reasons: {res['reasons']}"
    return True


def test_evaluate_memory_scaling_growth() -> bool:
    """memory scaling flags unexplained monotonic growth."""
    snapshots = [
        {"rss": 100, "heap": 50_000_000, "pool_count": 10, "active_pools": 10, "ts": 0.0},
    ]
    for i in range(1, 15):
        snapshots.append({
            "rss": 100 + i * 10,
            "heap": 50_000_000 + i * 10_000_000,  # grows past 2× baseline
            "pool_count": 10,
            "active_pools": 10,
            "ts": i * 30.0,
        })
    res = evaluate_memory_scaling(snapshots, {})
    assert res["monotonic_growth"] is True, f"growth: {res['monotonic_growth']}"
    assert res["ok"] is False, f"should fail: {res}"
    return True


def test_evaluate_affinity_scan_monotonic() -> bool:
    """affinity scan passes when A share is monotonic and W200 < W0 at 50%."""
    share_rows = {
        "0.10_0": {"A_share": 0.367, "A_ci_low": 0.34, "A_ci_high": 0.39},
        "0.30_0": {"A_share": 0.425, "A_ci_low": 0.40, "A_ci_high": 0.45},
        "0.50_0": {"A_share": 0.500, "A_ci_low": 0.47, "A_ci_high": 0.53},
        "0.70_0": {"A_share": 0.583, "A_ci_low": 0.55, "A_ci_high": 0.61},
        "0.10_200": {"A_share": 0.345, "A_ci_low": 0.32, "A_ci_high": 0.37},
        "0.30_200": {"A_share": 0.400, "A_ci_low": 0.37, "A_ci_high": 0.43},
        "0.50_200": {"A_share": 0.430, "A_ci_low": 0.40, "A_ci_high": 0.46},
        "0.70_200": {"A_share": 0.560, "A_ci_low": 0.53, "A_ci_high": 0.59},
    }
    res = evaluate_affinity_scan(share_rows, {})
    assert res["ok"] is True, f"should pass: {res}"
    assert res["monotonic"] is True
    assert res["correction_significant"] is True  # 0.50 - 0.43 = 0.07 > 0.02
    return True


def test_evaluate_affinity_scan_non_monotonic() -> bool:
    """affinity scan fails when A share decreases."""
    share_rows = {
        "0.10_0": {"A_share": 0.40, "A_ci_low": 0.37, "A_ci_high": 0.43},
        "0.30_0": {"A_share": 0.35, "A_ci_low": 0.32, "A_ci_high": 0.38},  # decreased!
        "0.50_0": {"A_share": 0.50, "A_ci_low": 0.47, "A_ci_high": 0.53},
        "0.70_0": {"A_share": 0.58, "A_ci_low": 0.55, "A_ci_high": 0.61},
        "0.10_200": {"A_share": 0.34, "A_ci_low": 0.31, "A_ci_high": 0.37},
        "0.30_200": {"A_share": 0.38, "A_ci_low": 0.35, "A_ci_high": 0.41},
        "0.50_200": {"A_share": 0.43, "A_ci_low": 0.40, "A_ci_high": 0.46},
        "0.70_200": {"A_share": 0.56, "A_ci_low": 0.53, "A_ci_high": 0.59},
    }
    res = evaluate_affinity_scan(share_rows, {})
    assert res["ok"] is False, f"should fail: {res}"
    assert res["monotonic"] is False
    return True


def test_evaluate_affinity_scan_no_correction() -> bool:
    """affinity scan fails when W200 not significantly lower than W0 at 50%."""
    share_rows = {
        "0.10_0": {"A_share": 0.37, "A_ci_low": 0.34, "A_ci_high": 0.40},
        "0.30_0": {"A_share": 0.42, "A_ci_low": 0.39, "A_ci_high": 0.45},
        "0.50_0": {"A_share": 0.50, "A_ci_low": 0.47, "A_ci_high": 0.53},
        "0.70_0": {"A_share": 0.58, "A_ci_low": 0.55, "A_ci_high": 0.61},
        "0.10_200": {"A_share": 0.35, "A_ci_low": 0.32, "A_ci_high": 0.38},
        "0.30_200": {"A_share": 0.40, "A_ci_low": 0.37, "A_ci_high": 0.43},
        "0.50_200": {"A_share": 0.49, "A_ci_low": 0.46, "A_ci_high": 0.52},  # only 1pp lower
        "0.70_200": {"A_share": 0.57, "A_ci_low": 0.54, "A_ci_high": 0.60},
    }
    res = evaluate_affinity_scan(share_rows, {})
    assert res["ok"] is False, f"should fail: {res}"
    assert res["correction_significant"] is False
    return True

def test_scenario_targets_s10_s11_s12_s13() -> bool:
    """S10-S13 scenario targets match #418 specifications."""
    targets = scenario_targets()

    race = targets["S10_RACE"]
    assert race["subject"] == "B"
    assert race["route_count"] == 2
    assert race["replica"] == 1
    assert race["min_samples"] == 6000

    glob = targets["S10_GLOBAL"]
    assert glob["replica"] == 2
    assert glob["min_samples"] == 12000, "12,000 must be GLOBAL, not per pod"
    assert glob["ci_bounds"] == (0.47, 0.53)

    restart = targets["S10_RESTART"]
    assert restart["subject"] == "KILLSWITCH"
    assert restart["kill_switch_option"] == "RouteStatsEnabled"
    assert restart["neutral_correction"] == 1.0
    assert restart["min_samples"] == 1000

    full = targets["S11_FULL"]
    assert full["subject"] == "PATHS"
    assert full["min_per_path"] == 100
    assert full["min_probe"] == 20
    assert full["user_paths"] == ["weighted", "affinity", "specific"]

    retry = targets["S12_RETRY"]
    assert retry["subject"] == "B"
    assert retry["injection"]["B"] == "first_fail_then_ok"
    assert retry["min_samples"] == 600
    assert retry["retry_times"] == 1

    casc = targets["S13_CASCADE"]
    assert_close(casc["target"], 0.50, 1e-9, "S13 target")
    assert_close(casc["tol_pp"], 0.03, 1e-9, "S13 tol_pp")
    assert casc["corr_min"] == 0.5
    assert casc["corr_max"] == 2.0
    assert casc["upstream_failure_threshold"] == 1, "503 is upstream, not local"
    assert casc["retry_times"] == 1
    assert casc["sample_interval_s"] == 10
    return True


def test_aggregate_global_share_pools_raw_attempts() -> bool:
    """Global share is one division over pooled attempts, not a mean of pod shares.

    Pod A: 1/1 for the subject. Pod B: 1/3. Averaging the percentages gives
    (1.00 + 0.333)/2 = 0.667; the correct pooled answer is 2/4 = 0.50. This test
    exists to catch exactly that mistake.
    """
    subject = (1, 0, "up1")
    other = (2, 0, "up2")
    attempts = [
        {"channel_id": 1, "key_index": 0, "upstream_model": "up1", "pod": "a"},
        {"channel_id": 1, "key_index": 0, "upstream_model": "up1", "pod": "b"},
        {"channel_id": 2, "key_index": 0, "upstream_model": "up2", "pod": "b"},
        {"channel_id": 2, "key_index": 0, "upstream_model": "up2", "pod": "b"},
    ]
    res = aggregate_global_share(attempts, subject)
    assert_close(res["global_share"], 0.50, 1e-9, "pooled global share")
    assert res["global_selections"] == 2
    assert res["global_total"] == 4
    assert res["pods_seen"] == ["a", "b"]
    assert_close(res["per_pod"]["a"]["share"], 1.0, 1e-9, "pod a share")
    assert_close(res["per_pod"]["b"]["share"], 1 / 3, 1e-9, "pod b share")
    naive_mean = (res["per_pod"]["a"]["share"] + res["per_pod"]["b"]["share"]) / 2
    assert abs(naive_mean - res["global_share"]) > 0.1, "the per-pod mean must differ from pooled"
    assert other != subject
    return True


def test_aggregate_global_share_empty() -> bool:
    """No attempts yields a zero share rather than a division error."""
    res = aggregate_global_share([], (1, 0, "up1"))
    assert res["global_share"] == 0.0
    assert res["global_total"] == 0
    assert res["pods_seen"] == []
    return True


def test_evaluate_kill_switch_pass() -> bool:
    """Option persisted false, corrections neutral, window frozen, pid changed."""
    targets = scenario_targets()["S10_RESTART"]
    before = {"option": "true", "corrections": [1.3, 0.8], "window_entries": 200, "pid": "111"}
    after = {"option": "false", "corrections": [1.0, 1.0], "window_entries": 200, "pid": "222"}
    res = evaluate_kill_switch(before, after, targets)
    assert res["ok"] is True, f"should pass: {res}"
    assert res["option_persisted"] is True
    assert res["corrections_neutral"] is True
    assert res["window_stopped"] is True
    assert res["restarted"] is True
    return True


def test_evaluate_kill_switch_non_neutral_correction() -> bool:
    """A non-1.0 correction after the kill switch is a product failure."""
    targets = scenario_targets()["S10_RESTART"]
    before = {"option": "true", "corrections": [1.0], "window_entries": 100, "pid": "111"}
    after = {"option": "false", "corrections": [1.0, 1.45], "window_entries": 100, "pid": "222"}
    res = evaluate_kill_switch(before, after, targets)
    assert res["ok"] is False, f"should fail: {res}"
    assert res["corrections_neutral"] is False
    assert res["non_neutral"] == [1.45]
    return True


def test_evaluate_kill_switch_no_restart_is_data_invalid() -> bool:
    """Same pid means no restart happened, so persistence is unproven."""
    targets = scenario_targets()["S10_RESTART"]
    before = {"option": "true", "corrections": [1.0], "window_entries": 10, "pid": "111"}
    after = {"option": "false", "corrections": [1.0], "window_entries": 10, "pid": "111"}
    res = evaluate_kill_switch(before, after, targets)
    assert res["ok"] is False
    assert res["restarted"] is False
    assert any("DATA_INVALID" in r for r in res["reasons"]), res["reasons"]
    return True


def test_evaluate_kill_switch_window_kept_growing() -> bool:
    """The window must stop allocating once the kill switch is set."""
    targets = scenario_targets()["S10_RESTART"]
    before = {"option": "true", "corrections": [1.0], "window_entries": 100, "pid": "111"}
    after = {"option": "false", "corrections": [1.0], "window_entries": 180, "pid": "222"}
    res = evaluate_kill_switch(before, after, targets)
    assert res["ok"] is False
    assert res["window_stopped"] is False
    assert any("kept allocating" in r for r in res["reasons"]), res["reasons"]
    return True


def _s11_attempts(path: str, n: int, channel_id: int = 1) -> list[dict]:
    return [
        {
            "request_id": f"{path}-{i}",
            "client_request_id": f"c-{path}-{i}",
            "group": "g",
            "alias": "a",
            "channel_id": channel_id,
            "key_index": 0,
            "upstream_model": f"up{channel_id}",
            "path": path,
        }
        for i in range(n)
    ]


def test_evaluate_path_audit_pass() -> bool:
    """All paths labelled and sampled, probes isolated, bypass in window."""
    targets = scenario_targets()["S11_FULL"]
    attempts = (
        _s11_attempts("weighted", 100, 1)
        + _s11_attempts("affinity", 100, 2)
        + _s11_attempts("specific", 100, 3)
    )
    res = evaluate_path_audit(attempts, 0, ["weighted", "affinity", "specific"], targets)
    assert res["ok"] is True, f"should pass: {res}"
    assert res["probe_isolated"] is True
    assert res["bypass_in_window"] is True
    assert res["unlabelled"] == 0
    return True


def test_evaluate_path_audit_probe_in_window() -> bool:
    """A probe contributing share opportunities is PRODUCT_FAIL."""
    targets = scenario_targets()["S11_FULL"]
    attempts = (
        _s11_attempts("weighted", 100, 1)
        + _s11_attempts("affinity", 100, 2)
        + _s11_attempts("specific", 100, 3)
    )
    res = evaluate_path_audit(attempts, 7, ["weighted", "affinity", "specific"], targets)
    assert res["ok"] is False, f"should fail: {res}"
    assert res["probe_isolated"] is False
    assert any("opportunity_count=0" in r for r in res["reasons"]), res["reasons"]
    return True


def test_evaluate_path_audit_bypass_missing_from_window() -> bool:
    """Real bypass traffic absent from the window blinds the correction."""
    targets = scenario_targets()["S11_FULL"]
    attempts = (
        _s11_attempts("weighted", 100, 1)
        + _s11_attempts("affinity", 100, 2)
        + _s11_attempts("specific", 100, 3)
    )
    res = evaluate_path_audit(attempts, 0, ["weighted"], targets)
    assert res["ok"] is False, f"should fail: {res}"
    assert res["bypass_in_window"] is False
    assert any("never entered the share window" in r for r in res["reasons"]), res["reasons"]
    return True


def test_evaluate_path_audit_unlabelled_attempt() -> bool:
    """An attempt with no path label breaks the data contract."""
    targets = scenario_targets()["S11_FULL"]
    attempts = (
        _s11_attempts("weighted", 100, 1)
        + _s11_attempts("affinity", 100, 2)
        + _s11_attempts("specific", 100, 3)
    )
    attempts.append({**attempts[0], "path": ""})
    res = evaluate_path_audit(attempts, 0, ["weighted", "affinity", "specific"], targets)
    assert res["ok"] is False
    assert res["unlabelled"] == 1
    assert any("no path label" in r for r in res["reasons"]), res["reasons"]
    return True


def test_evaluate_path_audit_below_min_per_path() -> bool:
    """Too few samples on a path is DATA_INVALID, not a product verdict."""
    targets = scenario_targets()["S11_FULL"]
    attempts = _s11_attempts("weighted", 100, 1) + _s11_attempts("affinity", 5, 2)
    res = evaluate_path_audit(attempts, 0, ["weighted", "affinity", "specific"], targets)
    assert res["ok"] is False
    assert "affinity" in res["paths_missing"]
    assert any("DATA_INVALID" in r for r in res["reasons"]), res["reasons"]
    return True


def test_evaluate_retry_attribution_both() -> bool:
    """Failed attempts moved quality AND took window slots: correct behaviour."""
    stats = {
        "b_attempt_failures": 300,
        "b_user_failures": 0,
        "b_ewma_before": 1.0,
        "b_ewma_after": 0.82,
        "b_total_attempts": 900,
        # 600 successes cannot account for 700 slots, so at least 100 slots are
        # owed to failed attempts.
        "b_window_slots_final": 700,
        "window_total_slots": 900,
    }
    res = evaluate_retry_attribution(stats, {})
    assert res["attribution"] == "both", res
    assert res["ok"] is True, res
    assert res["quality_observed"] is True
    assert res["window_observed"] is True
    assert res["ewma_delta"] < 0
    assert res["failed_attempts_in_window_min"] == 100, res
    return True


def test_evaluate_retry_attribution_neither() -> bool:
    """Neither signal: a failing route can never be de-weighted (PRODUCT_FAIL)."""
    stats = {
        "b_attempt_failures": 300,
        "b_user_failures": 0,
        "b_ewma_before": 1.0,
        "b_ewma_after": 1.0,
        # Every slot B holds is explained by its 600 successes, so no failed
        # attempt is provably in the window.
        "b_total_attempts": 900,
        "b_window_slots_final": 600,
        "window_total_slots": 900,
    }
    res = evaluate_retry_attribution(stats, {})
    assert res["attribution"] == "neither", res
    assert res["ok"] is False
    assert res["failed_attempts_in_window_min"] == 0, res
    assert any("never be de-weighted" in r for r in res["reasons"]), res["reasons"]
    return True


def test_evaluate_retry_attribution_partial() -> bool:
    """Exactly one half observed is a partial risk, never a silent pass."""
    quality_only = {
        "b_attempt_failures": 300,
        "b_user_failures": 0,
        "b_ewma_before": 1.0,
        "b_ewma_after": 0.8,
        "b_total_attempts": 900,
        "b_window_slots_final": 600,
        "window_total_slots": 900,
    }
    res = evaluate_retry_attribution(quality_only, {})
    assert res["attribution"] == "quality_only", res
    assert res["ok"] is False
    assert any("partial product risk" in r for r in res["reasons"]), res["reasons"]

    window_only = {
        "b_attempt_failures": 300,
        "b_user_failures": 0,
        "b_ewma_before": 1.0,
        "b_ewma_after": 1.0,
        "b_total_attempts": 900,
        "b_window_slots_final": 700,
        "window_total_slots": 900,
    }
    res2 = evaluate_retry_attribution(window_only, {})
    assert res2["attribution"] == "window_only", res2
    assert res2["ok"] is False
    assert any("partial product risk" in r for r in res2["reasons"]), res2["reasons"]
    return True


def test_evaluate_retry_attribution_rejects_cumulative_counter() -> bool:
    """A large cumulative EWMA sample count must not stand in for window evidence.

    This is the regression the old `sample_count >= failures` criterion allowed:
    653 >= 22 passed while the share window was never consulted. Without a
    measured window slot count the window half is unproven and the run is
    DATA_INVALID.
    """
    stats = {
        "b_attempt_failures": 22,
        "b_user_failures": 0,
        "b_ewma_before": 1.09697,
        "b_ewma_after": 0.77607,
        "b_total_attempts": 44,
        "b_ewma_sample_count_after": 653,
    }
    res = evaluate_retry_attribution(stats, {})
    assert res["window_observed"] is False, res
    assert res["ok"] is False
    assert any("DATA_INVALID" in r for r in res["reasons"]), res["reasons"]
    assert res["failed_attempts_in_window_min"] is None, res
    return True


def test_evaluate_retry_attribution_all_attempts_fail() -> bool:
    """When B fails every attempt, every slot it holds is a failed attempt.

    This is the shape S12 actually produces on a live gateway: B's mock fails the
    first call of each chain, so B has zero successes and its window slots are
    entirely retry-absorbed failures.
    """
    stats = {
        "b_attempt_failures": 27,
        "b_user_failures": 0,
        "b_ewma_before": 0.636,
        "b_ewma_after": 0.5,
        "b_total_attempts": 27,
        "b_window_slots_final": 9,
        "window_total_slots": 200,
    }
    res = evaluate_retry_attribution(stats, {})
    assert res["b_successes"] == 0, res
    assert res["failed_attempts_in_window_min"] == 9, res
    assert res["attribution"] == "both", res
    assert res["ok"] is True, res
    return True


def test_evaluate_retry_attribution_quality_floor_saturated() -> bool:
    """A baseline already at the quality floor is unmeasurable, not a product fail.

    Observed rerunning S12 against a warm gateway: B entered at exactly 0.5
    (RouteStatsQualityFloor) and stayed there, so EWMA had no room to fall. The
    plain `after < before` test called that PRODUCT_FAIL, which accuses the
    scheduler for a stale baseline.
    """
    stats = {
        "b_attempt_failures": 25,
        "b_user_failures": 0,
        "b_ewma_before": 0.5,
        "b_ewma_after": 0.5,
        "b_total_attempts": 25,
        "b_window_slots_final": 5,
        "window_total_slots": 200,
    }
    res = evaluate_retry_attribution(stats, {})
    assert res["quality_saturated"] is True, res
    assert res["ok"] is False
    assert any("DATA_INVALID" in r for r in res["reasons"]), res["reasons"]
    assert not any("PRODUCT_FAIL" in r for r in res["reasons"]), res["reasons"]
    return True


def test_evaluate_retry_attribution_no_data() -> bool:
    """Zero attempts is DATA_INVALID, not a product conclusion."""
    stats = {
        "b_attempt_failures": 0,
        "b_user_failures": 0,
        "b_ewma_before": 0.0,
        "b_ewma_after": 0.0,
        "b_total_attempts": 0,
        "b_window_slots_final": 0,
        "window_total_slots": 0,
    }
    res = evaluate_retry_attribution(stats, {})
    assert res["ok"] is False
    assert any("DATA_INVALID" in r for r in res["reasons"]), res["reasons"]
    assert res["attempt_failure_rate"] == 0.0
    return True


def _s13_samples(steady: float, fault: float, recover: list[float], corr: float = 1.0) -> list[dict]:
    rows = []
    ts = 0.0
    for _ in range(3):
        rows.append({"ts": ts, "phase": "steady", "b_share": steady, "corr": {"A": corr, "B": corr}, "samples": 200})
        ts += 10.0
    for _ in range(3):
        rows.append({"ts": ts, "phase": "fault", "b_share": fault, "corr": {"A": corr, "B": corr}, "samples": 200})
        ts += 10.0
    for share in recover:
        rows.append({"ts": ts, "phase": "recover", "b_share": share, "corr": {"A": corr, "B": corr}, "samples": 200})
        ts += 10.0
    return rows


def test_evaluate_recovery_pass() -> bool:
    """Degradation observed, corr clamped, share returns within tolerance."""
    targets = scenario_targets()["S13_CASCADE"]
    samples = _s13_samples(0.50, 0.20, [0.30, 0.42, 0.49])
    res = evaluate_recovery(samples, targets)
    assert res["ok"] is True, f"should pass: {res}"
    assert res["recovered"] is True
    assert res["corr_in_clamp"] is True
    assert res["degradation_observed"] is True
    assert res["recovery_seconds"] == 20.0, res["recovery_seconds"]
    return True


def test_evaluate_recovery_corr_violation() -> bool:
    """A correction outside [0.5, 2.0] is the defect this scenario hunts."""
    targets = scenario_targets()["S13_CASCADE"]
    samples = _s13_samples(0.50, 0.20, [0.30, 0.49])
    samples[4]["corr"] = {"A": 2.5, "B": 1.0}
    res = evaluate_recovery(samples, targets)
    assert res["ok"] is False, f"should fail: {res}"
    assert res["corr_in_clamp"] is False
    assert len(res["corr_violations"]) == 1
    assert res["corr_violations"][0]["corr"] == 2.5
    assert res["corr_violations"][0]["phase"] == "fault"
    return True


def test_evaluate_recovery_never_recovers() -> bool:
    """Share stuck far below target after recovery is PRODUCT_FAIL."""
    targets = scenario_targets()["S13_CASCADE"]
    samples = _s13_samples(0.50, 0.15, [0.16, 0.18, 0.20])
    res = evaluate_recovery(samples, targets)
    assert res["ok"] is False, f"should fail: {res}"
    assert res["recovered"] is False
    assert res["recovery_seconds"] is None
    return True


def test_evaluate_recovery_no_degradation() -> bool:
    """If the injection never bit there is nothing to judge: DATA_INVALID."""
    targets = scenario_targets()["S13_CASCADE"]
    samples = _s13_samples(0.50, 0.50, [0.50, 0.50])
    res = evaluate_recovery(samples, targets)
    assert res["ok"] is False
    assert res["degradation_observed"] is False
    assert any("DATA_INVALID" in r for r in res["reasons"]), res["reasons"]
    return True


def test_evaluate_recovery_empty() -> bool:
    """Empty input returns DATA_INVALID without raising."""
    res = evaluate_recovery([], scenario_targets()["S13_CASCADE"])
    assert res["ok"] is False
    assert res["recovery_seconds"] is None
    assert any("DATA_INVALID" in r for r in res["reasons"]), res["reasons"]
    return True


def test_evaluate_recovery_scalar_corr() -> bool:
    """A scalar corr field is accepted as well as a per-route dict."""
    targets = scenario_targets()["S13_CASCADE"]
    samples = _s13_samples(0.50, 0.20, [0.30, 0.49])
    for s in samples:
        s["corr"] = 3.0
    res = evaluate_recovery(samples, targets)
    assert res["corr_in_clamp"] is False
    assert all(v["route"] == "subject" for v in res["corr_violations"]), res["corr_violations"]
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
        ("scenario_targets_s7_s8_s9", test_scenario_targets_s7_s8_s9),
        ("evaluate_corr_headroom_pass", test_evaluate_corr_headroom_pass),
        ("evaluate_corr_headroom_insufficient", test_evaluate_corr_headroom_insufficient),
        ("evaluate_corr_headroom_low_headroom", test_evaluate_corr_headroom_low_headroom),
        ("evaluate_memory_scaling_pass", test_evaluate_memory_scaling_pass),
        ("evaluate_memory_scaling_pool_mismatch", test_evaluate_memory_scaling_pool_mismatch),
        ("evaluate_memory_scaling_growth", test_evaluate_memory_scaling_growth),
        ("evaluate_affinity_scan_monotonic", test_evaluate_affinity_scan_monotonic),
        ("evaluate_affinity_scan_non_monotonic", test_evaluate_affinity_scan_non_monotonic),
        ("evaluate_affinity_scan_no_correction", test_evaluate_affinity_scan_no_correction),
        ("scenario_targets_s10_s11_s12_s13", test_scenario_targets_s10_s11_s12_s13),
        ("aggregate_global_share_pools_raw_attempts", test_aggregate_global_share_pools_raw_attempts),
        ("aggregate_global_share_empty", test_aggregate_global_share_empty),
        ("evaluate_kill_switch_pass", test_evaluate_kill_switch_pass),
        ("evaluate_kill_switch_non_neutral_correction", test_evaluate_kill_switch_non_neutral_correction),
        ("evaluate_kill_switch_no_restart_is_data_invalid", test_evaluate_kill_switch_no_restart_is_data_invalid),
        ("evaluate_kill_switch_window_kept_growing", test_evaluate_kill_switch_window_kept_growing),
        ("evaluate_path_audit_pass", test_evaluate_path_audit_pass),
        ("evaluate_path_audit_probe_in_window", test_evaluate_path_audit_probe_in_window),
        ("evaluate_path_audit_bypass_missing_from_window", test_evaluate_path_audit_bypass_missing_from_window),
        ("evaluate_path_audit_unlabelled_attempt", test_evaluate_path_audit_unlabelled_attempt),
        ("evaluate_path_audit_below_min_per_path", test_evaluate_path_audit_below_min_per_path),
        ("evaluate_retry_attribution_both", test_evaluate_retry_attribution_both),
        ("evaluate_retry_attribution_neither", test_evaluate_retry_attribution_neither),
        ("evaluate_retry_attribution_partial", test_evaluate_retry_attribution_partial),
        ("evaluate_retry_attribution_no_data", test_evaluate_retry_attribution_no_data),
        ("evaluate_retry_attribution_rejects_cumulative_counter",
         test_evaluate_retry_attribution_rejects_cumulative_counter),
        ("evaluate_retry_attribution_all_attempts_fail",
         test_evaluate_retry_attribution_all_attempts_fail),
        ("evaluate_retry_attribution_quality_floor_saturated",
         test_evaluate_retry_attribution_quality_floor_saturated),
        ("evaluate_recovery_pass", test_evaluate_recovery_pass),
        ("evaluate_recovery_corr_violation", test_evaluate_recovery_corr_violation),
        ("evaluate_recovery_never_recovers", test_evaluate_recovery_never_recovers),
        ("evaluate_recovery_no_degradation", test_evaluate_recovery_no_degradation),
        ("evaluate_recovery_empty", test_evaluate_recovery_empty),
        ("evaluate_recovery_scalar_corr", test_evaluate_recovery_scalar_corr),
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