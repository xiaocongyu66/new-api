package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRecallPolicyFor 覆盖 recall_policies 解析与新旧配置的优先级。
func TestRecallPolicyFor(t *testing.T) {
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
		want        int
	}{
		{"policies 命中类型", `{"drop_award":30,"checkin_fail":10}`, false, 10, RecallKindDropAward, 30},
		{"policies 显式 0 不撤回", `{"drop_award":30,"checkin_fail":0}`, true, 10, RecallKindCheckinFail, 0},
		{"policies 缺失类型为 0", `{"drop_award":30}`, true, 10, RecallKindMenu, 0},
		{"policies 负数视为不撤回", `{"drop_award":-5}`, false, 10, RecallKindDropAward, 0},
		// failure_notice 在 policies 生效时完全接管，旧开关被忽略
		{"policies 含 failure_notice", `{"failure_notice":7}`, true, 20, RecallKindFailureNotice, 7},
		{"policies 生效时忽略旧开关", `{"drop_award":30}`, true, 20, RecallKindFailureNotice, 0},
		// 配置为空 → 回落旧行为
		{"空配置 failure_notice 走旧开关", "", true, 15, RecallKindFailureNotice, 15},
		{"空配置 其他类型不撤回", "", true, 15, RecallKindDropAward, 0},
		{"空配置且旧开关关闭", "", false, 10, RecallKindFailureNotice, 0},
		{"空映射回落旧开关", `{}`, true, 12, RecallKindFailureNotice, 12},
		// 非空但解析失败 → 一律不撤回（宁可不撤不可误撤）
		{"解析失败全部禁用", `{"drop_award":`, true, 10, RecallKindFailureNotice, 0},
		{"类型错误解析失败", `"not-a-map"`, true, 10, RecallKindDropAward, 0},
		// 旧开关延迟<=0 时 AutoRecallFailed 兜底为 10
		{"旧开关延迟非法回落10", "", true, 0, RecallKindFailureNotice, 10},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s.RecallPolicies = tc.policies
			s.RecallFailedMessages = tc.legacyOn
			s.RecallDelaySeconds = tc.legacyDelay
			assert.Equal(t, tc.want, recallPolicyFor(tc.kind))
		})
	}
}

// TestRecallPolicyForLegacyFailurePathUnchanged 验证未配置 policies 时，
// isFailureReply 路径与旧行为一致：failure_notice 由旧开关决定，其余类型永不撤回。
func TestRecallPolicyForLegacyFailurePathUnchanged(t *testing.T) {
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

	assert.Equal(t, 8, recallPolicyFor(RecallKindFailureNotice))
	assert.Equal(t, 0, recallPolicyFor(RecallKindCheckinSuccess))
	assert.Equal(t, 0, recallPolicyFor(""))

	s.RecallFailedMessages = false
	assert.Equal(t, 0, recallPolicyFor(RecallKindFailureNotice))
}
