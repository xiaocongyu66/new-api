package routestats

import (
	"sync/atomic"
	"time"
)

// RouteStatsSetting configures the EWMA-based route unit quality scoring.
//
// When Enabled is false, the system falls back to baseline behavior (quality=1.0
// for all route units). This serves as a kill switch: toggling it off instantly
// restores baseline, with no restart required.
type RouteStatsSetting struct {
	Enabled bool `json:"enabled"`

	// EWMA parameters
	AlphaMin      float64 `json:"alpha_min"`       // Minimum EWMA alpha, default 0.05
	AlphaSuccess  float64 `json:"alpha_success"`   // EWMA alpha for success rate, default 0.10
	Tau           float64 `json:"tau"`             // Time constant for alpha_eff (seconds), default 60
	TauStale      float64 `json:"tau_stale"`       // Staleness regression time constant (seconds), default 600

	// Target values for normalization
	TTFTTargetMs     int `json:"ttft_target_ms"`     // TTFT target (ms), default 2000
	TPSTarget        int `json:"tps_target"`         // TPS target (tok/s), default 20
	LatencyTargetMs  int `json:"latency_target_ms"`  // Latency target (ms), default 30000

	// Quality synthesis weights (must sum to 1.0 for intuitive behavior)
	WeightSuccess float64 `json:"weight_success"` // Weight for success rate, default 0.60
	WeightTTFT    float64 `json:"weight_ttft"`    // Weight for TTFT, default 0.25
	WeightTPS     float64 `json:"weight_tps"`     // Weight for TPS, default 0.15

	// Quality bounds
	ComponentFloor float64 `json:"component_floor"` // Per-component floor, default 0.2
	ComponentCeil  float64 `json:"component_ceil"`  // Per-component ceiling, default 1.5
	QualityFloor   float64 `json:"quality_floor"`   // Synthesized quality floor, default 0.5
	QualityCeil    float64 `json:"quality_ceil"`    // Synthesized quality ceiling, default 1.5

	// Observation caps
	TTFTCapMs     int `json:"ttft_cap_ms"`     // TTFT observation cap (ms), default 120000
	LatencyCapMs  int `json:"latency_cap_ms"`  // Latency observation cap (ms), default 120000

	// 429 penalty
	Penalty429DefaultMs int `json:"penalty_429_default_ms"` // Default 429 penalty (ms), default 5000
	Penalty429MinMs     int `json:"penalty_429_min_ms"`     // Min 429 penalty with Retry-After (ms), default 5000
	Penalty429MaxMs     int `json:"penalty_429_max_ms"`     // Max 429 penalty with Retry-After (ms), default 60000

	// Minimum samples before quality synthesis trusts the data
	MinSamples int `json:"min_samples"` // Default 5

	// TTL for stale entries
	TTLSeconds int `json:"ttl_seconds"` // Default 86400 (24h)
}

// DefaultRouteStatsSetting returns the recommended defaults.
func DefaultRouteStatsSetting() *RouteStatsSetting {
	return &RouteStatsSetting{
		Enabled:             true,
		AlphaMin:            0.05,
		AlphaSuccess:        0.10,
		Tau:                 60.0,
		TauStale:            600.0,
		TTFTTargetMs:        2000,
		TPSTarget:           20,
		LatencyTargetMs:     30000,
		WeightSuccess:       0.60,
		WeightTTFT:          0.25,
		WeightTPS:           0.15,
		ComponentFloor:      0.2,
		ComponentCeil:       1.5,
		QualityFloor:        0.5,
		QualityCeil:         1.5,
		TTFTCapMs:           120000,
		LatencyCapMs:        120000,
		Penalty429DefaultMs: 5000,
		Penalty429MinMs:     5000,
		Penalty429MaxMs:     60000,
		MinSamples:          5,
		TTLSeconds:          86400,
	}
}

// routeStatsSetting holds the runtime config. It is read on every request from
// handler goroutines while an admin may replace it at any time, so it is stored in
// an atomic.Pointer: the struct is swapped wholesale rather than mutated in place.
var routeStatsSetting atomic.Pointer[RouteStatsSetting]

func init() {
	routeStatsSetting.Store(DefaultRouteStatsSetting())
}

// GetRouteStatsSetting returns the current route stats setting.
func GetRouteStatsSetting() *RouteStatsSetting {
	return routeStatsSetting.Load()
}

// SetRouteStatsSetting updates the route stats setting. The caller's struct is
// copied before publication so the caller cannot mutate the live config after
// this call returns.
func SetRouteStatsSetting(cfg *RouteStatsSetting) {
	if cfg == nil {
		cfg = DefaultRouteStatsSetting()
	}
	// Copy to avoid external mutation
	copy := *cfg
	normalizeRouteStatsSetting(&copy)
	routeStatsSetting.Store(&copy)
}

// normalizeRouteStatsSetting clamps all fields into their valid ranges in place.
func normalizeRouteStatsSetting(cfg *RouteStatsSetting) {
	if cfg.AlphaMin < 0 {
		cfg.AlphaMin = 0
	} else if cfg.AlphaMin > 1 {
		cfg.AlphaMin = 1
	}
	if cfg.AlphaSuccess < 0 {
		cfg.AlphaSuccess = 0
	} else if cfg.AlphaSuccess > 1 {
		cfg.AlphaSuccess = 1
	}
	if cfg.Tau <= 0 {
		cfg.Tau = 60
	}
	if cfg.TauStale <= 0 {
		cfg.TauStale = 600
	}
	if cfg.TTFTTargetMs <= 0 {
		cfg.TTFTTargetMs = 2000
	}
	if cfg.TPSTarget <= 0 {
		cfg.TPSTarget = 20
	}
	if cfg.LatencyTargetMs <= 0 {
		cfg.LatencyTargetMs = 30000
	}
	// Normalize weights to sum to 1.0
	sum := cfg.WeightSuccess + cfg.WeightTTFT + cfg.WeightTPS
	if sum <= 0 {
		cfg.WeightSuccess = 0.60
		cfg.WeightTTFT = 0.25
		cfg.WeightTPS = 0.15
	} else {
		cfg.WeightSuccess /= sum
		cfg.WeightTTFT /= sum
		cfg.WeightTPS /= sum
	}
	if cfg.ComponentFloor < 0 {
		cfg.ComponentFloor = 0
	} else if cfg.ComponentFloor > 1 {
		cfg.ComponentFloor = 1
	}
	if cfg.ComponentCeil < cfg.ComponentFloor {
		cfg.ComponentCeil = cfg.ComponentFloor
	}
	if cfg.QualityFloor < 0 {
		cfg.QualityFloor = 0
	} else if cfg.QualityFloor > 1 {
		cfg.QualityFloor = 1
	}
	if cfg.QualityCeil < cfg.QualityFloor {
		cfg.QualityCeil = cfg.QualityFloor
	}
	if cfg.TTFTCapMs <= 0 {
		cfg.TTFTCapMs = 120000
	}
	if cfg.LatencyCapMs <= 0 {
		cfg.LatencyCapMs = 120000
	}
	if cfg.Penalty429DefaultMs < 0 {
		cfg.Penalty429DefaultMs = 5000
	}
	if cfg.Penalty429MinMs < 0 {
		cfg.Penalty429MinMs = 5000
	}
	if cfg.Penalty429MaxMs < cfg.Penalty429MinMs {
		cfg.Penalty429MaxMs = cfg.Penalty429MinMs
	}
	if cfg.MinSamples < 0 {
		cfg.MinSamples = 5
	}
	if cfg.TTLSeconds <= 0 {
		cfg.TTLSeconds = 86400
	}
}

// RouteStatsConfigAccessor defines the interface for accessing route stats config.
// This allows the model package to inject its own config accessor without
// routestats importing model.
type RouteStatsConfigAccessor interface {
	GetRouteStatsSetting() *RouteStatsSetting
}

// configAccessor is the package-level accessor, set by the model package on init.
var configAccessor atomic.Pointer[RouteStatsConfigAccessor]

// SetConfigAccessor sets the config accessor. Should be called once during
// application initialization by the model package.
func SetConfigAccessor(accessor RouteStatsConfigAccessor) {
	configAccessor.Store(&accessor)
}

// getConfig returns the current config, using the accessor if set.
func getConfig() *RouteStatsSetting {
	if accessor := configAccessor.Load(); accessor != nil && *accessor != nil {
		return (*accessor).GetRouteStatsSetting()
	}
	return GetRouteStatsSetting()
}

// Time constants for monotonic clock
var (
	processStart = time.Now()
	// nowFunc allows tests to inject a custom clock
	nowFunc = func() time.Duration {
		return time.Since(processStart)
	}
)

// SetNowFunc replaces the time source for testing. Caller must restore via
// t.Cleanup.
func SetNowFunc(f func() time.Duration) func() {
	old := nowFunc
	nowFunc = f
	return func() { nowFunc = old }
}

// NowMonotonic returns nanoseconds since process start.
func NowMonotonic() int64 {
	return nowFunc().Nanoseconds()
}

// SecondsSinceProcessStart returns seconds since process start as float64.
func SecondsSinceProcessStart() float64 {
	return nowFunc().Seconds()
}