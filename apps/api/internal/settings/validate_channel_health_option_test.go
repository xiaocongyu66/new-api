package settings_test

import (
	"testing"

	_ "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/settings"
	"github.com/stretchr/testify/assert"
)

// The channel health implementation now lives in internal/catalog/track_health.go
// (package channel). Settings uses only the existing On* hook vars registered
// in its init (no direct import of catalog children in main package to avoid cycles).
// The external test package uses blank import to trigger init() and public
// settings.ValidateOptionValue. This resolves the test closure per C1 constraints
// while preserving all assertions. The hook side is covered in catalog tests.

func TestValidateOptionValueRejectsOutOfRangeChannelHealthCooldown(t *testing.T) {
	for key, value := range map[string]string{
		"ChannelHealthCooldownThreshold":          "0",
		"ChannelHealthCooldownBaseSeconds":        "-1",
		"ChannelHealthCooldownMaxSeconds":         "-1",
		"ChannelHealthCooldownMaxEjectionPercent": "101",
		"ChannelHealthCooldownAlpha":              "1.5",
	} {
		assert.Error(t, settings.ValidateOptionValue(key, value), key)
	}
}
