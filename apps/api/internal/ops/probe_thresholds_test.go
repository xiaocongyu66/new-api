package ops

import (
	"context"
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyProbeGrade(t *testing.T) {
	s := &ProbeSetting{
		WarningThreshold:     70,
		CriticalThreshold:    50,
		EmergencyThreshold:   30,
		ProbeIntervalMinutes: 30,
	}
	cases := []struct {
		availability float64
		want         ProbeGrade
	}{
		{100, ProbeGradeOK},
		{70, ProbeGradeOK}, // 边界：等于 warning
		{69.999, ProbeGradeWarning},
		{50, ProbeGradeWarning}, // 边界：等于 critical
		{49.999, ProbeGradeCritical},
		{30, ProbeGradeCritical}, // 边界：等于 emergency
		{29.999, ProbeGradeEmergency},
		{0, ProbeGradeEmergency},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, ClassifyProbeGrade(tc.availability, s))
	}
}

func countTitle(calls []dto.Notify, title string) (n int) {
	for _, c := range calls {
		if c.Title == title {
			n++
		}
	}
	return
}

func resetProbeZoneState() {
	probeZoneState.mu.Lock()
	probeZoneState.m = make(map[probeUnitKey]probeZone)
	probeZoneState.mu.Unlock()
}

func TestProbeThresholdFlipAndDedupPerModel(t *testing.T) {
	withProbeDB(t)
	now := common.GetTimestamp()

	require.NoError(t, dbx.DB.Create(&channelpkg.Channel{
		Id:          1,
		Type:        1,
		Key:         "sk-probe-1",
		Name:        "openai",
		Models:      "gpt-4o",
		Status:      common.ChannelStatusEnabled,
		CreatedTime: now,
	}).Error)
	require.NoError(t, dbx.DB.Create(&channelpkg.Channel{
		Id:          2,
		Type:        1,
		Key:         "sk-probe-2",
		Name:        "bad",
		Models:      "claude",
		Status:      common.ChannelStatusEnabled,
		CreatedTime: now,
	}).Error)

	insert := func(channelId int, model string, successCount, failCount int) {
		for i := 0; i < successCount; i++ {
			probeResultRow(t, channelId, model, true, "", now-int64(i))
		}
		for i := 0; i < failCount; i++ {
			probeResultRow(t, channelId, model, false, "upstream_error", now-int64(100+i))
		}
	}
	insert(1, "gpt-4o", 8, 2) // 80% → above
	insert(2, "claude", 2, 8) // 20% → below

	previous := probeSettingSnapshot()
	t.Cleanup(func() {
		restoreProbeSetting(previous)
		resetProbeZoneState()
	})

	previousSend := SendProbeWebhook
	var calls []dto.Notify
	SendProbeWebhook = func(url, secret string, n dto.Notify) error {
		calls = append(calls, n)
		return nil
	}
	t.Cleanup(func() { SendProbeWebhook = previousSend })

	probeSetting = ProbeSetting{
		ProbeIntervalMinutes: 30,
		WarningThreshold:     70,
		CriticalThreshold:    50,
		EmergencyThreshold:   30,
		WebhookEnabled:       true,
		WebhookUrl:           "http://127.0.0.1:9/probe",
	}
	resetProbeZoneState()

	// 首次：两个模型各穿越一次（一恢复一告警）
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Len(t, calls, 2)
	assert.Equal(t, 1, countTitle(calls, "路由健康探测恢复"))
	assert.Equal(t, 1, countTitle(calls, "路由健康探测告警"))

	// 再次：区间未变，不重发（方向性：不是"贴阈值就通知"）
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Len(t, calls, 2)

	// 渠道 2 的模型恢复：below → above
	require.NoError(t, dbx.DB.Model(&ChannelProbeResult{}).Where("channel_id = ?", 2).
		Updates(map[string]interface{}{"success": true, "error_code": ""}).Error)
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Len(t, calls, 3)
	assert.Equal(t, 2, countTitle(calls, "路由健康探测恢复"))

	// 渠道 1 的模型跌落：above → below
	require.NoError(t, dbx.DB.Model(&ChannelProbeResult{}).Where("channel_id = ?", 1).
		Updates(map[string]interface{}{"success": false, "error_code": "probe-down"}).Error)
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Len(t, calls, 4)
	assert.Equal(t, 2, countTitle(calls, "路由健康探测告警"))

	// webhook 关闭：再翻转也不发
	probeSetting = ProbeSetting{
		ProbeIntervalMinutes: 30,
		WarningThreshold:     70,
		CriticalThreshold:    50,
		EmergencyThreshold:   30,
		WebhookEnabled:       false,
		WebhookUrl:           "http://127.0.0.1:9/probe",
	}
	require.NoError(t, dbx.DB.Model(&ChannelProbeResult{}).Where("channel_id = ?", 1).
		Updates(map[string]interface{}{"success": true, "error_code": ""}).Error)
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Len(t, calls, 4)
}

func TestProbeAutoDisableAndRestoreWithPolicy(t *testing.T) {
	withProbeDB(t)
	now := common.GetTimestamp()

	policy := &dto.ChannelProbePolicy{AutoControlEnabled: true, DisableBelow: 50, EnableAbove: 70}
	settings, err := common.Marshal(&dto.ChannelOtherSettings{ProbePolicy: policy})
	require.NoError(t, err)
	require.NoError(t, dbx.DB.Create(&channelpkg.Channel{
		Id:            3,
		Type:          1,
		Key:           "sk-probe-3",
		Name:          "policy-chan",
		Models:        "m1",
		Status:        common.ChannelStatusEnabled,
		OtherSettings: string(settings),
		CreatedTime:   now,
	}).Error)

	insertRows := func(successCount, failCount int) {
		require.NoError(t, dbx.DB.Where("channel_id = ?", 3).Delete(&ChannelProbeResult{}).Error)
		for i := 0; i < successCount; i++ {
			probeResultRow(t, 3, "m1", true, "", now-int64(i))
		}
		for i := 0; i < failCount; i++ {
			probeResultRow(t, 3, "m1", false, "upstream_error", now-int64(100+i))
		}
	}

	previous := probeSettingSnapshot()
	t.Cleanup(func() {
		restoreProbeSetting(previous)
		resetProbeZoneState()
	})
	probeSetting = ProbeSetting{
		ProbeIntervalMinutes: 30,
		WarningThreshold:     70,
		CriticalThreshold:    50,
		EmergencyThreshold:   30,
		WebhookEnabled:       true,
		WebhookUrl:           "http://127.0.0.1:9/probe",
	}

	previousSend := SendProbeWebhook
	var calls []dto.Notify
	SendProbeWebhook = func(url, secret string, n dto.Notify) error {
		calls = append(calls, n)
		return nil
	}
	t.Cleanup(func() { SendProbeWebhook = previousSend })

	var disabled, restored []string
	prevDisable, prevRestore := autoDisableModel, autoRestoreModel
	autoDisableModel = func(channelID int, model string) error {
		disabled = append(disabled, model)
		return nil
	}
	autoRestoreModel = func(channelID int, model string) error {
		restored = append(restored, model)
		return nil
	}
	t.Cleanup(func() { autoDisableModel, autoRestoreModel = prevDisable, prevRestore })
	resetProbeZoneState()

	// 20% → 跌破 50%：自动禁用一次
	insertRows(2, 8)
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Len(t, disabled, 1)
	assert.Equal(t, "m1", disabled[0])
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].Content, "已自动禁用")

	// 再次评估：区间未变，不重复禁用/通知
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Len(t, disabled, 1)
	require.Len(t, calls, 1)

	// 90% → 高于 70%：自动恢复一次
	insertRows(9, 1)
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Len(t, restored, 1)
	require.Len(t, calls, 2)
	assert.Contains(t, calls[1].Content, "已自动恢复")

	// 迟滞带内（60%）不动作不通知
	insertRows(6, 4)
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Len(t, disabled, 1)
	require.Len(t, restored, 1)
	require.Len(t, calls, 2)
}

func TestProbePolicyValidation(t *testing.T) {
	bad := []*dto.ChannelProbePolicy{
		{DisableBelow: -1, EnableAbove: 70},
		{DisableBelow: 50, EnableAbove: 101},
		{DisableBelow: 70, EnableAbove: 70},
		{DisableBelow: 80, EnableAbove: 60},
	}
	for _, p := range bad {
		require.Error(t, p.Validate())
	}
	require.NoError(t, (&dto.ChannelProbePolicy{DisableBelow: 50, EnableAbove: 70}).Validate())
	p := &dto.ChannelProbePolicy{}
	p.Normalize()
	assert.Equal(t, float64(dto.DefaultProbeDisableBelow), p.DisableBelow)
	assert.Equal(t, float64(dto.DefaultProbeEnableAbove), p.EnableAbove)
}
