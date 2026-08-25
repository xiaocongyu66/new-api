package routestats

import (
	"math"
)

// QualityComponents holds the raw EWMA values for a route unit.
type QualityComponents struct {
	SuccessRate float64 // EWMA success rate [0, 1]
	TTFTMs      float64 // EWMA TTFT in milliseconds
	TPS         float64 // EWMA tokens per second
	LatencyMs   float64 // EWMA latency in milliseconds

	// Track which components have been observed
	HasSuccess bool
	HasTTFT    bool
	HasTPS     bool
	HasLatency bool

	SampleCount int // Total observations across all components
}

// QualityResult holds the normalized quality components and synthesized score.
type QualityResult struct {
	QSuccess float64 // Normalized success quality [ComponentFloor, ComponentCeil]
	QTTFT    float64 // Normalized TTFT quality [ComponentFloor, ComponentCeil]
	QTPS     float64 // Normalized TPS quality [ComponentFloor, ComponentCeil]
	QLatency float64 // Normalized latency quality [ComponentFloor, ComponentCeil]

	Quality float64 // Synthesized quality [QualityFloor, QualityCeil]

	// Which components contributed to synthesis
	ObservedComponents int
}

// clamp clamps x to [lo, hi].
func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// normalizeSuccessRate normalizes success rate to quality.
// Higher success rate = higher quality.
// q = clamp(success_rate, ComponentFloor, ComponentCeil)
func normalizeSuccessRate(successRate float64, cfg *RouteStatsSetting) float64 {
	return clamp(successRate, cfg.ComponentFloor, cfg.ComponentCeil)
}

// normalizeLowerBetter normalizes a "lower is better" metric (TTFT, latency).
// q = clamp((target / observed)^p, ComponentFloor, ComponentCeil)
// p defaults to 1.0.
func normalizeLowerBetter(observed, target float64, cfg *RouteStatsSetting) float64 {
	if observed <= 0 {
		return cfg.ComponentCeil
	}
	q := target / observed
	return clamp(q, cfg.ComponentFloor, cfg.ComponentCeil)
}

// normalizeHigherBetter normalizes a "higher is better" metric (TPS).
// q = clamp((observed / target)^p, ComponentFloor, ComponentCeil)
// p defaults to 1.0.
func normalizeHigherBetter(observed, target float64, cfg *RouteStatsSetting) float64 {
	if target <= 0 {
		return cfg.ComponentCeil
	}
	q := observed / target
	return clamp(q, cfg.ComponentFloor, cfg.ComponentCeil)
}

// ComputeQuality computes normalized components and synthesized quality from
// raw EWMA values. This is a pure function: it does not modify any state.
// If SampleCount < MinSamples, returns neutral quality (1.0).
func ComputeQuality(comp QualityComponents, cfg *RouteStatsSetting) QualityResult {
	var result QualityResult

	// MinSamples guard: if insufficient samples, return neutral quality
	if comp.SampleCount < cfg.MinSamples {
		result.QSuccess = 1.0
		result.QTTFT = 1.0
		result.QTPS = 1.0
		result.QLatency = 1.0
		result.Quality = 1.0
		result.ObservedComponents = 0
		return result
	}

	// Normalize every observed component. Latency is normalized for observability
	// but deliberately excluded from the synthesis: the agreed weights cover
	// success/ttft/tps only (0.60/0.25/0.15).
	result.QSuccess = 1.0
	result.QTTFT = 1.0
	result.QTPS = 1.0
	result.QLatency = 1.0
	if comp.HasSuccess {
		result.QSuccess = normalizeSuccessRate(comp.SuccessRate, cfg)
	}
	if comp.HasTTFT {
		result.QTTFT = normalizeLowerBetter(comp.TTFTMs, float64(cfg.TTFTTargetMs), cfg)
	}
	if comp.HasTPS {
		result.QTPS = normalizeHigherBetter(comp.TPS, float64(cfg.TPSTarget), cfg)
	}
	if comp.HasLatency {
		result.QLatency = normalizeLowerBetter(comp.LatencyMs, float64(cfg.LatencyTargetMs), cfg)
	}

	// Weights are renormalized over observed components only. Filling a missing
	// component with neutral 1.0 would hand it a free credit: a throttled route
	// with no TPS sample would score 0.695 instead of 0.641.
	var sumWeights, weightedSum float64
	if comp.HasSuccess {
		sumWeights += cfg.WeightSuccess
		weightedSum += cfg.WeightSuccess * result.QSuccess
		result.ObservedComponents++
	}
	if comp.HasTTFT {
		sumWeights += cfg.WeightTTFT
		weightedSum += cfg.WeightTTFT * result.QTTFT
		result.ObservedComponents++
	}
	if comp.HasTPS {
		sumWeights += cfg.WeightTPS
		weightedSum += cfg.WeightTPS * result.QTPS
		result.ObservedComponents++
	}

	if sumWeights > 0 {
		result.Quality = clamp(weightedSum/sumWeights, cfg.QualityFloor, cfg.QualityCeil)
	} else {
		result.Quality = 1.0
	}

	return result
}

// RegressionTowardsNeutral computes the regressed value towards neutral after
// a time delta. This is a pure function.
//
// v_base = v_neutral + (v_stored - v_neutral) * exp(-Δt / τ_stale)
//
// Neutral values:
//   - Success rate: 1.0
//   - TTFT: TTFTTargetMs
//   - TPS: TPSTarget
//   - Latency: LatencyTargetMs
//
// Latency-type metrics (TTFT, Latency) must NEVER regress towards 0
// (would give idle routes q=1.5 max reward). They regress towards their targets.
func RegressionTowardsNeutral(stored, neutral float64, deltaSeconds, tauStale float64) float64 {
	if tauStale <= 0 {
		return stored
	}
	if deltaSeconds <= 0 {
		return stored
	}
	factor := math.Exp(-deltaSeconds / tauStale)
	return neutral + (stored-neutral)*factor
}

// RegressionComponents applies staleness regression to all components.
// This is a pure function.
func RegressionComponents(comp QualityComponents, deltaSeconds float64, cfg *RouteStatsSetting) QualityComponents {
	if deltaSeconds <= 0 {
		return comp
	}

	// Only observed components regress. An unobserved component stores a zero, and
	// regressing that toward the target manufactures data: a route that never
	// streamed would report a TTFT climbing toward target while it sits idle, and
	// the value would differ between two consecutive reads.
	reg := comp
	if comp.HasSuccess {
		reg.SuccessRate = RegressionTowardsNeutral(comp.SuccessRate, 1.0, deltaSeconds, cfg.TauStale)
	}
	if comp.HasTTFT {
		reg.TTFTMs = RegressionTowardsNeutral(comp.TTFTMs, float64(cfg.TTFTTargetMs), deltaSeconds, cfg.TauStale)
	}
	if comp.HasTPS {
		reg.TPS = RegressionTowardsNeutral(comp.TPS, float64(cfg.TPSTarget), deltaSeconds, cfg.TauStale)
	}
	if comp.HasLatency {
		reg.LatencyMs = RegressionTowardsNeutral(comp.LatencyMs, float64(cfg.LatencyTargetMs), deltaSeconds, cfg.TauStale)
	}
	return reg
}
