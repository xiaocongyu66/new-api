package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateProbeSettingValue(t *testing.T) {
	valid := ProbeSetting{
		WarningThreshold:     70,
		CriticalThreshold:    50,
		EmergencyThreshold:   30,
		ProbeIntervalMinutes: 30,
	}
	require.NoError(t, ValidateProbeSettingValue(valid))

	// 阈值越界
	bad := valid
	bad.WarningThreshold = -1
	assert.Error(t, ValidateProbeSettingValue(bad))
	bad = valid
	bad.WarningThreshold = 101
	assert.Error(t, ValidateProbeSettingValue(bad))
	bad = valid
	bad.CriticalThreshold = -0.5
	assert.Error(t, ValidateProbeSettingValue(bad))
	bad = valid
	bad.EmergencyThreshold = 100.0001
	assert.Error(t, ValidateProbeSettingValue(bad))

	// 间隔非法
	bad = valid
	bad.ProbeIntervalMinutes = -1
	assert.Error(t, ValidateProbeSettingValue(bad))

	// 阈值顺序非法
	bad = valid
	bad.WarningThreshold = 70
	bad.CriticalThreshold = 70
	assert.Error(t, ValidateProbeSettingValue(bad))
	bad = valid
	bad.CriticalThreshold = 30
	bad.EmergencyThreshold = 30
	assert.Error(t, ValidateProbeSettingValue(bad))

	// webhook 开启时 url 必须是 http(s)
	bad = valid
	bad.WebhookEnabled = true
	bad.WebhookUrl = "ftp://example.com"
	assert.Error(t, ValidateProbeSettingValue(bad))
	bad = valid
	bad.WebhookEnabled = true
	bad.WebhookUrl = "example.com"
	assert.Error(t, ValidateProbeSettingValue(bad))
	bad = valid
	bad.WebhookEnabled = true
	bad.WebhookUrl = "http://127.0.0.1:8000/hooks"
	require.NoError(t, ValidateProbeSettingValue(bad))
	bad = valid
	bad.WebhookEnabled = true
	bad.WebhookUrl = "https://example.com/hooks"
	require.NoError(t, ValidateProbeSettingValue(bad))
	// webhook 未开启时 url 不限
	bad = valid
	bad.WebhookEnabled = false
	bad.WebhookUrl = "anything"
	require.NoError(t, ValidateProbeSettingValue(bad))
}

func probeSettingSnapshot() ProbeSetting {
	probeSettingMu.RLock()
	defer probeSettingMu.RUnlock()
	return probeSetting
}

func restoreProbeSetting(s ProbeSetting) {
	probeSettingMu.Lock()
	probeSetting = s
	probeSettingMu.Unlock()
}

func TestGetProbeSettingNormalize(t *testing.T) {
	previous := probeSettingSnapshot()
	t.Cleanup(func() { restoreProbeSetting(previous) })

	// 越界夹回 [0,100]；顺序不合法回退默认 70/50/30；间隔 < 0 回退 30
	probeSetting = ProbeSetting{
		WarningThreshold:     120,
		CriticalThreshold:    -5,
		EmergencyThreshold:   200,
		ProbeIntervalMinutes: -10,
		WebhookEnabled:       true,
		WebhookUrl:           "https://example.com",
	}
	s := GetProbeSetting()
	require.Equal(t, 70.0, s.WarningThreshold)
	require.Equal(t, 50.0, s.CriticalThreshold)
	require.Equal(t, 30.0, s.EmergencyThreshold)
	require.Equal(t, 30.0, s.ProbeIntervalMinutes)
	require.True(t, s.WebhookEnabled)
	require.Equal(t, "https://example.com", s.WebhookUrl)

	// 仅越界，顺序合法：保留夹后值
	probeSetting = ProbeSetting{
		WarningThreshold:     150,
		CriticalThreshold:    60,
		EmergencyThreshold:   10,
		ProbeIntervalMinutes: 0,
	}
	s = GetProbeSetting()
	require.Equal(t, 100.0, s.WarningThreshold)
	require.Equal(t, 60.0, s.CriticalThreshold)
	require.Equal(t, 10.0, s.EmergencyThreshold)
	require.Equal(t, 0.0, s.ProbeIntervalMinutes)
}
