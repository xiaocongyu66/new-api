package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/catalog/health_store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cooldownTestSetting() *health_store.ChannelHealthSetting {
	return health_store.DefaultChannelHealthSetting()
}

func configureCooldownTest(t *testing.T, cfg *health_store.ChannelHealthSetting, now *time.Time) {
	t.Helper()
	previous := *health_store.GetChannelHealthSetting()
	previousNow := ChannelHealthNow
	if now != nil {
		ChannelHealthNow = func() time.Time { return *now }
	}
	t.Cleanup(func() {
		ChannelHealthNow = previousNow
		health_store.SetChannelHealthSetting(&previous)
	})
	health_store.SetChannelHealthSetting(cfg)
}

func TestChannelHealthCooldownConfigNormalizesAndPreservesInput(t *testing.T) {
	cfg := cooldownTestSetting()
	cfg.CooldownThreshold = 0
	cfg.CooldownBaseSeconds = -1
	cfg.CooldownMaxSeconds = -2
	cfg.CooldownMaxEjectionPercent = 101
	cfg.CooldownAlpha = -1
	configureCooldownTest(t, cfg, nil)

	got := health_store.GetChannelHealthSetting()
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
	configureCooldownTest(t, cooldownTestSetting(), nil)

	require.NoError(t, health_store.UpdateChannelHealthSettingValue(
		"ChannelHealthCooldownThreshold", "7",
	))
	assert.Equal(t, 7, health_store.GetChannelHealthSetting().CooldownThreshold)

	before := *health_store.GetChannelHealthSetting()
	assert.Error(t, health_store.UpdateChannelHealthSettingValue(
		"ChannelHealthCooldownAlpha", "not-a-number",
	))
	assert.Error(t, health_store.UpdateChannelHealthSettingValue("unknown", "1"))
	assert.Equal(t, before, *health_store.GetChannelHealthSetting())

	// Out-of-range values are rejected rather than silently clamped: a clamped
	// value would leave the persisted option and the effective config disagreeing.
	for key, value := range map[string]string{
		"ChannelHealthCooldownThreshold":          "0",
		"ChannelHealthCooldownBaseSeconds":        "-1",
		"ChannelHealthCooldownMaxSeconds":         "-1",
		"ChannelHealthCooldownMaxEjectionPercent": "101",
		"ChannelHealthCooldownAlpha":              "1.5",
	} {
		assert.Error(t, health_store.UpdateChannelHealthSettingValue(key, value), key)
		assert.Error(t, validateOptionValue(key, value), key)
	}
	assert.Equal(t, before, *health_store.GetChannelHealthSetting(),
		"a rejected value must not change the live config")

	// An inverted base/max pair is rejected too: normalization would raise max up
	// to base, so accepting it would store a duration that never takes effect.
	assert.Error(t, health_store.UpdateChannelHealthSettingValue(
		"ChannelHealthCooldownMaxSeconds", "5",
	), "max below the configured base must be rejected")
	require.NoError(t, health_store.UpdateChannelHealthSettingValue(
		"ChannelHealthCooldownMaxSeconds", "90",
	))
	assert.Equal(t, 90, health_store.GetChannelHealthSetting().CooldownMaxSeconds)
}
