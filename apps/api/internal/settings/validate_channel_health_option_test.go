package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The health_store half of this contract lives in
// internal/catalog/health_store/configure_channel_health_test.go; settings
// imports health_store, so one test cannot assert both directions.
//
// Rejecting an out-of-range cooldown value here matters because
// UpdateOption validates before persisting: without this route the option is
// stored verbatim while the live config holds a normalized (clamped) number, so
// the admin UI keeps showing a setting that is not the one in effect.
func TestValidateOptionValueRejectsOutOfRangeChannelHealthCooldown(t *testing.T) {
	for key, value := range map[string]string{
		"ChannelHealthCooldownThreshold":          "0",
		"ChannelHealthCooldownBaseSeconds":        "-1",
		"ChannelHealthCooldownMaxSeconds":         "-1",
		"ChannelHealthCooldownMaxEjectionPercent": "101",
		"ChannelHealthCooldownAlpha":              "1.5",
	} {
		assert.Error(t, ValidateOptionValue(key, value), key)
	}
}
