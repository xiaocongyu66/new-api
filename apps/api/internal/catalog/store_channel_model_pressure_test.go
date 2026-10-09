package channel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetPressure and setPressure live in health_unit_fixture_test.go (the shared
// fixture file); the pressure cases here just call them.

func TestModelPressureLevel_ThreeTierBoundaries(t *testing.T) {
	resetPressure()
	// Default thresholds: EmergencyThreshold=20, WarningThreshold=50.
	// total=10 → healthy counts at the boundaries:
	//   1/10 = 10% → Emergency; 2/10 = 20% → Warning; 5/10 = 50% → Normal.
	cases := []struct {
		name    string
		healthy int
		want    PressureLevel
	}{
		{"1/10 = 10% → emergency", 1, PressureEmergency},
		{"2/10 = 20% → warning (not < 20)", 2, PressureWarning},
		{"5/10 = 50% → normal (not < 50)", 5, PressureNormal},
		{"4/10 = 40% → warning", 4, PressureWarning},
		{"10/10 = 100% → normal", 10, PressureNormal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setPressure("test-model", 10, tc.healthy)
			assert.Equal(t, tc.want, modelPressureLevel("test-model"))
		})
	}
}

func TestModelPressureLevel_TotalZeroFallsToNormal(t *testing.T) {
	resetPressure()
	setPressure("zero-model", 0, 0)
	assert.Equal(t, PressureNormal, modelPressureLevel("zero-model"))

	resetPressure()
	// Unknown model (no entry) also normal.
	assert.Equal(t, PressureNormal, modelPressureLevel("nonexistent"))
}

// TestPressureOnUnitTransition_Boundary pins the healthy-counter movement: only
// the healthy ↔ unhealthy boundary crossings move it. Same-side transitions
// (rung climbs inside an active cooldown, idle anneal) leave it untouched.
func TestPressureOnUnitTransition_Boundary(t *testing.T) {
	resetPressure()
	setPressure("m", 10, 10)
	key := RouteKey{ChannelId: 1, KeyIndex: 0, Model: "m"}

	// healthy -> unhealthy: healthy decrements.
	pressureOnUnitTransition(key, false, true)
	assert.Equal(t, 9, pressureIDM["m"].healthy)

	// unhealthy -> unhealthy (ladder climb / still cooling): no change.
	pressureOnUnitTransition(key, true, true)
	assert.Equal(t, 9, pressureIDM["m"].healthy)

	// unhealthy -> healthy: healthy increments.
	pressureOnUnitTransition(key, true, false)
	assert.Equal(t, 10, pressureIDM["m"].healthy)

	// healthy -> healthy: no change.
	pressureOnUnitTransition(key, false, false)
	assert.Equal(t, 10, pressureIDM["m"].healthy)
}

// TestPressureOnUnitTransition_FloorAtZero pins the floor: a healthy counter
// that is already zero must not go negative.
func TestPressureOnUnitTransition_FloorAtZero(t *testing.T) {
	resetPressure()
	setPressure("floor", 5, 0)
	key := RouteKey{ChannelId: 1, KeyIndex: 0, Model: "floor"}

	// healthy(0) -> unhealthy must not underflow.
	pressureOnUnitTransition(key, false, true)
	pressureLock.RLock()
	p := pressureIDM["floor"]
	pressureLock.RUnlock()
	assert.Equal(t, 0, p.healthy)
}

func TestPressureOnRemove_DecrementsTotalAndHealthy(t *testing.T) {
	resetPressure()
	setPressure("rm", 5, 5)
	key := RouteKey{ChannelId: 7, KeyIndex: 0, Model: "rm"}

	pressureOnRemove(key)
	pressureLock.RLock()
	p := pressureIDM["rm"]
	pressureLock.RUnlock()
	assert.Equal(t, 4, p.total)
	assert.Equal(t, 4, p.healthy)
}

func TestPressureOnRemove_FlooredAtZero(t *testing.T) {
	resetPressure()
	setPressure("rm0", 1, 1)
	key := RouteKey{ChannelId: 9, KeyIndex: 0, Model: "rm0"}

	pressureOnRemove(key)
	pressureLock.RLock()
	p := pressureIDM["rm0"]
	pressureLock.RUnlock()
	assert.Equal(t, 0, p.total)
	assert.Equal(t, 0, p.healthy)
}

// TestDefaultUnitHealthSetting_PressureDefaults pins the two pool-pressure
// thresholds on the unit config (the only pressure knobs that survive the
// rework).
func TestDefaultUnitHealthSetting_PressureDefaults(t *testing.T) {
	s := DefaultUnitHealthSetting()
	require.NotNil(t, s)
	assert.Equal(t, 20, s.EmergencyThreshold)
	assert.Equal(t, 50, s.WarningThreshold)
}

// TestValidateGatewayDispatchOption_Thresholds covers the operator-facing
// validation of the two pressure keys: 0–100 inclusive, integers only.
func TestValidateGatewayDispatchOption_Thresholds(t *testing.T) {
	for _, key := range []string{"EmergencyThreshold", "WarningThreshold"} {
		assert.NoError(t, ValidateGatewayDispatchOption(key, "0"))
		assert.NoError(t, ValidateGatewayDispatchOption(key, "50"))
		assert.NoError(t, ValidateGatewayDispatchOption(key, "100"))
		assert.Error(t, ValidateGatewayDispatchOption(key, "101"))
		assert.Error(t, ValidateGatewayDispatchOption(key, "-1"))
		assert.Error(t, ValidateGatewayDispatchOption(key, "abc"))
	}
}

// TestUpdateGatewayDispatchOption_Thresholds drives the hot-swap apply path: a
// threshold change replaces the live config without touching unrelated knobs.
func TestUpdateGatewayDispatchOption_Thresholds(t *testing.T) {
	orig := *GetUnitHealthSetting()
	t.Cleanup(func() { RestoreUnitHealthSetting(&orig) })

	require.NoError(t, UpdateGatewayDispatchOption("EmergencyThreshold", "15"))
	require.NoError(t, UpdateGatewayDispatchOption("WarningThreshold", "45"))
	updated := GetUnitHealthSetting()
	assert.Equal(t, 15, updated.EmergencyThreshold)
	assert.Equal(t, 45, updated.WarningThreshold)
	// An unrelated knob must be untouched.
	assert.Equal(t, orig.CooldownBaseMs, updated.CooldownBaseMs)
}
