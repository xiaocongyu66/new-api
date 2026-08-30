package health_store

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cooldownTestSetting() *ChannelHealthSetting {
	return DefaultChannelHealthSetting()
}

func configureCooldownTest(t *testing.T, cfg *ChannelHealthSetting) {
	t.Helper()
	previous := *GetChannelHealthSetting()
	t.Cleanup(func() { SetChannelHealthSetting(&previous) })
	SetChannelHealthSetting(cfg)
}

func TestChannelHealthCooldownConfigNormalizesAndPreservesInput(t *testing.T) {
	cfg := cooldownTestSetting()
	cfg.CooldownThreshold = 0
	cfg.CooldownBaseSeconds = -1
	cfg.CooldownMaxSeconds = -2
	cfg.CooldownMaxEjectionPercent = 101
	cfg.CooldownAlpha = -1
	configureCooldownTest(t, cfg)

	got := GetChannelHealthSetting()
	require.NotNil(t, got)
	assert.Equal(t, 1, got.CooldownThreshold)
	assert.Equal(t, 0, got.CooldownBaseSeconds)
	assert.Equal(t, 0, got.CooldownMaxSeconds)
	assert.Equal(t, 100, got.CooldownMaxEjectionPercent)
	assert.Equal(t, 0.0, got.CooldownAlpha)

	assert.Equal(t, 0, cfg.CooldownThreshold, "publication must not mutate the caller's struct")
	assert.Equal(t, -1, cfg.CooldownBaseSeconds)
	assert.Equal(t, -2, cfg.CooldownMaxSeconds)
	assert.Equal(t, 101, cfg.CooldownMaxEjectionPercent)
	assert.Equal(t, -1.0, cfg.CooldownAlpha)
}

func TestChannelHealthCooldownOptionUpdateRejectsInvalidValue(t *testing.T) {
	configureCooldownTest(t, cooldownTestSetting())

	require.NoError(t, UpdateChannelHealthSettingValue(
		"ChannelHealthCooldownThreshold", "7",
	))
	assert.Equal(t, 7, GetChannelHealthSetting().CooldownThreshold)

	before := *GetChannelHealthSetting()
	assert.Error(t, UpdateChannelHealthSettingValue(
		"ChannelHealthCooldownAlpha", "not-a-number",
	))
	assert.Error(t, UpdateChannelHealthSettingValue("unknown", "1"))
	assert.Equal(t, before, *GetChannelHealthSetting())

	// Out-of-range values are rejected rather than silently clamped: a clamped
	// value would leave the persisted option and the effective config disagreeing.
	for key, value := range map[string]string{
		"ChannelHealthCooldownThreshold":          "0",
		"ChannelHealthCooldownBaseSeconds":        "-1",
		"ChannelHealthCooldownMaxSeconds":         "-1",
		"ChannelHealthCooldownMaxEjectionPercent": "101",
		"ChannelHealthCooldownAlpha":              "1.5",
	} {
		assert.Error(t, UpdateChannelHealthSettingValue(key, value), key)
	}
	assert.Equal(t, before, *GetChannelHealthSetting(),
		"a rejected value must not change the live config")

	// An inverted base/max pair is rejected too: normalization would raise max up
	// to base, so accepting it would store a duration that never takes effect.
	assert.Error(t, UpdateChannelHealthSettingValue(
		"ChannelHealthCooldownMaxSeconds", "5",
	), "max below the configured base must be rejected")
	require.NoError(t, UpdateChannelHealthSettingValue(
		"ChannelHealthCooldownMaxSeconds", "90",
	))
	assert.Equal(t, 90, GetChannelHealthSetting().CooldownMaxSeconds)
}
