package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRecallDelayFor 覆盖 recall_policies 解析、按文案兜底与新旧配置的优先级。
func TestRecallDelayFor(t *testing.T) {
	s := GetQQBotSetting()
	origPolicies := s.RecallPolicies
	origFailed := s.RecallFailedMessages
	origDelay := s.RecallDelaySeconds
	defer func() {
		s.RecallPolicies = origPolicies
		s.RecallFailedMessages = origFailed
		s.RecallDelaySeconds = origDelay
	}()

	cases := []struct {
		name        string
		policies    string
		legacyOn    bool
		legacyDelay int
		kind        string
		content     string
		want        int
	}{
		{"policies 命中类型", `{"drop_award":30,"checkin_fail":10}`, false, 10, RecallKindDropAward, "", 30},
		{"policies 显式 0 不撤回", `{"drop_award":30,"checkin_fail":0}`, true, 10, RecallKindCheckinFail, "", 0},
		{"policies 缺失类型为 0", `{"drop_award":30}`, true, 10, RecallKindMenu, "", 0},
		{"policies 负数视为不撤回", `{"drop_award":-5}`, false, 10, RecallKindDropAward, "", 0},
		// bind_fail：未配置 policies 时给默认保留时间，受总开关控制；显式配置（含 0）完全接管
		{"bind_fail 空配置默认30秒", "", true, 10, RecallKindBindFail, "", DefaultBindFailRecallSeconds},
		{"bind_fail 空配置开关关不撤回", "", false, 10, RecallKindBindFail, "", 0},
		{"bind_fail 空映射默认30秒", `{}`, true, 20, RecallKindBindFail, "", DefaultBindFailRecallSeconds},
		{"bind_fail 显式延迟", `{"bind_fail":45}`, true, 10, RecallKindBindFail, "", 45},
		{"bind_fail 显式延迟忽略总开关", `{"bind_fail":45}`, false, 10, RecallKindBindFail, "", 45},
		{"bind_fail 显式0不撤回", `{"bind_fail":0}`, true, 10, RecallKindBindFail, "", 0},
		{"bind_fail policies无此键不撤回", `{"drop_award":30}`, true, 10, RecallKindBindFail, "", 0},
		// 回归：policies 显式配置时，缺席的 kind 不得被 failure_notice 按文案兜底救回
		{"policies 有failure_notice但bind_fail缺席不撤回", `{"failure_notice":7}`, true, 10, RecallKindBindFail, "**绑定失败！**\n\n验证码错误", 0},
		{"policies 生效时空kind失败文案不兜底", `{"drop_award":30}`, true, 10, "", "**签到失败！**\n\n请先绑定", 0},
		// failure_notice 在 policies 生效时完全接管，旧开关被忽略
		{"policies 含 failure_notice", `{"failure_notice":7}`, true, 20, RecallKindFailureNotice, "", 7},
		{"policies 生效时忽略旧开关", `{"drop_award":30}`, true, 20, RecallKindFailureNotice, "", 0},
		// 配置为空 → 回落旧行为
		{"空配置 failure_notice 走旧开关", "", true, 15, RecallKindFailureNotice, "", 15},
		{"空配置 其他类型不撤回", "", true, 15, RecallKindDropAward, "", 0},
		{"空配置且旧开关关闭", "", false, 10, RecallKindFailureNotice, "", 0},
		{"空映射回落旧开关", `{}`, true, 12, RecallKindFailureNotice, "", 12},
		// 未配置 policies 时，kind 为空但文案命中 isFailureReply → 旧的按内容撤回行为
		{"空配置空kind失败文案走旧开关", "", true, 9, "", "**转账失败！**\n\n余额不足", 9},
		{"空配置空kind成功文案不撤回", "", true, 9, "", "**签到成功！**\n\n获得 1.00", 0},
		// 非空但解析失败 → 一律不撤回（宁可不撤不可误撤）
		{"解析失败全部禁用", `{"drop_award":`, true, 10, RecallKindFailureNotice, "", 0},
		{"类型错误解析失败", `"not-a-map"`, true, 10, RecallKindDropAward, "", 0},
		// 旧开关延迟<=0 时 AutoRecallFailed 兜底为 10
		{"旧开关延迟非法回落10", "", true, 0, RecallKindFailureNotice, "", 10},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s.RecallPolicies = tc.policies
			s.RecallFailedMessages = tc.legacyOn
			s.RecallDelaySeconds = tc.legacyDelay
			assert.Equal(t, tc.want, recallDelayFor(tc.kind, tc.content))
		})
	}
}

// TestRecallDelayForLegacyFailurePathUnchanged 验证未配置 policies 时，
// isFailureReply 路径与旧行为一致：failure_notice 由旧开关决定；bind_fail 的
// 默认保留时间由上面的表驱动用例覆盖，这里只确认其余类型不受旧开关影响。
func TestRecallDelayForLegacyFailurePathUnchanged(t *testing.T) {
	s := GetQQBotSetting()
	origPolicies := s.RecallPolicies
	origFailed := s.RecallFailedMessages
	origDelay := s.RecallDelaySeconds
	defer func() {
		s.RecallPolicies = origPolicies
		s.RecallFailedMessages = origFailed
		s.RecallDelaySeconds = origDelay
	}()

	s.RecallPolicies = ""
	s.RecallFailedMessages = true
	s.RecallDelaySeconds = 8

	assert.Equal(t, 8, recallDelayFor(RecallKindFailureNotice, ""))
	assert.Equal(t, 0, recallDelayFor(RecallKindCheckinSuccess, ""))
	assert.Equal(t, 0, recallDelayFor("", ""))

	s.RecallFailedMessages = false
	assert.Equal(t, 0, recallDelayFor(RecallKindFailureNotice, ""))
}
