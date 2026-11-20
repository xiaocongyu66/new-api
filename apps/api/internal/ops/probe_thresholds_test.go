package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/usage"
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
	probeControlZoneState.mu.Lock()
	probeControlZoneState.m = make(map[probeUnitKey]probeZone)
	probeControlZoneState.mu.Unlock()
	probeNotifyZoneState.mu.Lock()
	probeNotifyZoneState.m = make(map[probeUnitKey]probeZone)
	probeNotifyZoneState.mu.Unlock()
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
	prevHealth := loadProbeWebhookHealth
	loadProbeWebhookHealth = func(context.Context, int64, int64, []string) ([]usage.UpstreamHealthPoint, error) {
		return nil, nil
	}
	t.Cleanup(func() { loadProbeWebhookHealth = prevHealth })

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
	require.NoError(t, dbx.DB.Create(&channelpkg.Ability{
		Group: "default", Model: "m1", ChannelId: 3, Enabled: true,
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
		return dbx.DB.Model(&channelpkg.Ability{}).
			Where("channel_id = ? AND model = ?", channelID, model).
			Updates(map[string]interface{}{"enabled": false, "auto_disabled": true}).Error
	}
	autoRestoreModel = func(channelID int, model string) error {
		restored = append(restored, model)
		return dbx.DB.Model(&channelpkg.Ability{}).
			Where("channel_id = ? AND model = ?", channelID, model).
			Updates(map[string]interface{}{"enabled": true, "auto_disabled": false}).Error
	}
	t.Cleanup(func() { autoDisableModel, autoRestoreModel = prevDisable, prevRestore })
	resetProbeZoneState()
	prevHealth := loadProbeWebhookHealth
	loadProbeWebhookHealth = func(context.Context, int64, int64, []string) ([]usage.UpstreamHealthPoint, error) {
		return nil, nil
	}
	t.Cleanup(func() { loadProbeWebhookHealth = prevHealth })

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

func TestProbeWebhookBlendedDecisionIndependentOfControl(t *testing.T) {
	withProbeDB(t)
	now := common.GetTimestamp()

	require.NoError(t, dbx.DB.Create(&channelpkg.Channel{
		Id:          10,
		Type:        1,
		Key:         "sk-blend",
		Name:        "blend-channel",
		Models:      "blend-model",
		Status:      common.ChannelStatusEnabled,
		CreatedTime: now,
	}).Error)
	require.NoError(t, dbx.DB.Create(&channelpkg.Channel{
		Id:          11,
		Type:        1,
		Key:         "sk-upstream-only",
		Name:        "upstream-only-channel",
		Models:      "upstream-only",
		Status:      common.ChannelStatusEnabled,
		CreatedTime: now,
	}).Error)
	require.NoError(t, dbx.DB.Create(&channelpkg.Channel{
		Id:          12,
		Type:        1,
		Key:         "sk-no-signal",
		Name:        "no-signal-channel",
		Models:      "no-signal",
		Status:      common.ChannelStatusEnabled,
		CreatedTime: now,
	}).Error)
	for i := 0; i < 10; i++ {
		probeResultRow(t, 10, "blend-model", true, "", now-int64(i))
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
	resetProbeZoneState()

	prevSend := SendProbeWebhook
	var calls []dto.Notify
	SendProbeWebhook = func(url, secret string, n dto.Notify) error {
		calls = append(calls, n)
		return nil
	}
	t.Cleanup(func() { SendProbeWebhook = prevSend })

	prevDisable, prevRestore := autoDisableModel, autoRestoreModel
	autoDisableModel = func(channelID int, model string) error {
		t.Fatalf("unexpected auto disable: channel %d model %s", channelID, model)
		return nil
	}
	autoRestoreModel = func(channelID int, model string) error {
		t.Fatalf("unexpected auto restore: channel %d model %s", channelID, model)
		return nil
	}
	t.Cleanup(func() { autoDisableModel, autoRestoreModel = prevDisable, prevRestore })

	healthPoints := []usage.UpstreamHealthPoint{
		{ChannelID: 10, Model: "blend-model", Rate: 20, LatencyMs: 150, Weight: 1},
		{ChannelID: 11, Model: "upstream-only", Rate: 10, LatencyMs: 400, Weight: 2},
	}
	healthErr := error(nil)
	healthCalls := 0
	prevHealth := loadProbeWebhookHealth
	loadProbeWebhookHealth = func(context.Context, int64, int64, []string) ([]usage.UpstreamHealthPoint, error) {
		healthCalls++
		return healthPoints, healthErr
	}
	t.Cleanup(func() { loadProbeWebhookHealth = prevHealth })

	// 100% 探测与 20% 上游均值 60%，跌破 70%：原始探测不会自动禁用，
	// 但融合口径仍判定告警；无探测行的 11 号渠道仅由上游支撑。
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Equal(t, 2, len(calls))
	require.Equal(t, 1, healthCalls)

	blendNotify := findNotify(calls, "blend-model")
	require.NotNil(t, blendNotify)
	assert.Equal(t, "路由健康探测告警", blendNotify.Title)
	assert.Contains(t, blendNotify.Content, "通知判定可用率 60.0%")
	assert.Contains(t, blendNotify.Content, "24h 探测与上游请求观测均值")
	assert.Contains(t, blendNotify.Content, "10/10 次成功")

	upstreamOnly := findNotify(calls, "upstream-only")
	require.NotNil(t, upstreamOnly)
	assert.Equal(t, "路由健康探测告警", upstreamOnly.Title)
	assert.Contains(t, upstreamOnly.Content, "通知判定可用率 10.0%")
	assert.Contains(t, upstreamOnly.Content, "上游请求观测均值；无 24h 探测样本")

	// 区间不变，重复评估不新增通知。
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Equal(t, 2, len(calls))
	require.Equal(t, 2, healthCalls)

	// 上游回升到 100%，融合 100%，判定恢复，不新增自动恢复。
	healthPoints = []usage.UpstreamHealthPoint{
		{ChannelID: 10, Model: "blend-model", Rate: 100, LatencyMs: 150, Weight: 1},
		{ChannelID: 11, Model: "upstream-only", Rate: 100, LatencyMs: 400, Weight: 2},
	}
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Equal(t, 4, len(calls))
	require.Equal(t, 3, healthCalls)
	require.Equal(t, 2, countTitle(calls, "路由健康探测恢复"))
	recovery := findNotify(calls, "blend-model", "路由健康探测恢复")
	require.NotNil(t, recovery)
	assert.Contains(t, recovery.Content, "通知判定可用率 100.0%")
	assert.Contains(t, recovery.Content, "10/10 次成功")
	assert.NotContains(t, recovery.Content, "已自动恢复")

	// 相同区间不重复。
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Equal(t, 4, len(calls))

	// 关闭 webhook：不再调用上游健康，也不再发通知。
	probeSetting.WebhookEnabled = false
	healthPoints = []usage.UpstreamHealthPoint{
		{ChannelID: 10, Model: "blend-model", Rate: 0, LatencyMs: 150},
	}
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Equal(t, 4, len(calls))
	require.Equal(t, 4, healthCalls)
}

func TestProbeWebhookLoadFailureFallsBackToProbes(t *testing.T) {
	withProbeDB(t)
	now := common.GetTimestamp()

	require.NoError(t, dbx.DB.Create(&channelpkg.Channel{
		Id:          13,
		Type:        1,
		Key:         "sk-fallback",
		Name:        "fallback-channel",
		Models:      "fallback-model",
		Status:      common.ChannelStatusEnabled,
		CreatedTime: now,
	}).Error)
	for i := 0; i < 10; i++ {
		probeResultRow(t, 13, "fallback-model", true, "", now-int64(i))
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
	resetProbeZoneState()

	prevSend := SendProbeWebhook
	var calls []dto.Notify
	SendProbeWebhook = func(url, secret string, n dto.Notify) error {
		calls = append(calls, n)
		return nil
	}
	t.Cleanup(func() { SendProbeWebhook = prevSend })

	prevHealth := loadProbeWebhookHealth
	loadProbeWebhookHealth = func(context.Context, int64, int64, []string) ([]usage.UpstreamHealthPoint, error) {
		return nil, errors.New("upstream health unavailable")
	}
	t.Cleanup(func() { loadProbeWebhookHealth = prevHealth })

	// 上游健康读取失败后保留探测回退：只写 100% 探测，不提及上游观测。
	EvaluateAndNotifyProbeThresholds(context.Background())
	require.Len(t, calls, 1)
	assert.Equal(t, "路由健康探测恢复", calls[0].Title)
	assert.Contains(t, calls[0].Content, "通知判定可用率 100.0%")
	assert.Contains(t, calls[0].Content, "24h 探测 10/10 次成功")
	assert.NotContains(t, calls[0].Content, "上游请求观测")
}

// TestProbeWebhookNotifyStatusIgnoresDivergentControlZone pins that a webhook
// judged on the blended zone never reuses control wording from a different
// raw-probe zone. Raw probe 60% triggers auto-disable (below), while upstream
// 100% lifts the blended rate to 80% (above): the notification must be a
// recovery message without claiming a disable action in its body.
func TestProbeWebhookNotifyStatusIgnoresDivergentControlZone(t *testing.T) {
	withProbeDB(t)
	now := common.GetTimestamp()

	policy := &dto.ChannelProbePolicy{AutoControlEnabled: true, DisableBelow: 70, EnableAbove: 75}
	settings, err := common.Marshal(&dto.ChannelOtherSettings{ProbePolicy: policy})
	require.NoError(t, err)
	require.NoError(t, dbx.DB.Create(&channelpkg.Channel{
		Id: 14, Type: 1, Key: "sk-divergent", Name: "divergent",
		Models: "divergent-model", Status: common.ChannelStatusEnabled,
		OtherSettings: string(settings), CreatedTime: now,
	}).Error)
	require.NoError(t, dbx.DB.Create(&channelpkg.Ability{
		Group: "default", Model: "divergent-model", ChannelId: 14, Enabled: true,
	}).Error)
	for i := range 6 {
		probeResultRow(t, 14, "divergent-model", true, "", now-int64(i))
	}
	for i := range 4 {
		probeResultRow(t, 14, "divergent-model", false, "upstream_error", now-int64(100+i))
	}

	previous := probeSettingSnapshot()
	t.Cleanup(func() { restoreProbeSetting(previous); resetProbeZoneState() })
	probeSetting = ProbeSetting{
		ProbeIntervalMinutes: 30,
		WarningThreshold:     70,
		CriticalThreshold:    50,
		EmergencyThreshold:   30,
		WebhookEnabled:       true,
		WebhookUrl:           "http://127.0.0.1:9/probe",
	}
	resetProbeZoneState()

	prevSend := SendProbeWebhook
	var calls []dto.Notify
	SendProbeWebhook = func(url, secret string, n dto.Notify) error {
		calls = append(calls, n)
		return nil
	}
	t.Cleanup(func() { SendProbeWebhook = prevSend })

	var disabled []string
	prevDisable, prevRestore := autoDisableModel, autoRestoreModel
	autoDisableModel = func(channelID int, model string) error {
		disabled = append(disabled, model)
		return nil
	}
	autoRestoreModel = func(channelID int, model string) error { return nil }
	t.Cleanup(func() { autoDisableModel, autoRestoreModel = prevDisable, prevRestore })

	prevHealth := loadProbeWebhookHealth
	loadProbeWebhookHealth = func(context.Context, int64, int64, []string) ([]usage.UpstreamHealthPoint, error) {
		return []usage.UpstreamHealthPoint{{ChannelID: 14, Model: "divergent-model", Rate: 100, Weight: 1}}, nil
	}
	t.Cleanup(func() { loadProbeWebhookHealth = prevHealth })

	EvaluateAndNotifyProbeThresholds(context.Background())

	// Raw probe 60% still triggers the automatic disable.
	require.Equal(t, []string{"divergent-model"}, disabled)
	// Blended 80% is above enableAbove: notification is a recovery with
	// notify-zone wording, never the raw-probe control wording.
	require.Len(t, calls, 1)
	assert.Equal(t, "路由健康探测恢复", calls[0].Title)
	assert.Contains(t, calls[0].Content, "通知判定可用率 80.0%")
	assert.Contains(t, calls[0].Content, "（通知判定）")
	assert.NotContains(t, calls[0].Content, "已自动禁用")
	assert.NotContains(t, calls[0].Content, "已自动恢复")
}

func findNotify(calls []dto.Notify, model string, titles ...string) *dto.Notify {
	for i := range calls {
		if !strings.Contains(calls[i].Content, model) {
			continue
		}
		if len(titles) > 0 && calls[i].Title != titles[0] {
			continue
		}
		return &calls[i]
	}
	return nil
}
