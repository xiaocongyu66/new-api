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
        ci_bounds: (min_ci, max_ci) allowed CI range (proportions), or None when
            the scenario judges only the point estimate and sets no CI-containment
            criterion (e.g. S13, which measures recovery rather than convergence).

    Returns:
        Dict with "ok" (bool) and "reasons" (list[str]).
        Reasons are empty on success; on failure, human-readable explanations.
    """
    reasons: list[str] = []
    ci_low, ci_high = ci

    # Check point estimate within target ± tol_pp
    if not (target - tol_pp <= observed_share <= target + tol_pp):
        reasons.append(
            f"point {observed_share:.4f} outside {target:.3f}±{tol_pp:.2f}"
        )

    # Check CI fully within bounds, when the scenario defines any.
    if ci_bounds is not None:
        min_ci, max_ci = ci_bounds
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

def evaluate_corr_headroom(
    corr_snapshots: list[dict[str, list[float]]],
    targets: dict[str, Any],
) -> dict[str, Any]:
    """Evaluate S7 corr walk vs clamp headroom.

    Args:
        corr_snapshots: list of per-route corr dicts, each:
            {"route_label": [corr_val, ...], ...}
        targets: scenario target dict with corr_p99_max, corr_p99_headroom_min,
            min_corr_snapshots

    Returns:
        Dict with ok, reasons, corr_p99, corr_min, corr_max, corr_p50, corr_p95,
        headroom, per_route_counts, total_snapshots
    """
    corr_max_bound = targets.get("corr_p99_max", 2.0)
    headroom_min = targets.get("corr_p99_headroom_min", 0.20)
    min_snapshots = targets.get("min_corr_snapshots", 10000)

    all_corr_vals: list[float] = []
    per_route_counts: dict[str, int] = {}
    for snap in corr_snapshots:
        for label, vals in snap.items():
            all_corr_vals.extend(vals)
            per_route_counts[label] = per_route_counts.get(label, 0) + len(vals)

    reasons: list[str] = []

    if not all_corr_vals:
        return {
            "ok": False,
            "reasons": ["no corr snapshots collected"],
            "corr_p99": 0.0,
            "corr_min": 0.0,
            "corr_max": 0.0,
            "corr_p50": 0.0,
            "corr_p95": 0.0,
            "headroom": 0.0,
            "per_route_counts": {},
            "total_snapshots": 0,
        }

    total = len(all_corr_vals)
    per_route_min = min(per_route_counts.values()) if per_route_counts else 0
    if per_route_min < min_snapshots:
        reasons.append(
            f"min per-route corr snapshots {per_route_min} < required {min_snapshots} (DATA_INVALID)"
        )

    p = percentiles(all_corr_vals, [50, 95, 99])
    corr_p50 = p["p50"]
    corr_p95 = p["p95"]
    corr_p99 = p["p99"]
    corr_min = min(all_corr_vals)
    corr_max = max(all_corr_vals)

    if corr_max_bound > 0:
        headroom = (corr_max_bound - corr_p99) / corr_max_bound
    else:
        headroom = 0.0

    if headroom < headroom_min:
        reasons.append(
            f"corr p99 headroom {headroom:.2%} < required {headroom_min:.0%} "
            f"(corr_p99={corr_p99:.3f} vs bound {corr_max_bound})"
        )

    return {
        "ok": len(reasons) == 0,
        "reasons": reasons,
        "corr_p99": corr_p99,
        "corr_min": corr_min,
        "corr_max": corr_max,
        "corr_p50": corr_p50,
        "corr_p95": corr_p95,
        "headroom": headroom,
        "per_route_counts": per_route_counts,
        "total_snapshots": total,
    }


def evaluate_memory_scaling(
    snapshots: list[dict[str, Any]],
    targets: dict[str, Any],
) -> dict[str, Any]:
    """Evaluate S8 memory scaling: heap/pool count consistency.

    Args:
        snapshots: list of resource+pool snapshots taken during steady state:
            {"rss": int, "heap": int|None, "pool_count": int, "active_pools": int, "ts": float}
        targets: scenario target dict

    Returns:
        Dict with ok, reasons, heap_baseline, heap_peak, heap_post_sweep,
        rss_peak, pool_count_peak, active_pool_match, monotonic_growth
    """
    reasons: list[str] = []

    if not snapshots:
        return {
            "ok": False,
            "reasons": ["no snapshots collected"],
            "heap_baseline": None,
            "heap_peak": None,
            "heap_post_sweep": None,
            "rss_peak": None,
            "pool_count_peak": 0,
            "active_pool_match": False,
            "monotonic_growth": False,
        }

    heaps = [s.get("heap") for s in snapshots if s.get("heap") is not None]
    rss_vals = [s.get("rss", 0) for s in snapshots]
    pool_counts = [s.get("pool_count", 0) for s in snapshots]
    active_counts = [s.get("active_pools", 0) for s in snapshots]

    heap_baseline = heaps[0] if heaps else None
    heap_peak = max(heaps) if heaps else None
    heap_post_sweep = heaps[-1] if heaps else None
    rss_peak = max(rss_vals) if rss_vals else 0
    pool_count_peak = max(pool_counts) if pool_counts else 0

    # Check pool_count matches active_pools at each snapshot
    pool_mismatches = sum(
        1 for pc, ac in zip(pool_counts, active_counts) if pc != ac
    )
    active_pool_match = pool_mismatches == 0
    if not active_pool_match:
        reasons.append(
            f"pool_count != active_pools in {pool_mismatches}/{len(snapshots)} snapshots"
        )

    # Check for unexplained monotonic heap growth (last > 2× first, no sweep)
    monotonic_growth = False
    if len(heaps) >= 10:
        if heap_peak and heap_baseline and heap_peak > 2 * heap_baseline:
            # Check if it stabilized (last 3 samples within 10% of peak)
            tail = heaps[-3:]
            if tail and max(tail) > 0.9 * heap_peak:
                monotonic_growth = True
                reasons.append(
                    f"unexplained heap growth: baseline={heap_baseline} peak={heap_peak} "
                    f"(2× baseline, not swept)"
                )

    return {
        "ok": len(reasons) == 0,
        "reasons": reasons,
        "heap_baseline": heap_baseline,
        "heap_peak": heap_peak,
        "heap_post_sweep": heap_post_sweep,
        "rss_peak": rss_peak,
        "pool_count_peak": pool_count_peak,
        "active_pool_match": active_pool_match,
        "monotonic_growth": monotonic_growth,
    }


def evaluate_affinity_scan(
    share_rows: dict[str, dict[str, float]],
    targets: dict[str, Any],
) -> dict[str, Any]:
    """Evaluate S9 affinity ratio scan: monotonicity and correction effect.

    Args:
        share_rows: dict keyed by "ratio_Wsize" -> {"A_share": float, "A_ci_low": float, "A_ci_high": float}
        targets: scenario target dict (unused, kept for API symmetry)

    Returns:
        Dict with ok, reasons, monotonic, correction_significant, per_combo
    """
    reasons: list[str] = []

    # Extract ratios and window sizes
    ratios = sorted(set(
        float(k.split("_")[0]) for k in share_rows
    ))
    w0_shares: dict[float, float] = {}
    w200_shares: dict[float, float] = {}
    for key, data in share_rows.items():
        parts = key.split("_")
        ratio = float(parts[0])
        wsize = int(parts[1])
        if wsize == 0:
            w0_shares[ratio] = data["A_share"]
        else:
            w200_shares[ratio] = data["A_share"]

    # Monotonicity: A share must increase with ratio for both window sizes
    monotonic = True
    for shares_dict, wname in [(w0_shares, "W0"), (w200_shares, "W200")]:
        sorted_ratios = sorted(shares_dict.keys())
        for i in range(1, len(sorted_ratios)):
            if shares_dict[sorted_ratios[i]] < shares_dict[sorted_ratios[i - 1]]:
                monotonic = False
                reasons.append(
                    f"{wname}: A share decreased from {sorted_ratios[i-1]:.0%} to {sorted_ratios[i]:.0%} "
                    f"({shares_dict[sorted_ratios[i-1]]:.4f} → {shares_dict[sorted_ratios[i]]:.4f})"
                )

    # Correction significance: at 50%, W200 must be significantly lower than W0
    correction_significant = False
    if 0.5 in w0_shares and 0.5 in w200_shares:
        diff = w0_shares[0.5] - w200_shares[0.5]
        if diff > 0.02:  # at least 2pp lower
            correction_significant = True
        else:
            reasons.append(
                f"50% ratio: W200 ({w200_shares[0.5]:.4f}) not significantly lower than W0 ({w0_shares[0.5]:.4f})"
            )

    return {
        "ok": len(reasons) == 0,
        "reasons": reasons,
        "monotonic": monotonic,
        "correction_significant": correction_significant,
        "w0_shares": w0_shares,
        "w200_shares": w200_shares,
    }


def aggregate_global_share(attempts: list[dict[str, Any]], subject_identity: tuple) -> dict[str, Any]:
    """S10: re-aggregate raw attempt quadruples into one global share.

    Share windows are process-local, so with N replicas each pod converges on its
    own view. #418 forbids averaging per-pod percentages: the only sound global
    number comes from pooling raw attempts and dividing once. This function is
    that single division, plus the per-pod breakdown for reporting only.

    Args:
        attempts: audit rows, each with channel_id / key_index / upstream_model
            and optionally `pod` (replica identity).
        subject_identity: (channel_id, key_index, upstream_model) of the subject.

    Returns:
        Dict with global_share, global_selections, global_total, per_pod
        (pod -> {selections, total, share} for reporting), pods_seen.
    """
    global_total = 0
    global_selections = 0
    per_pod: dict[str, dict[str, Any]] = {}

    for a in attempts:
        identity = (a.get("channel_id"), a.get("key_index"), a.get("upstream_model"))
        pod = str(a.get("pod", "single"))
        bucket = per_pod.setdefault(pod, {"selections": 0, "total": 0, "share": 0.0})
        bucket["total"] += 1
        global_total += 1
        if identity == subject_identity:
            bucket["selections"] += 1
            global_selections += 1

    for bucket in per_pod.values():
        bucket["share"] = (bucket["selections"] / bucket["total"]) if bucket["total"] else 0.0

    return {
        "global_share": (global_selections / global_total) if global_total else 0.0,
        "global_selections": global_selections,
        "global_total": global_total,
        "per_pod": per_pod,
        "pods_seen": sorted(per_pod.keys()),
    }


def evaluate_kill_switch(before: dict[str, Any], after: dict[str, Any], targets: dict[str, Any]) -> dict[str, Any]:
    """S10: verify RouteStatsEnabled=false survives a restart and neutralises correction.

    Args:
        before/after: each {"option": str|None, "corrections": list[float],
            "window_entries": int, "pid": str|None}
        targets: scenario dict (reads neutral_correction, default 1.0).

    Returns:
        Dict with ok, reasons, option_persisted, corrections_neutral,
        window_stopped, restarted, non_neutral.
    """
    neutral = targets.get("neutral_correction", 1.0)
    reasons: list[str] = []

    option_persisted = str(after.get("option")).strip().lower() == "false"
    if not option_persisted:
        reasons.append(
            f"RouteStatsEnabled did not persist as false across restart (after={after.get('option')!r})"
        )

    after_corr = after.get("corrections") or []
    if not after_corr:
        reasons.append("no post-restart corrections sampled (DATA_INVALID)")
    non_neutral = [c for c in after_corr if abs(c - neutral) > 1e-9]
    corrections_neutral = bool(after_corr) and not non_neutral
    if non_neutral:
        reasons.append(
            f"{len(non_neutral)} post-restart corrections are not neutral {neutral}: "
            f"{non_neutral[:5]}"
        )

    # The kill switch must stop the window growing. Equal counts are acceptable
    # (nothing added); growth means the window kept allocating.
    before_entries = before.get("window_entries", 0) or 0
    after_entries = after.get("window_entries", 0) or 0
    window_stopped = after_entries <= before_entries
    if not window_stopped:
        reasons.append(
            f"share window kept allocating after kill switch: {before_entries} -> {after_entries}"
        )

    # A restart that never happened invalidates the whole premise.
    before_pid, after_pid = before.get("pid"), after.get("pid")
    restarted = bool(before_pid and after_pid and before_pid != after_pid)
    if not restarted:
        reasons.append(
            f"no restart observed (pid {before_pid!r} -> {after_pid!r}); "
            "the persistence claim is unproven (DATA_INVALID)"
        )

    return {
        "ok": len(reasons) == 0,
        "reasons": reasons,
        "option_persisted": option_persisted,
        "corrections_neutral": corrections_neutral,
        "window_stopped": window_stopped,
        "restarted": restarted,
        "non_neutral": non_neutral,
    }


def evaluate_path_audit(
    attempts: list[dict[str, Any]],
    probe_opportunities: int,
    window_paths: list[str],
    targets: dict[str, Any],
) -> dict[str, Any]:
    """S11: turn "selected route == counted route" into a checked data contract.

    Three independent failure modes, all PRODUCT_FAIL per #418:
      1. any attempt missing its path label or its identity quadruple
      2. a probe inside the share window (probe must yield opportunity_count=0)
      3. real bypass traffic (affinity / specific) absent from the window

    Args:
        attempts: audit rows carrying `path` plus the identity quadruple.
        probe_opportunities: opportunity count attributable to probes; must be 0.
        window_paths: path labels observed inside the share window.
        targets: scenario dict (reads user_paths, min_per_path).

    Returns:
        Dict with ok, reasons, per_path_counts, unlabelled, incomplete_quadruples,
        probe_isolated, bypass_in_window, paths_missing.
    """
    expected_paths = list(targets.get("user_paths", ["weighted", "affinity", "specific"]))
    min_per_path = targets.get("min_per_path", 100)
    reasons: list[str] = []

    identity_fields = ("group", "alias", "channel_id", "key_index", "upstream_model")
    per_path_counts: dict[str, int] = {}
    unlabelled = 0
    incomplete_quadruples: list[dict[str, Any]] = []

    for a in attempts:
        path = (a.get("path") or "").strip()
        if path:
            per_path_counts[path] = per_path_counts.get(path, 0) + 1
        else:
            unlabelled += 1
        missing = [f for f in identity_fields if a.get(f) in (None, "")]
        # key_index 0 is legitimate; only a genuinely absent field counts.
        missing = [f for f in missing if not (f == "key_index" and a.get(f) == 0)]
        if missing:
            incomplete_quadruples.append(
                {"request_id": a.get("client_request_id") or a.get("request_id"), "missing": missing}
            )

    if not attempts:
        reasons.append("no attempts collected (DATA_INVALID)")
    if unlabelled:
        reasons.append(f"{unlabelled} attempts carry no path label")
    if incomplete_quadruples:
        reasons.append(
            f"{len(incomplete_quadruples)} attempts have an incomplete identity quadruple: "
            f"{incomplete_quadruples[:3]}"
        )

    paths_missing = [p for p in expected_paths if per_path_counts.get(p, 0) < min_per_path]
    if paths_missing:
        reasons.append(
            f"paths below {min_per_path} samples: "
            f"{ {p: per_path_counts.get(p, 0) for p in paths_missing} } (DATA_INVALID)"
        )

    probe_isolated = probe_opportunities == 0
    if not probe_isolated:
        reasons.append(
            f"probe traffic contributed {probe_opportunities} share opportunities; probes must "
            "produce EWMA signal with opportunity_count=0"
        )

    bypass_labels = [p for p in ("affinity", "specific") if p in expected_paths]
    observed_window = set(window_paths)
    bypass_missing = [p for p in bypass_labels if p not in observed_window]
    bypass_in_window = not bypass_missing
    if bypass_missing:
        reasons.append(
            f"bypass paths {bypass_missing} never entered the share window; correction would be "
            "blind to the skew they cause"
        )

    return {
        "ok": len(reasons) == 0,
        "reasons": reasons,
        "per_path_counts": per_path_counts,
        "unlabelled": unlabelled,
        "incomplete_quadruples": incomplete_quadruples,
        "probe_isolated": probe_isolated,
        "bypass_in_window": bypass_in_window,
        "paths_missing": paths_missing,
    }


def evaluate_retry_attribution(attempt_stats: dict[str, Any], targets: dict[str, Any]) -> dict[str, Any]:
    """S12: did a retry-absorbed failure still reach quality and the share window?

    Args:
        attempt_stats: b_attempt_failures, b_user_failures, b_ewma_before,
            b_ewma_after, b_window_selections, b_total_attempts.
        targets: scenario dict (unused; kept for signature symmetry).

    Returns:
        Dict with ok, reasons, quality_observed, window_observed, attribution,
        attempt_failure_rate, user_failure_rate, ewma_delta.
    """
    failures = attempt_stats.get("b_attempt_failures", 0) or 0
    user_failures = attempt_stats.get("b_user_failures", 0) or 0
    ewma_before = attempt_stats.get("b_ewma_before", 0.0) or 0.0
    ewma_after = attempt_stats.get("b_ewma_after", 0.0) or 0.0
    window_selections = attempt_stats.get("b_window_selections", 0) or 0
    total = attempt_stats.get("b_total_attempts", 0) or 0

    ewma_delta = ewma_after - ewma_before
    quality_observed = failures > 0 and ewma_after < ewma_before
    window_observed = failures > 0 and window_selections >= failures

    if quality_observed and window_observed:
        attribution = "both"
    elif quality_observed:
        attribution = "quality_only"
    elif window_observed:
        attribution = "window_only"
    else:
        attribution = "neither"

    reasons: list[str] = []
    if total == 0:
        reasons.append("no attempts recorded for B (DATA_INVALID)")
    elif attribution == "neither":
        reasons.append(
            "failed attempts reached neither EWMA quality nor the share window: a persistently "
            "failing route can never be de-weighted (PRODUCT_FAIL)"
        )
    elif attribution == "quality_only":
        reasons.append(
            "partial product risk: failed attempts moved EWMA quality but did not take share "
            "window slots, so the correction cannot see the failing load"
        )
    elif attribution == "window_only":
        reasons.append(
            "partial product risk: failed attempts took share window slots but did not move EWMA "
            "quality, so repeated failure does not lower the route's score"
        )

    return {
        "ok": len(reasons) == 0,
        "reasons": reasons,
        "quality_observed": quality_observed,
        "window_observed": window_observed,
        "attribution": attribution,
        "attempt_failure_rate": (failures / total) if total else 0.0,
        "user_failure_rate": (user_failures / total) if total else 0.0,
        "ewma_delta": ewma_delta,
    }


def evaluate_recovery(samples: list[dict[str, Any]], targets: dict[str, Any]) -> dict[str, Any]:
    """S13: correction stays clamped through degradation, and share recovers after.

    Args:
        samples: ordered list of {"ts", "phase" in {steady,fault,recover},
            "b_share", "corr" (per-route dict or scalar), "samples"}.
        targets: reads corr_min, corr_max, target, recovery_tolerance_pp.

    Returns:
        Dict with ok, reasons, corr_in_clamp, corr_violations, recovery_seconds,
        recovered, steady_share, fault_share, post_recovery_share,
        degradation_observed.
    """
    corr_min = targets.get("corr_min", 0.5)
    corr_max = targets.get("corr_max", 2.0)
    target = targets.get("target", 0.5)
    tol = targets.get("recovery_tolerance_pp", 0.03)

    empty = {
        "ok": False,
        "reasons": ["no samples collected (DATA_INVALID)"],
        "corr_in_clamp": False,
        "corr_violations": [],
        "recovery_seconds": None,
        "recovered": False,
        "steady_share": None,
        "fault_share": None,
        "post_recovery_share": None,
        "degradation_observed": False,
    }
    if not samples:
        return empty

    reasons: list[str] = []

    # Corrections are checked in every phase: an out-of-clamp value during
    # degradation is exactly the defect this scenario hunts.
    corr_violations: list[dict[str, Any]] = []
    for s in samples:
        corr = s.get("corr")
        if isinstance(corr, dict):
            pairs = list(corr.items())
        elif corr is None:
            pairs = []
        else:
            pairs = [("subject", corr)]
        for route, value in pairs:
            if value is None:
                continue
            if value < corr_min or value > corr_max:
                corr_violations.append(
                    {"ts": s.get("ts"), "phase": s.get("phase"), "route": route, "corr": value}
                )
    corr_in_clamp = not corr_violations
    if corr_violations:
        reasons.append(
            f"{len(corr_violations)} corrections outside clamp [{corr_min}, {corr_max}]: "
            f"{corr_violations[:3]} (PRODUCT_FAIL)"
        )

    by_phase: dict[str, list[dict[str, Any]]] = {}
    for s in samples:
        by_phase.setdefault(s.get("phase", ""), []).append(s)

    def mean_share(phase: str) -> float | None:
        rows = by_phase.get(phase) or []
        vals = [r.get("b_share") for r in rows if r.get("b_share") is not None]
        return (sum(vals) / len(vals)) if vals else None

    steady_share = mean_share("steady")
    fault_share = mean_share("fault")

    missing_phases = [p for p in ("steady", "fault", "recover") if not by_phase.get(p)]
    if missing_phases:
        reasons.append(f"phases missing from samples: {missing_phases} (DATA_INVALID)")

    # If the injection never bit, there is nothing to recover from: that is bad
    # data, not a product verdict.
    degradation_observed = False
    if steady_share is not None and fault_share is not None:
        degradation_observed = fault_share < steady_share - tol
        if not degradation_observed:
            reasons.append(
                f"fault phase share {fault_share:.4f} is not meaningfully below steady "
                f"{steady_share:.4f}: the injection did not degrade B (DATA_INVALID)"
            )

    recover_rows = by_phase.get("recover") or []
    recovery_seconds = None
    recovered = False
    post_recovery_share = None
    if recover_rows:
        start_ts = recover_rows[0].get("ts")
        post_recovery_share = recover_rows[-1].get("b_share")
        for r in recover_rows:
            share = r.get("b_share")
            if share is None:
                continue
            if abs(share - target) <= tol:
                recovered = True
                if start_ts is not None and r.get("ts") is not None:
                    recovery_seconds = max(0.0, r["ts"] - start_ts)
                break
        if not recovered:
            reasons.append(
                f"B never returned within +/-{tol * 100:.0f}pp of {target:.2f} before the recovery "
                f"deadline (last {post_recovery_share}) (PRODUCT_FAIL)"
            )

    return {
        "ok": len(reasons) == 0,
        "reasons": reasons,
        "corr_in_clamp": corr_in_clamp,
        "corr_violations": corr_violations,
        "recovery_seconds": recovery_seconds,
        "recovered": recovered,
        "steady_share": steady_share,
        "fault_share": fault_share,
        "post_recovery_share": post_recovery_share,
        "degradation_observed": degradation_observed,
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
        "S7_W50": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "CORR",
            "route_count": 4,
            "share_window_size": 50,
            "min_corr_snapshots": 10000,
            "corr_p99_headroom_min": 0.20,
            "corr_p99_max": 2.0,
            "corr_snapshot_interval_s": 0.25,
            "max_duration_s": 900,
            "injection": {"STABLE": "ok", "SLOW": "ttft_4000", "STEP": "ttft_4000→ttft_500"},
            "description": "S7 window=50: corr walk vs clamp headroom; four routes (two ok, one 2×TTFT, one step); "
            "collect ≥10,000 share_correction snapshots per route at ≤250ms interval; "
            "corr p99 headroom from ShareCorrMax=2.0 must be >20%; <10k snapshots = DATA_INVALID",
        },
        "S7_W200": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "CORR",
            "route_count": 4,
            "share_window_size": 200,
            "min_corr_snapshots": 10000,
            "corr_p99_headroom_min": 0.20,
            "corr_p99_max": 2.0,
            "corr_snapshot_interval_s": 0.25,
            "max_duration_s": 900,
            "injection": {"STABLE": "ok", "SLOW": "ttft_4000", "STEP": "ttft_4000→ttft_500"},
            "description": "S7 window=200: corr walk vs clamp headroom; same topology as S7_W50; "
            "collect ≥10,000 share_correction snapshots per route; corr p99 headroom >20%",
        },
        "S7_W1000": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "CORR",
            "route_count": 4,
            "share_window_size": 1000,
            "min_corr_snapshots": 10000,
            "corr_p99_headroom_min": 0.20,
            "corr_p99_max": 2.0,
            "corr_snapshot_interval_s": 0.25,
            "max_duration_s": 900,
            "injection": {"STABLE": "ok", "SLOW": "ttft_4000", "STEP": "ttft_4000→ttft_500"},
            "description": "S7 window=1000: corr walk vs clamp headroom; same topology as S7_W50; "
            "collect ≥10,000 share_correction snapshots per route; corr p99 headroom >20%",
        },
        "S8_3K": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "MEMORY",
            "route_count": 3000,
            "share_window_size": 200,
            "scale": {"channels": 50, "aliases": 20, "groups": 3},
            "steady_duration_s": 2160,
            "candidates_per_pool": 8,
            "description": "S8 scale 3K: 50 channel × 20 alias × 3 group = 3,000 route; "
            "measure heap baseline/peak/post-sweep, RSS, SharePoolCount(), active pool count; "
            "pool count must equal active pools; orphan pools must clear after sweep; "
            "unexplained heap growth or OOM/restart = PRODUCT_FAIL",
        },
        "S8_50K": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "MEMORY",
            "route_count": 50000,
            "share_window_size": 200,
            "scale": {"channels": 200, "aliases": 50, "groups": 5},
            "steady_duration_s": 2160,
            "candidates_per_pool": 8,
            "description": "S8 scale 50K: 200 channel × 50 alias × 5 group = 50,000 route; "
            "same metrics as S8_3K; measure heap cost of shareEntry.targets snapshot map",
        },
        "S9_A10_W0": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "A",
            "route_count": 3,
            "share_window_size": 0,
            "affinity_ratio": 0.10,
            "injection": {"A": "ok", "B": "ok", "C": "ok"},
            "min_samples": 1200,
            "description": "S9 affinity 10% window=0: three routes q=1 static_weight=100; "
            "route A pinned by affinity (prompt_cache_key); 10% requests use same key; "
            "measure A's global share; baseline (no correction)",
        },
        "S9_A10_W200": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "A",
            "route_count": 3,
            "share_window_size": 200,
            "affinity_ratio": 0.10,
            "injection": {"A": "ok", "B": "ok", "C": "ok"},
            "min_samples": 1200,
            "description": "S9 affinity 10% window=200: correction active; "
            "expect A share lower than W0 baseline at same ratio",
        },
        "S9_A30_W0": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "A",
            "route_count": 3,
            "share_window_size": 0,
            "affinity_ratio": 0.30,
            "injection": {"A": "ok", "B": "ok", "C": "ok"},
            "min_samples": 1200,
            "description": "S9 affinity 30% window=0: 30% requests pinned to A; baseline",
        },
        "S9_A30_W200": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "A",
            "route_count": 3,
            "share_window_size": 200,
            "affinity_ratio": 0.30,
            "injection": {"A": "ok", "B": "ok", "C": "ok"},
            "min_samples": 1200,
            "description": "S9 affinity 30% window=200: correction should pull A down from baseline",
        },
        "S9_A50_W0": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "A",
            "route_count": 3,
            "share_window_size": 0,
            "affinity_ratio": 0.50,
            "injection": {"A": "ok", "B": "ok", "C": "ok"},
            "min_samples": 1200,
            "description": "S9 affinity 50% window=0: 50% requests pinned to A; baseline",
        },
        "S9_A50_W200": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "A",
            "route_count": 3,
            "share_window_size": 200,
            "affinity_ratio": 0.50,
            "injection": {"A": "ok", "B": "ok", "C": "ok"},
            "min_samples": 1200,
            "description": "S9 affinity 50% window=200: must be significantly lower than W0 at same ratio",
        },
        "S9_A70_W0": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "A",
            "route_count": 3,
            "share_window_size": 0,
            "affinity_ratio": 0.70,
            "injection": {"A": "ok", "B": "ok", "C": "ok"},
            "min_samples": 1200,
            "description": "S9 affinity 70% window=0: 70% requests pinned to A; baseline",
        },
        "S9_A70_W200": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "A",
            "route_count": 3,
            "share_window_size": 200,
            "affinity_ratio": 0.70,
            "injection": {"A": "ok", "B": "ok", "C": "ok"},
            "min_samples": 1200,
            "description": "S9 affinity 70% window=200: correction cannot fully offset pinned traffic",
        },
        "S10_RACE": {
            "target": 0.50,
            "tol_pp": 0.03,
            "ci_bounds": None,
            "subject": "B",
            "route_count": 2,
            "replica": 1,
            "min_samples": 6000,
            "injection": {"A": "ok", "B": "ok"},
            "description": "S10 race: single replica, 2 route / 2 key, 64 concurrent, 6,000 stat "
            "requests; validates lock contention under real concurrency. The -race verdict comes "
            "from `go test -race`, not from this runner; the runner checks share sanity and that "
            "sampleCount agrees with attributable attempts.",
        },
        "S10_GLOBAL": {
            "target": 0.50,
            "tol_pp": 0.03,
            "ci_bounds": (0.47, 0.53),
            "subject": "B",
            "route_count": 2,
            "replica": 2,
            "min_samples": 12000,
            "injection": {"A": "ok", "B": "ok"},
            "description": "S10 global share: 2 replicas, 12,000 GLOBAL requests (not 12,000 per "
            "pod). Share windows are process-local, so the verdict must come from re-aggregating "
            "raw attempt quadruples into one global share. Averaging per-pod percentages is "
            "explicitly forbidden and is what this scenario exists to catch.",
        },
        "S10_RESTART": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "KILLSWITCH",
            "route_count": 2,
            "replica": 1,
            "min_samples": 1000,
            "injection": {"A": "ok", "B": "ok"},
            "kill_switch_option": "RouteStatsEnabled",
            "neutral_correction": 1.0,
            "description": "S10 restart kill switch: >=1,000 requests before and after writing "
            "RouteStatsEnabled=false and restarting. After restart every correction must be exactly "
            "1.0 (neutral) and the share window must stop allocating. Sweep evidence is collected "
            "when available; a run shorter than the 1h sweep ticker that shows orphan pools with no "
            "sweep log is a KNOWN LIMITATION, not a FAIL.",
        },
        "S11_FULL": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "PATHS",
            "route_count": 4,
            "injection": {"A": "ok", "B": "ok", "C": "ok", "D": "ok"},
            "user_paths": ["weighted", "affinity", "specific"],
            "min_per_path": 100,
            "min_probe": 20,
            "description": "S11 auditability: >=100 requests per labelled user path "
            "(weighted / affinity / specific) plus >=20 administrative probes. Contract: every "
            "attempt carries its path label and a matching quadruple; probes produce EWMA signal "
            "but contribute opportunity_count=0 and must NOT appear in the share window; real "
            "bypass traffic (affinity / specific) MUST enter the window. Any quadruple mismatch, a "
            "probe inside the window, or bypass missing from it is PRODUCT_FAIL. Note: locked "
            "replay shares SelectedRouteFromChannel with the specific-channel path and has no "
            "distinct label, so it is covered by 'specific' rather than counted separately. "
            "SETUP (verified on a real gateway): the shipped affinity rule only matches "
            "path_regex /v1/responses, so on /v1/chat/completions affinity never engages and every "
            "request is labelled 'weighted'. A rule whose path_regex matches the relay path under "
            "test must be installed first, otherwise the affinity path silently reports 0 samples. "
            "The first request of an affinity chain is also legitimately 'weighted' because the "
            "affinity entry is only recorded after a success.",
        },
        "S12_RETRY": {
            "target": None,
            "tol_pp": None,
            "ci_bounds": None,
            "subject": "B",
            "route_count": 2,
            "injection": {"A": "ok", "B": "first_fail_then_ok"},
            "min_samples": 600,
            "retry_times": 1,
            "description": "S12 retry attribution: B fails its first attempt and succeeds on retry. "
            "The question is whether the absorbed failure still reaches EWMA quality AND takes a "
            "share-window slot. Neither => a persistently failing route can never be de-weighted, "
            "PRODUCT_FAIL. Both => correct. Exactly one => partial product risk, reported as such "
            "and never silently passed. Requires gateway RetryTimes >= 1.",
        },
        "S13_CASCADE": {
            "target": 0.50,
            "tol_pp": 0.03,
            "ci_bounds": None,
            "subject": "B",
            "route_count": 2,
            "injection": {"A": "ok", "B": "ok"},
            "share_window_size": 200,
            "retry_times": 1,
            "upstream_failure_threshold": 1,
            "corr_min": 0.5,
            "corr_max": 2.0,
            "recovery_tolerance_pp": 0.03,
            "recovery_deadline_s": 300,
            "sample_interval_s": 10,
            "description": "S13 degradation and recovery: phases steady -> fault -> recover, B "
            "flipped at runtime by hook. Judgement: share_correction must stay inside "
            "[0.5, 2.0] in EVERY phase, and B's share must return within +/-3pp after recovery. "
            "Out-of-clamp corr or non-recovery is PRODUCT_FAIL. Note: isolation is per-route with "
            "no cross-route cascade, and calm/dormant routes stay selectable at reduced weight, so "
            "this measures weight degradation and recovery, NOT an emptied candidate pool. A 503 "
            "mock is FailureSourceUpstream, so UpstreamFailureThreshold is the governing knob.",
        },
     }