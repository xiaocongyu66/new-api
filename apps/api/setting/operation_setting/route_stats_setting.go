package operation_setting

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/pkg/routestats"
)

// RouteStatsSettingOptionKeys are the option keys exposed to the admin panel.
// These map directly to the fields in routestats.RouteStatsSetting.
var RouteStatsSettingOptionKeys = map[string]struct{}{
	"RouteStatsEnabled":            {},
	"RouteStatsShareWindowSize":    {},
	"RouteStatsShareCorrMin":       {},
	"RouteStatsShareCorrMax":       {},
	"RouteStatsMinSamples":         {},
	"RouteStatsTTLSeconds":         {},
	"RouteStatsTTFTTargetMs":       {},
	"RouteStatsTPSTarget":          {},
	"RouteStatsQualityFloor":       {},
	"RouteStatsQualityCeil":        {},
	"RouteStatsComponentFloor":     {},
	"RouteStatsComponentCeil":      {},
}

// IsRouteStatsOptionKey reports whether key is a route stats option key.
func IsRouteStatsOptionKey(key string) bool {
	_, ok := RouteStatsSettingOptionKeys[key]
	return ok
}

// ValidateRouteStatsSettingValue validates a single route stats option value.
func ValidateRouteStatsSettingValue(key, value string) error {
	if !IsRouteStatsOptionKey(key) {
		return fmt.Errorf("unknown route stats option %q", key)
	}

	switch key {
	case "RouteStatsEnabled":
		if value != "true" && value != "false" {
			return fmt.Errorf("%s must be \"true\" or \"false\"", key)
		}
		return nil

	case "RouteStatsShareWindowSize":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s must be an integer", key)
		}
		if v < 0 {
			return fmt.Errorf("%s must be a non-negative integer", key)
		}
		if v > 100000 {
			return fmt.Errorf("%s must not exceed 100000", key)
		}
		return nil

	case "RouteStatsShareCorrMin":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("%s must be a float", key)
		}
		if v <= 0 || v > 1 {
			return fmt.Errorf("%s must be in (0, 1]", key)
		}
		return nil

	case "RouteStatsShareCorrMax":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("%s must be a float", key)
		}
		if v < 1 {
			return fmt.Errorf("%s must be >= 1", key)
		}
		return nil

	case "RouteStatsMinSamples", "RouteStatsTTLSeconds", "RouteStatsTTFTTargetMs", "RouteStatsTPSTarget":
		v, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s must be an integer", key)
		}
		if v <= 0 {
			return fmt.Errorf("%s must be a positive integer (>=1)", key)
		}
		return nil

	case "RouteStatsQualityFloor", "RouteStatsComponentFloor":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("%s must be a float", key)
		}
		if v < 0 || v > 1 {
			return fmt.Errorf("%s must be in [0, 1]", key)
		}
		return nil

	case "RouteStatsQualityCeil", "RouteStatsComponentCeil":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("%s must be a float", key)
		}
		if v < 1 {
			return fmt.Errorf("%s must be >= 1", key)
		}
		return nil
	}

	return nil
}

// UpdateRouteStatsSettingValue validates and applies a single route stats option.
func UpdateRouteStatsSettingValue(key, value string) error {
	if err := ValidateRouteStatsSettingValue(key, value); err != nil {
		return err
	}

	current := routestats.GetRouteStatsSetting()
	if current == nil {
		current = routestats.DefaultRouteStatsSetting()
	}
	next := *current

	switch key {
	case "RouteStatsEnabled":
		next.Enabled = value == "true"
	case "RouteStatsShareWindowSize":
		v, _ := strconv.Atoi(value)
		next.ShareWindowSize = v
	case "RouteStatsShareCorrMin":
		v, _ := strconv.ParseFloat(value, 64)
		next.ShareCorrMin = v
	case "RouteStatsShareCorrMax":
		v, _ := strconv.ParseFloat(value, 64)
		next.ShareCorrMax = v
	case "RouteStatsMinSamples":
		v, _ := strconv.Atoi(value)
		next.MinSamples = v
	case "RouteStatsTTLSeconds":
		v, _ := strconv.Atoi(value)
		next.TTLSeconds = v
	case "RouteStatsTTFTTargetMs":
		v, _ := strconv.Atoi(value)
		next.TTFTTargetMs = v
	case "RouteStatsTPSTarget":
		v, _ := strconv.Atoi(value)
		next.TPSTarget = v
	case "RouteStatsQualityFloor":
		v, _ := strconv.ParseFloat(value, 64)
		next.QualityFloor = v
	case "RouteStatsQualityCeil":
		v, _ := strconv.ParseFloat(value, 64)
		next.QualityCeil = v
	case "RouteStatsComponentFloor":
		v, _ := strconv.ParseFloat(value, 64)
		next.ComponentFloor = v
	case "RouteStatsComponentCeil":
		v, _ := strconv.ParseFloat(value, 64)
		next.ComponentCeil = v
	}

	routestats.SetRouteStatsSetting(&next)
	return nil
}