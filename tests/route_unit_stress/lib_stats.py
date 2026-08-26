#!/usr/bin/env python3
"""Statistical utilities for route-unit EWMA stress scenarios (S1–S3).

Pure Python stdlib implementation — no numpy/scipy/pandas.
"""
from __future__ import annotations

import math
from typing import Any


def wilson_ci(successes: int, total: int, z: float = 1.96) -> tuple[float, float]:
    """Wilson score interval for a binomial proportion.

    Args:
        successes: Number of successes (x).
        total: Number of trials (n).
        z: Z-score for confidence level (default 1.96 for 95%).

    Returns:
        (lower, upper) bounds of the confidence interval as proportions [0, 1].
        For total=0, returns (0.0, 1.0) — complete uncertainty.
    """
    if total == 0:
        return (0.0, 1.0)

    p = successes / total
    z2 = z * z
    denom = 1 + z2 / total
    centre = (p + z2 / (2 * total)) / denom
    half = (z / denom) * math.sqrt(p * (1 - p) / total + z2 / (4 * total * total))
    lower = centre - half
    upper = centre + half
    return (max(0.0, lower), min(1.0, upper))


def percentiles(values: list[float], ps: list[float]) -> dict[str, float]:
    """Compute percentiles using linear interpolation (NumPy default method).

    Args:
        values: List of numeric values (will be sorted).
        ps: List of percentiles in [0, 100] (e.g., [50, 90, 95, 99]).

    Returns:
        Dict with keys "p50", "p90", "p95", "p99", "max" (and any requested p*).
        Missing values are None. Empty input returns all None.
    """
    result: dict[str, float] = {}
    if not values:
        for p in ps:
            result[f"p{int(p)}" if p == int(p) else f"p{p}"] = None
        result["max"] = None
        return result

    sorted_vals = sorted(values)
    n = len(sorted_vals)

    for p in ps:
        if p < 0 or p > 100:
            key = f"p{int(p)}" if p == int(p) else f"p{p}"
            result[key] = None
            continue
        # Linear interpolation: index = (n - 1) * p / 100
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


def share_stats(selections: int, opportunities: int) -> dict[str, float]:
    """Compute point estimate and Wilson CI for a share proportion.

    Args:
        selections: Number of times the target was selected.
        opportunities: Total number of selection opportunities.

    Returns:
        Dict with keys: "point", "ci_low", "ci_high".
    """
    if opportunities == 0:
        return {"point": 0.0, "ci_low": 0.0, "ci_high": 1.0}
    point = selections / opportunities
    ci_low, ci_high = wilson_ci(selections, opportunities)
    return {"point": point, "ci_low": ci_low, "ci_high": ci_high}


def evaluate_share(
    observed_share: float,
    ci: tuple[float, float],
    target: float,
    tol_pp: float,
    ci_bounds: tuple[float, float],
) -> dict[str, Any]:
    """Evaluate whether an observed share meets scenario criteria.

    Criteria:
    1. Point estimate must be within target ± tol_pp (percentage points).
    2. Entire confidence interval must fall within ci_bounds.

    Args:
        observed_share: Point estimate (proportion 0–1).
        ci: (lower, upper) confidence interval from wilson_ci.
        target: Expected share (proportion 0–1).
        tol_pp: Tolerance in percentage points (e.g., 0.02 for ±2pp).
        ci_bounds: (min_ci, max_ci) allowed CI range (proportions).

    Returns:
        Dict with "ok" (bool) and "reasons" (list[str]).
        Reasons are empty on success; on failure, human-readable explanations.
    """
    reasons: list[str] = []
    ci_low, ci_high = ci
    min_ci, max_ci = ci_bounds

    # Check point estimate within target ± tol_pp
    if not (target - tol_pp <= observed_share <= target + tol_pp):
        reasons.append(
            f"point {observed_share:.4f} outside {target:.3f}±{tol_pp:.2f}"
        )

    # Check CI fully within bounds
    if not (min_ci <= ci_low and ci_high <= max_ci):
        reasons.append(
            f"ci [{ci_low:.3f},{ci_high:.3f}] not within [{min_ci:.3f},{max_ci:.3f}]"
        )

    return {"ok": len(reasons) == 0, "reasons": reasons}



def required_n(
    target: float,
    tol_pp: float,
    z: float = 1.96,
    margin_z: float = 2.576,
) -> int:
    """Minimum sample size so that (z + margin_z) * SE ≤ tol_pp.

    Standard error for a binomial proportion: SE = sqrt(p(1-p)/n).
    We require (z + margin_z) * sqrt(p(1-p)/n) ≤ tol_pp.
    Solving for n: n ≥ (z + margin_z)^2 * p(1-p) / tol_pp^2.

    Args:
        target: Expected proportion p (0 < p < 1).
        tol_pp: Tolerance in percentage points (e.g., 0.02 for ±2pp).
        z: Z-score for confidence level (default 1.96 for 95%).
        margin_z: Extra z-margin for CI containment safety (default 2.576 for 99%).

    Returns:
        Minimum integer n (ceiling).
    """
    if not (0 < target < 1):
        raise ValueError("target must be in (0, 1)")
    if tol_pp <= 0:
        raise ValueError("tol_pp must be > 0")
    z_sum = z + margin_z
    n_float = (z_sum * z_sum) * target * (1 - target) / (tol_pp * tol_pp)
    return math.ceil(n_float)


def bad_route_target(route_count: int, bad_quality: float = 0.5) -> float:
    """Return expected share of a single bad route among N routes.

    The bad route has quality `bad_quality` (0.5 = 50% failure rate equivalent),
    all other (route_count - 1) routes have quality 1.0.
    Share = bad_quality / (bad_quality + (route_count - 1)).

    Args:
        route_count: Total number of routes in the pool (≥2).
        bad_quality: Quality factor of the bad route (default 0.5).

    Returns:
        Expected share proportion [0, 1] of the bad route.
    """
    if route_count < 2:
        raise ValueError("route_count must be >= 2")
    if not (0 < bad_quality <= 1):
        raise ValueError("bad_quality must be in (0, 1]")
    return bad_quality / (bad_quality + (route_count - 1))


def throttle_target(throttled_observation: float = 0.7) -> float:
    """Return expected share of a throttled route under pure ThrottledObservation.

    When ThrottledObservation = q (default 0.7 from pkg/routestats/routestats.go:390)
    acts alone without Retry-After penalty, the first-order share approximation is
    q / (1 + q).

    Args:
        throttled_observation: The ThrottledObservation factor (default 0.7).

    Returns:
        Expected share proportion [0, 1] of the throttled route.
    """
    if not (0 < throttled_observation):
        raise ValueError("throttled_observation must be > 0")
    return throttled_observation / (1 + throttled_observation)


def evaluate_process_stability(
    windows: list[dict],
    thresholds: dict,
) -> dict:
    """Evaluate S6 process stability across consecutive share windows.

    Args:
        windows: List of window dicts, each must contain at least:
            - "share": route share proportion [0, 1]
            - "corr_p99": correlation p99 value
            - "samples": number of samples in this window
        thresholds: Dict with keys:
            - "corr_p99_headroom_min": minimum required headroom from corr_p99 to max (default 0.20 = 20%)
            - "share_stddev_max_pp": maximum allowed share stddev in percentage points (e.g., 0.06 for 6pp)
            - "consecutive_breach_max": maximum allowed consecutive out-of-bounds windows (default 2)
            - "min_window_samples": minimum samples per window, below = DATA_INVALID (default 100)
            - "corr_p99_max": maximum corr_p99 bound (default 2.0, from ShareCorrMax)

    Returns:
        Dict with:
            - "ok": bool, all criteria met
            - "reasons": list[str], empty on success, failure explanations otherwise
            - "share_stddev_pp": float, observed share stddev in percentage points
            - "max_consecutive_breach": int, longest streak of share out-of-bounds windows
            - "corr_p99_headroom": float, minimum headroom observed (as fraction of max)
            - "insufficient_windows": list[int], indices of windows with samples < min_window_samples
    """
    if not windows:
        return {
            "ok": False,
            "reasons": ["no windows provided"],
            "share_stddev_pp": 0.0,
            "max_consecutive_breach": 0,
            "corr_p99_headroom": 0.0,
            "insufficient_windows": [],
        }

    reasons: list[str] = []

    # Extract config with defaults
    corr_max = thresholds.get("corr_p99_max", 2.0)
    headroom_min = thresholds.get("corr_p99_headroom_min", 0.20)
    share_stddev_max = thresholds.get("share_stddev_max_pp", 0.06)
    consecutive_max = thresholds.get("consecutive_breach_max", 2)
    min_samples = thresholds.get("min_window_samples", 100)

    shares = [w.get("share", 0.0) for w in windows]
    corr_p99_vals = [w.get("corr_p99", 0.0) for w in windows]
    samples_list = [w.get("samples", 0) for w in windows]

    # Check insufficient samples
    insufficient = [i for i, s in enumerate(samples_list) if s < min_samples]
    if insufficient:
        reasons.append(
            f"windows {insufficient} have samples < {min_samples} (DATA_INVALID)"
        )

    # Compute share stddev (population stddev)
    if len(shares) > 1:
        mean_share = sum(shares) / len(shares)
        variance = sum((s - mean_share) ** 2 for s in shares) / len(shares)
        share_stddev = math.sqrt(variance)
    else:
        share_stddev = 0.0
    share_stddev_pp = share_stddev * 100  # convert to percentage points

    if share_stddev_pp > share_stddev_max * 100:
        reasons.append(
            f"share stddev {share_stddev_pp:.2f}pp > max {share_stddev_max * 100:.1f}pp"
        )

    # Check corr_p99 headroom: (corr_max - corr_p99) / corr_max >= headroom_min
    min_headroom = 1.0
    for cp in corr_p99_vals:
        if corr_max > 0:
            headroom = (corr_max - cp) / corr_max
            if headroom < min_headroom:
                min_headroom = headroom
    if min_headroom < headroom_min:
        reasons.append(
            f"corr_p99 headroom {min_headroom:.2%} < required {headroom_min:.0%} "
            f"(max corr_p99 {max(corr_p99_vals):.2f} vs bound {corr_max})"
        )
    # Check consecutive out-of-bounds windows
    # Out-of-bounds means share outside [target - tol, target + tol] but we don't have target/tol here.
    # Instead, define breach as share deviating from median by more than share_stddev_max.
    # Actually for S6 the spec says: "连续越界窗口数 ≤2" — use median as center, breach = |share - median| > share_stddev_max
    median_share = sorted(shares)[len(shares) // 2]
    breach_threshold = share_stddev_max
    max_consecutive = 0
    current_streak = 0
    for s in shares:
        if abs(s - median_share) > breach_threshold:
            current_streak += 1
            if current_streak > max_consecutive:
                max_consecutive = current_streak
        else:
            current_streak = 0

    if max_consecutive > consecutive_max:
        reasons.append(
            f"max consecutive breach windows {max_consecutive} > allowed {consecutive_max}"
        )

    return {
        "ok": len(reasons) == 0,
        "reasons": reasons,
        "share_stddev_pp": share_stddev_pp,
        "max_consecutive_breach": max_consecutive,
        "corr_p99_headroom": min_headroom,
        "insufficient_windows": insufficient,
    }


def scenario_targets() -> dict[str, dict[str, Any]]:
     """Return official judgment criteria for S1, S2, S3 per issue #418.
 
     All three scenarios use two routes with identical static_weight=100, same
     quality and same upstream behaviour, so EWMA routing should split ~50/50
     absent latency differences. S2/S3 inject a TTFT (time-to-first-token) skew
     so the latency-sensitive EWMA favouritism can be measured against a known
     target share for the subject route.
 
     Returns:
         Dict keyed by scenario name ("S1", "S2", "S3"), each containing:
         - "target": expected share (proportion) of the subject route
         - "tol_pp": tolerance in percentage points (e.g., 0.02 for ±2pp)
         - "ci_bounds": (min_ci, max_ci) allowed CI range (proportions)
         - "subject": route being judged ("A" or "B")
         - "injection": mock mode per route, {"A": mode, "B": mode};
                       runner consumes this to set upstream mock headers
         - "min_samples": minimum n so that (z+margin_z)*SE ≤ tol_pp (default 13000)
         - "description": human-readable summary
     """
     return {
         "S1": {
             "target": 0.50,
             "tol_pp": 0.02,
             "ci_bounds": (0.48, 0.52),
             "subject": "A",
             "injection": {"A": "ttft_2000", "B": "ttft_2000"},
             "min_samples": 13000,
             "description": "S1 equivalence: two routes same static_weight=100, same "
             "quality, same upstream behaviour (both ttft_2000); expect A=50.0% "
             "±2pp, CI⊂[48%,52%]; n≥13000 so ±2pp CI criterion feasible",
         },
         "S2": {
             "target": 0.467,
             "tol_pp": 0.02,
             "ci_bounds": (0.447, 0.487),
             "subject": "B",
             "injection": {"A": "ttft_2000", "B": "ttft_4000"},
             "min_samples": 13000,
             "description": "S2 latency sensitivity: two routes same static_weight=100; "
             "B injects streaming first-token 4000ms (2× A's 2000ms TTFT); expect "
             "B=46.7% ±2pp, CI⊂[44.7%,48.7%]; n≥13000 so ±2pp CI criterion feasible",
         },
         "S3": {
             "target": 0.529,
             "tol_pp": 0.02,
             "ci_bounds": (0.509, 0.549),
             "subject": "B",
             "injection": {"A": "ttft_2000", "B": "ttft_500"},
             "min_samples": 13000,
             "description": "S3 reverse latency advantage: two routes same "
             "static_weight=100; A ttft_2000ms, B ttft_500ms (B 4× faster); expect "
             "B=52.9% ±2pp, CI⊂[50.9%,54.9%]; n≥13000 so ±2pp CI criterion feasible",
         },
         "S4_NORETRY": {
             "target": 0.38,
             "tol_pp": 0.03,
             "ci_bounds": (0.35, 0.41),
             "subject": "B",
             "injection": {"A": "ok", "B": "ratelimit_missing"},
             "min_samples": 5400,
             "throttle_only_share": 0.4117647058823529,
             "description": "S4 throttling no Retry-After: two routes static_weight=100; "
             "B returns 429 without Retry-After header; expect B=38.0% ±3pp, "
             "CI⊂[35%,41%]; n≥5400. Note: throttle_only_share=0.4118 is the "
             "theoretical share if ONLY ThrottledObservation=0.7 applies (q/(1+q)). "
             "If observed share ~41% instead of 38%, Retry-After is NOT counted in TTFT → PRODUCT_FAIL.",
         },
         "S4_RETRY5": {
             "target": 0.38,
             "tol_pp": 0.03,
             "ci_bounds": (0.35, 0.41),
             "subject": "B",
             "injection": {"A": "ok", "B": "ratelimit_5s"},
             "min_samples": 5400,
             "throttle_only_share": 0.4117647058823529,
             "description": "S4 throttling Retry-After 5s: two routes static_weight=100; "
             "B returns 429 with Retry-After: 5; expect B=38.0% ±3pp, "
             "CI⊂[35%,41%]; n≥5400. throttle_only_share=0.4118 for comparison. "
             "If observed share ~41% instead of 38%, Retry-After is NOT counted in TTFT → PRODUCT_FAIL.",
         },
         "S4_RETRY10": {
             "target": 0.38,
             "tol_pp": 0.03,
             "ci_bounds": (0.35, 0.41),
             "subject": "B",
             "injection": {"A": "ok", "B": "ratelimit_10s"},
             "min_samples": 5400,
             "throttle_only_share": 0.4117647058823529,
             "description": "S4 throttling Retry-After 10s: two routes static_weight=100; "
             "B returns 429 with Retry-After: 10; expect B=38.0% ±3pp, "
             "CI⊂[35%,41%]; n≥5400. throttle_only_share=0.4118 for comparison. "
             "If observed share ~41% instead of 38%, Retry-After is NOT counted in TTFT → PRODUCT_FAIL.",
         },
         "S5_POOL2": {
             "target": bad_route_target(2, 0.5),
             "tol_pp": 0.03,
             "ci_bounds": (0.303, 0.363),
             "subject": "BAD",
             "route_count": 2,
             "injection": {"BAD": "q05", "GOOD": "ok"},
             "min_samples": 5100,
             "description": "S5 pool 2 routes: same group/alias static_weight=100; "
             "one route q05 (crc32 deterministic 50% failure ≡ quality 0.5), other ok (quality 1.0); "
             "BAD target=33.33% ±3pp, CI⊂[30.3%,36.3%]; n≥5100",
         },
         "S5_POOL4": {
             "target": bad_route_target(4, 0.5),
             "tol_pp": 0.03,
             "ci_bounds": (0.113, 0.173),
             "subject": "BAD",
             "route_count": 4,
             "injection": {"BAD": "q05", "GOOD": "ok"},
             "min_samples": 2800,
             "description": "S5 pool 4 routes: same group/alias static_weight=100; "
             "one route q05 (quality 0.5), three ok (quality 1.0); "
             "BAD target=14.29% ±3pp, CI⊂[11.3%,17.3%]; n≥2800",
         },
         "S5_POOL8": {
             "target": bad_route_target(8, 0.5),
             "tol_pp": 0.02,
             "ci_bounds": (0.0467, 0.0867),
             "subject": "BAD",
             "route_count": 8,
             "injection": {"BAD": "q05", "GOOD": "ok"},
             "min_samples": 3300,
             "description": "S5 pool 8 routes: same group/alias static_weight=100; "
             "one route q05 (quality 0.5), seven ok (quality 1.0); "
             "BAD target=6.67% ±2pp, CI⊂[4.67%,8.67%]; n≥3300",
         },
         "S6_W50": {
             "target": None,
             "tol_pp": None,
             "ci_bounds": None,
             "subject": "STABLE",
             "route_count": 4,
             "share_window_size": 50,
             "process_thresholds": {
                 "corr_p99_headroom_min": 0.20,
                 "share_stddev_max_pp": 0.06,
                 "consecutive_breach_max": 2,
                 "min_window_samples": 100,
                 "corr_p99_max": 2.0,
             },
             "injection": {"STABLE": "ok", "SLOW": "ttft_4000", "STEP": "ttft_4000→ttft_500"},
             "step_at_ratio": 0.5,
             "min_tail_seconds": 90,
             "description": "S6 window=50: four routes (two STABLE ok, one SLOW 2×TTFT, one STEP ttft_4000→ttft_500 at 50% progress); "
             "no point-estimate CI judgment; evaluate process stability: "
             "corr_p99 headroom >20%, share stddev ≤6pp, consecutive breach ≤2, "
             "min 100 samples/window; step follow by EWMA ±1σ band; window<100 samples = DATA_INVALID",
         },
         "S6_W200": {
             "target": None,
             "tol_pp": None,
             "ci_bounds": None,
             "subject": "STABLE",
             "route_count": 4,
             "share_window_size": 200,
             "process_thresholds": {
                 "corr_p99_headroom_min": 0.20,
                 "share_stddev_max_pp": 0.03,
                 "consecutive_breach_max": 2,
                 "min_window_samples": 100,
                 "corr_p99_max": 2.0,
             },
             "injection": {"STABLE": "ok", "SLOW": "ttft_4000", "STEP": "ttft_4000→ttft_500"},
             "step_at_ratio": 0.5,
             "min_tail_seconds": 90,
             "description": "S6 window=200: four routes (two STABLE ok, one SLOW 2×TTFT, one STEP ttft_4000→ttft_500 at 50% progress); "
             "no point-estimate CI judgment; evaluate process stability: "
             "corr_p99 headroom >20%, share stddev ≤3pp, consecutive breach ≤2, "
             "min 100 samples/window; step follow by EWMA ±1σ band; window<100 samples = DATA_INVALID",
         },
         "S6_W1000": {
             "target": None,
             "tol_pp": None,
             "ci_bounds": None,
             "subject": "STABLE",
             "route_count": 4,
             "share_window_size": 1000,
             "process_thresholds": {
                 "corr_p99_headroom_min": 0.20,
                 "share_stddev_max_pp": 0.03,
                 "consecutive_breach_max": 2,
                 "min_window_samples": 100,
                 "corr_p99_max": 2.0,
             },
             "injection": {"STABLE": "ok", "SLOW": "ttft_4000", "STEP": "ttft_4000→ttft_500"},
             "step_at_ratio": 0.5,
             "min_tail_seconds": 90,
             "description": "S6 window=1000: four routes (two STABLE ok, one SLOW 2×TTFT, one STEP ttft_4000→ttft_500 at 50% progress); "
             "no point-estimate CI judgment; evaluate process stability: "
             "corr_p99 headroom >20%, share stddev ≤3pp, consecutive breach ≤2, "
             "min 100 samples/window; step follow by EWMA ±1σ band; window<100 samples = DATA_INVALID",
         },
     }