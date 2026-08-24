package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/routestats"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRouteStatsRoundTrip proves every key round-trips through validate+update
// and lands in routestats.GetRouteStatsSetting().
func TestRouteStatsRoundTrip(t *testing.T) {
	saved := routestats.GetRouteStatsSetting()
	t.Cleanup(func() {
		routestats.SetRouteStatsSetting(saved)
	})

	base := routestats.DefaultRouteStatsSetting()
	routestats.SetRouteStatsSetting(base)

	roundTrips := map[string]struct {
		value   string
		extract func(*routestats.RouteStatsSetting) any
		want    any
	}{
		"RouteStatsEnabled":          {"false", func(s *routestats.RouteStatsSetting) any { return s.Enabled }, false},
		"RouteStatsShareWindowSize":   {"300", func(s *routestats.RouteStatsSetting) any { return s.ShareWindowSize }, 300},
		"RouteStatsShareCorrMin":      {"0.7", func(s *routestats.RouteStatsSetting) any { return s.ShareCorrMin }, 0.7},
		"RouteStatsShareCorrMax":      {"2.5", func(s *routestats.RouteStatsSetting) any { return s.ShareCorrMax }, 2.5},
		"RouteStatsMinSamples":        {"12", func(s *routestats.RouteStatsSetting) any { return s.MinSamples }, 12},
		"RouteStatsTTLSeconds":        {"3600", func(s *routestats.RouteStatsSetting) any { return s.TTLSeconds }, 3600},
		"RouteStatsTTFTTargetMs":      {"1500", func(s *routestats.RouteStatsSetting) any { return s.TTFTTargetMs }, 1500},
		"RouteStatsTPSTarget":         {"40", func(s *routestats.RouteStatsSetting) any { return s.TPSTarget }, 40},
		"RouteStatsQualityFloor":      {"0.6", func(s *routestats.RouteStatsSetting) any { return s.QualityFloor }, 0.6},
		"RouteStatsQualityCeil":       {"2.0", func(s *routestats.RouteStatsSetting) any { return s.QualityCeil }, 2.0},
		"RouteStatsComponentFloor":    {"0.3", func(s *routestats.RouteStatsSetting) any { return s.ComponentFloor }, 0.3},
		"RouteStatsComponentCeil":     {"1.8", func(s *routestats.RouteStatsSetting) any { return s.ComponentCeil }, 1.8},
	}

	for key, tc := range roundTrips {
		t.Run(key, func(t *testing.T) {
			require.NoError(t, UpdateRouteStatsSettingValue(key, tc.value))
			got := routestats.GetRouteStatsSetting()
			assert.Equal(t, tc.want, tc.extract(got))
		})
	}
}

// TestRouteStatsShareWindowSizeZeroAccepted confirms 0 is valid and disables correction.
func TestRouteStatsShareWindowSizeZeroAccepted(t *testing.T) {
	saved := routestats.GetRouteStatsSetting()
	t.Cleanup(func() {
		routestats.SetRouteStatsSetting(saved)
	})

	routestats.SetRouteStatsSetting(routestats.DefaultRouteStatsSetting())

	require.NoError(t, UpdateRouteStatsSettingValue("RouteStatsShareWindowSize", "0"))
	assert.Equal(t, 0, routestats.GetRouteStatsSetting().ShareWindowSize)
}

// TestRouteStatsInvalidValues is table-driven, one row per bound.
func TestRouteStatsInvalidValues(t *testing.T) {
	invalids := []struct {
		key    string
		value  string
		why    string
	}{
		// RouteStatsEnabled
		{"RouteStatsEnabled", "yes", "not true/false"},
		{"RouteStatsEnabled", "1", "not true/false"},
		{"RouteStatsEnabled", "", "empty"},

		// RouteStatsShareWindowSize: >=0, <=100000
		{"RouteStatsShareWindowSize", "-1", "negative"},
		{"RouteStatsShareWindowSize", "-100", "negative"},
		{"RouteStatsShareWindowSize", "100001", "exceeds cap"},
		{"RouteStatsShareWindowSize", "abc", "not integer"},

		// RouteStatsShareCorrMin: (0, 1]
		{"RouteStatsShareCorrMin", "0", "excludes zero"},
		{"RouteStatsShareCorrMin", "-0.5", "negative"},
		{"RouteStatsShareCorrMin", "1.5", "exceeds 1"},
		{"RouteStatsShareCorrMin", "xyz", "not float"},

		// RouteStatsShareCorrMax: >=1
		{"RouteStatsShareCorrMax", "0.5", "below 1"},
		{"RouteStatsShareCorrMax", "0", "zero"},
		{"RouteStatsShareCorrMax", "nope", "not float"},

		// positive-integer keys
		{"RouteStatsMinSamples", "0", "zero not positive"},
		{"RouteStatsMinSamples", "-5", "negative"},
		{"RouteStatsTTLSeconds", "0", "zero not positive"},
		{"RouteStatsTTLSeconds", "-1", "negative"},
		{"RouteStatsTTFTTargetMs", "0", "zero not positive"},
		{"RouteStatsTTFTTargetMs", "bad", "not integer"},
		{"RouteStatsTPSTarget", "0", "zero not positive"},
		{"RouteStatsTPSTarget", "-1", "negative"},

		// RouteStatsQualityFloor / ComponentFloor: [0, 1]
		{"RouteStatsQualityFloor", "-0.1", "below 0"},
		{"RouteStatsQualityFloor", "1.1", "above 1"},
		{"RouteStatsQualityFloor", "bad", "not float"},
		{"RouteStatsComponentFloor", "-0.01", "below 0"},
		{"RouteStatsComponentFloor", "1.5", "above 1"},

		// RouteStatsQualityCeil / ComponentCeil: >=1
		{"RouteStatsQualityCeil", "0.5", "below 1"},
		{"RouteStatsQualityCeil", "0", "zero"},
		{"RouteStatsQualityCeil", "bad", "not float"},
		{"RouteStatsComponentCeil", "0.9", "below 1"},
	}

	for _, tc := range invalids {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			err := ValidateRouteStatsSettingValue(tc.key, tc.value)
			require.Error(t, err, tc.why)
		})
	}
}

// TestRouteStatsUpdateDoesNotMutateSharedPointer confirms UpdateRouteStatsSettingValue
// copies before publishing, so the live config is not mutated in place.
func TestRouteStatsUpdateDoesNotMutateSharedPointer(t *testing.T) {
	saved := routestats.GetRouteStatsSetting()
	t.Cleanup(func() {
		routestats.SetRouteStatsSetting(saved)
	})

	routestats.SetRouteStatsSetting(routestats.DefaultRouteStatsSetting())
	original := routestats.GetRouteStatsSetting()

	require.NoError(t, UpdateRouteStatsSettingValue("RouteStatsShareWindowSize", "50"))
	updated := routestats.GetRouteStatsSetting()

	assert.NotEqual(t, 50, original.ShareWindowSize, "original pointer must not be mutated")
	assert.Equal(t, 50, updated.ShareWindowSize)
}

// TestIsRouteStatsOptionKey confirms key membership.
func TestIsRouteStatsOptionKey(t *testing.T) {
	assert.True(t, IsRouteStatsOptionKey("RouteStatsEnabled"))
	assert.True(t, IsRouteStatsOptionKey("RouteStatsShareWindowSize"))
	assert.False(t, IsRouteStatsOptionKey("CalmFastBase"))
	assert.False(t, IsRouteStatsOptionKey("unknown"))
}