package channel

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/internal/settings"
)

// This domain owns the gateway-dispatch (route-unit health) setting, so it
// registers its own option hooks. Without this the GatewayDispatch* keys
// never reach the atomic runtime config: a persisted operator value would be
// stored in OptionMap and silently ignored by the health state machine,
// which would keep running on defaults.
//
// The seed hook is chained rather than assigned so it combines with the
// other catalog domains instead of overwriting them (same contract as
// configure_route_stats.go).
func init() {
	settings.OnIsGatewayDispatchOptionKey = IsGatewayDispatchOptionKey
	settings.OnValidateGatewayDispatchOption = ValidateGatewayDispatchOption
	settings.OnApplyGatewayDispatchOption = UpdateGatewayDispatchOption

	previousSeed := settings.OnSeedCatalogOptions
	settings.OnSeedCatalogOptions = func() map[string]string {
		m := map[string]string{}
		if previousSeed != nil {
			m = previousSeed()
		}
		for k, v := range seedGatewayDispatchOptions() {
			m[k] = v
		}
		return m
	}
}

// seedGatewayDispatchOptions publishes the admin-visible keys, so an
// operator sees the values the health state machine is actually using
// rather than an empty field.
func seedGatewayDispatchOptions() map[string]string {
	cfg := GetUnitHealthSetting()
	concurrency := GetConcurrencyLimitSetting()
	return map[string]string{
		"GatewayDispatchCooldownBaseMs":      strconv.FormatInt(cfg.CooldownBaseMs, 10),
		"GatewayDispatchCooldownMaxMs":       strconv.FormatInt(cfg.CooldownMaxMs, 10),
		"GatewayDispatchCooldownRampSteps":   strconv.Itoa(cfg.CooldownRampSteps),
		"GatewayDispatchThrottleBaseMs":      strconv.FormatInt(cfg.ThrottleBaseMs, 10),
		"GatewayDispatchThrottleMaxMs":       strconv.FormatInt(cfg.ThrottleMaxMs, 10),
		"GatewayDispatchScoreDecayTauMs":     strconv.FormatInt(cfg.ScoreDecayTauMs, 10),
		"GatewayDispatchHealthAlpha":         strconv.FormatFloat(cfg.Alpha, 'f', -1, 64),
		"GatewayDispatchHealthMinRequests":   strconv.Itoa(cfg.MinRequests),
		"GatewayDispatchHealthMinScoreFloor": strconv.FormatFloat(cfg.MinScoreFloor, 'f', -1, 64),
		"GatewayRetryTotalBudgetMs":          strconv.FormatInt(cfg.RetryTotalBudgetMs, 10),
		"GatewayForwardMaxConcurrency":       strconv.Itoa(concurrency.GlobalMaxConcurrency),
		"GatewayChannelMaxConcurrency":       strconv.Itoa(concurrency.ChannelMaxConcurrency),
		"EmergencyThreshold":                 strconv.Itoa(cfg.EmergencyThreshold),
		"WarningThreshold":                   strconv.Itoa(cfg.WarningThreshold),
	}
}

// GatewayDispatchOptionKeys are the option keys exposed to the admin panel.
// They map directly to the fields in UnitHealthSetting. The two pool
// pressure keys keep their historical names (EmergencyThreshold /
// WarningThreshold) so persisted operator values survive the rework.
var GatewayDispatchOptionKeys = map[string]struct{}{
	"GatewayDispatchCooldownBaseMs":      {},
	"GatewayDispatchCooldownMaxMs":       {},
	"GatewayDispatchCooldownRampSteps":   {},
	"GatewayDispatchThrottleBaseMs":      {},
	"GatewayDispatchThrottleMaxMs":       {},
	"GatewayDispatchScoreDecayTauMs":     {},
	"GatewayDispatchHealthAlpha":         {},
	"GatewayDispatchHealthMinRequests":   {},
	"GatewayDispatchHealthMinScoreFloor": {},
	"GatewayRetryTotalBudgetMs":          {},
	"GatewayForwardMaxConcurrency":       {},
	"GatewayChannelMaxConcurrency":       {},
	"EmergencyThreshold":                 {},
	"WarningThreshold":                   {},
}

// IsGatewayDispatchOptionKey reports whether key is a gateway dispatch
// option key.
func IsGatewayDispatchOptionKey(key string) bool {
	_, ok := GatewayDispatchOptionKeys[key]
	return ok
}

// ValidateGatewayDispatchOption validates a single gateway dispatch option
// value.
func ValidateGatewayDispatchOption(key, value string) error {
	if !IsGatewayDispatchOptionKey(key) {
		return fmt.Errorf("unknown gateway dispatch option %q", key)
	}

	switch key {
	case "GatewayDispatchCooldownBaseMs",
		"GatewayDispatchCooldownMaxMs",
		"GatewayDispatchThrottleBaseMs",
		"GatewayDispatchThrottleMaxMs",
		"GatewayDispatchScoreDecayTauMs",
		"GatewayDispatchHealthMinRequests":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s must be an integer", key)
		}
		if v < 0 {
			return fmt.Errorf("%s must be a non-negative integer", key)
		}
		return nil

	case "GatewayRetryTotalBudgetMs":
		// The value is clamped into [1000, 300000] at apply time, so any
		// integer is accepted here — rejecting would fight the clamp.
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("%s must be an integer", key)
		}
		return nil

	case "GatewayDispatchCooldownRampSteps":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s must be an integer", key)
		}
		if v < 1 {
			return fmt.Errorf("%s must be a positive integer (>=1)", key)
		}
		return nil

	case "GatewayDispatchHealthAlpha":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("%s must be a float", key)
		}
		if v <= 0 || v > 1 {
			return fmt.Errorf("%s must be in (0, 1]", key)
		}
		return nil

	case "GatewayDispatchHealthMinScoreFloor":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("%s must be a float", key)
		}
		if v < 0 || v > 1 {
			return fmt.Errorf("%s must be in [0, 1]", key)
		}
		return nil

	case "EmergencyThreshold", "WarningThreshold":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s must be an integer", key)
		}
		if v < 0 || v > 100 {
			return fmt.Errorf("%s must be in [0, 100]", key)
		}
		return nil

	case "GatewayForwardMaxConcurrency", "GatewayChannelMaxConcurrency":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s must be an integer", key)
		}
		// Non-negative: 0 is the unlimited convention, so a negative
		// value is a caller bug, not a valid limit.
		if v < 0 {
			return fmt.Errorf("%s must be a non-negative integer", key)
		}
		return nil
	}

	return nil
}

// GetGatewayRetryTotalBudgetMs reads the request-level failover time budget
// of the relay retry loop from the live atomic config (ms). 0 disables the
// time budget (attempt cap only).
func GetGatewayRetryTotalBudgetMs() int64 {
	return GetUnitHealthSetting().RetryTotalBudgetMs
}

// UpdateGatewayDispatchOption validates and applies a single gateway
// dispatch option to the live atomic config. A value change replaces the
// parameters only — the cooldown/streak/EWMA state of running units is
// untouched (mono set_config semantics).
func UpdateGatewayDispatchOption(key, value string) error {
	if err := ValidateGatewayDispatchOption(key, value); err != nil {
		return err
	}

	next := *GetUnitHealthSetting()

	switch key {
	case "GatewayDispatchCooldownBaseMs":
		v, _ := strconv.Atoi(value)
		next.CooldownBaseMs = int64(v)
	case "GatewayDispatchCooldownMaxMs":
		v, _ := strconv.Atoi(value)
		next.CooldownMaxMs = int64(v)
	case "GatewayDispatchCooldownRampSteps":
		v, _ := strconv.Atoi(value)
		next.CooldownRampSteps = v
	case "GatewayDispatchThrottleBaseMs":
		v, _ := strconv.Atoi(value)
		next.ThrottleBaseMs = int64(v)
	case "GatewayDispatchThrottleMaxMs":
		v, _ := strconv.Atoi(value)
		next.ThrottleMaxMs = int64(v)
	case "GatewayDispatchScoreDecayTauMs":
		v, _ := strconv.Atoi(value)
		next.ScoreDecayTauMs = int64(v)
	case "GatewayDispatchHealthAlpha":
		v, _ := strconv.ParseFloat(value, 64)
		next.Alpha = v
	case "GatewayDispatchHealthMinRequests":
		v, _ := strconv.Atoi(value)
		next.MinRequests = v
	case "GatewayDispatchHealthMinScoreFloor":
		v, _ := strconv.ParseFloat(value, 64)
		next.MinScoreFloor = v
	case "EmergencyThreshold":
		v, _ := strconv.Atoi(value)
		next.EmergencyThreshold = v
	case "WarningThreshold":
		v, _ := strconv.Atoi(value)
		next.WarningThreshold = v
	case "GatewayRetryTotalBudgetMs":
		// Clamp into [1000, 300000]: out-of-range operator values are
		// pinned to the nearest bound instead of rejected, so a stale
		// persisted value can never disable the budget or stall a relay.
		v, _ := strconv.Atoi(value)
		const minRetryBudgetMs, maxRetryBudgetMs = 1_000, 300_000
		switch {
		case v < minRetryBudgetMs:
			next.RetryTotalBudgetMs = minRetryBudgetMs
		case v > maxRetryBudgetMs:
			next.RetryTotalBudgetMs = maxRetryBudgetMs
		default:
			next.RetryTotalBudgetMs = int64(v)
		}

	case "GatewayForwardMaxConcurrency", "GatewayChannelMaxConcurrency":
		// The concurrency limits live in their own atomic view, not on
		// UnitHealthSetting: they size slot pools, not the health state
		// machine. The global pool tracks the option live; the
		// per-upstream-key pools pick the limit up at the next channel
		// snapshot rebuild, and an installed pool is never replaced.
		v, _ := strconv.Atoi(value)
		next := *GetConcurrencyLimitSetting()
		if key == "GatewayForwardMaxConcurrency" {
			next.GlobalMaxConcurrency = v
		} else {
			next.ChannelMaxConcurrency = v
		}
		RestoreConcurrencyLimitSetting(&next)
		ApplyGlobalConcurrencyLimit(next.GlobalMaxConcurrency)
		return nil
	}

	RestoreUnitHealthSetting(&next)

	return nil
}
