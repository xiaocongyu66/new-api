package channel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultChannelModelHealthSetting_FailureThresholdsAreOne(t *testing.T) {
	s := DefaultChannelModelHealthSetting()

	require.NotNil(t, s)
	assert.Equal(t, 1, s.LocalFailureThreshold)
	assert.Equal(t, 1, s.UpstreamFailureThreshold)
	assert.Equal(t, 5, s.FastWindowUnits)
	assert.Equal(t, 5, s.FastWindowCapSeconds)
	assert.Equal(t, 10, s.LargeWindowCapSeconds)
}

// TestWindowCapReadsLiveSettings pins that the cooldown tier caps follow the
// running settings rather than baked-in constants: a raised boundary and
// shifted caps must be honored by windowCapSeconds.
func TestWindowCapReadsLiveSettings(t *testing.T) {
	cfg := DefaultChannelModelHealthSetting()
	cfg.FastWindowUnits = 3
	cfg.FastWindowCapSeconds = 4
	cfg.LargeWindowCapSeconds = 9
	withHealthSetting(t, cfg)

	assert.Equal(t, int64(4), windowCapSeconds(2), "pool below the boundary uses the fast cap")
	assert.Equal(t, int64(4), windowCapSeconds(3), "pool at the boundary uses the fast cap")
	assert.Equal(t, int64(9), windowCapSeconds(4), "pool above the boundary uses the large cap")
	assert.Equal(t, int64(9), windowCapSeconds(99))
}

func TestValidateChannelModelHealthSettingValue_FailureThresholds(t *testing.T) {
	thresholdKeys := []string{"LocalFailureThreshold", "UpstreamFailureThreshold", "FastWindowCapSeconds", "LargeWindowCapSeconds"}

	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"accepts one", "1", false},
		{"accepts large positive", "5", false},
		{"rejects zero", "0", true},
		{"rejects negative", "-1", true},
		{"rejects fractional", "1.5", true},
		{"rejects nonnumeric", "abc", true},
	}

	for _, key := range thresholdKeys {
		for _, tc := range cases {
			t.Run(key+"/"+tc.name, func(t *testing.T) {
				err := ValidateChannelModelHealthSettingValue(key, tc.value)
				if tc.wantErr {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	}
}

func TestValidateChannelModelHealthSettingValue_DormantDisableThresholdAllowsZero(t *testing.T) {
	assert.NoError(t, ValidateChannelModelHealthSettingValue("DormantDisableThreshold", "0"))
	assert.NoError(t, ValidateChannelModelHealthSettingValue("DormantDisableThreshold", "10"))
	assert.Error(t, ValidateChannelModelHealthSettingValue("DormantDisableThreshold", "-1"))
	assert.Error(t, ValidateChannelModelHealthSettingValue("DormantDisableThreshold", "1.5"))
	assert.Error(t, ValidateChannelModelHealthSettingValue("DormantDisableThreshold", "abc"))
}

func TestValidateChannelModelHealthSettingValue_UnknownKey(t *testing.T) {
	assert.Error(t, ValidateChannelModelHealthSettingValue("NonexistentKey", "1"))
	assert.Error(t, ValidateChannelModelHealthSettingValue("", "1"))
}

func TestUpdateChannelModelHealthSettingValue_OnlyChangesRequestedThreshold(t *testing.T) {
	orig := GetChannelModelHealthSetting()
	t.Cleanup(func() { channelModelHealthSetting.Store(orig) })

	t.Run("LocalFailureThreshold", func(t *testing.T) {
		require.NoError(t, UpdateChannelModelHealthSettingValue("LocalFailureThreshold", "7"))

		updated := GetChannelModelHealthSetting()
		assert.Equal(t, 7, updated.LocalFailureThreshold)
		assert.Equal(t, orig.UpstreamFailureThreshold, updated.UpstreamFailureThreshold)
	})

	t.Run("UpstreamFailureThreshold", func(t *testing.T) {
		channelModelHealthSetting.Store(orig)
		require.NoError(t, UpdateChannelModelHealthSettingValue("UpstreamFailureThreshold", "9"))

		updated := GetChannelModelHealthSetting()
		assert.Equal(t, 9, updated.UpstreamFailureThreshold)
		assert.Equal(t, orig.LocalFailureThreshold, updated.LocalFailureThreshold)
	})
}
