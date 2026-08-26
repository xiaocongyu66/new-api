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
        - "tol_pp": tolerance in percentage points (e.g. 0.02 for ±2pp)
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
    }