package billing

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestCheckCooldownCleanupWhenMapLarge tests that cleanup runs when map > 1000 entries.
func TestCheckCooldownCleanupWhenMapLarge(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.CommandCooldownSeconds
	defer func() { s.CommandCooldownSeconds = orig }()
	s.CommandCooldownSeconds = 1

	fillCooldownTracker(1500)

	openID := "cleanup-test-user"
	assert.NoError(t, CheckCooldown(openID))

	cooldownMu.Lock()
	if entry, ok := cooldownTracker[openID]; ok {
		entry.lastSeen = time.Now().Add(-10 * time.Second)
		cooldownTracker[openID] = entry
	}
	cooldownMu.Unlock()

	assert.NoError(t, CheckCooldown(openID))
}

func fillCooldownTracker(n int) {
	cooldownMu.Lock()
	defer cooldownMu.Unlock()
	for i := 0; i < n; i++ {
		key := "user-" + strconv.Itoa(i)
		cooldownTracker[key] = cooldownEntry{lastSeen: time.Now()}
	}
}

// TestAutoRecallFailedDelayZero tests that delay=0 defaults to 10.
func TestAutoRecallFailedDelayZero(t *testing.T) {
	s := GetQQBotSetting()
	origFailed := s.RecallFailedMessages
	origDelay := s.RecallDelaySeconds
	defer func() {
		s.RecallFailedMessages = origFailed
		s.RecallDelaySeconds = origDelay
	}()

	s.RecallFailedMessages = true
	s.RecallDelaySeconds = 0

	enabled, delay := AutoRecallFailed()
	assert.True(t, enabled)
	assert.Equal(t, 10, delay)
}

// TestIsFailureReply 只有失败类提示会被自动撤回,成功与查询类文案一律不撤。
func TestIsFailureReply(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"签到失败", "**签到失败！**\n\n请先绑定站点账号", true},
		{"抢红包失败未绑定", "**抢红包失败！**\n\n请先绑定站点账号才能领取", true},
		{"没抢到", "**没抢到！**\n\n红包已经被抢完了", true},
		{"红包无效", "**红包无效**", true},
		{"红包不存在", "**红包不存在**", true},
		{"发红包失败", "**发红包失败！**\n\n余额不足", true},
		{"转账失败", "**转账失败！**\n\n对方还没有绑定站点账号", true},
		{"绑定失败", "**绑定失败！**\n\n验证码错误", true},
		{"查询失败", "**查询失败！**\n\n系统繁忙", true},
		{"签到成功", "**签到成功！**\n\n您已获得 1.00 点额度", false},
		{"抢到红包", "@xx 抢到了 **1.00**！\n\n红包还剩 4 份", false},
		{"菜单", "**功能菜单**\n\n请选择要使用的功能", false},
		{"余额查询", "**余额查询**\n\n当前余额 10.00", false},
		{"红包战绩", "**红包战绩**\n\n总额 10.00，共 5 份", false},
		{"群状态", "**本群功能状态**\n\n签到：开", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isFailureReply(tc.content), tc.content)
		})
	}
}
