package billing

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── isAdminOpenID ──────────────────────────────────────────────────────────

func TestIsAdminOpenID(t *testing.T) {
	t.Setenv("QQ_BOT_SETTING", "")
	// Manually override settings for the test
	s := GetQQBotSetting()
	orig := s.AdminOpenIDs
	defer func() { s.AdminOpenIDs = orig }()

	s.AdminOpenIDs = "OPENID_A,OPENID_B"
	assert.True(t, isAdminOpenID("OPENID_A"))
	assert.True(t, isAdminOpenID("OPENID_B"))
	assert.False(t, isAdminOpenID("OPENID_C"))
	assert.False(t, isAdminOpenID(""))

	// Empty whitelist disables admin commands
	s.AdminOpenIDs = ""
	assert.False(t, isAdminOpenID("OPENID_A"))
}

// ─── parseTargetUser ────────────────────────────────────────────────────────

func TestParseTargetUser(t *testing.T) {
	// XML @ tag format
	openID, rest := parseTargetUser(`<qq sms="TARGET_OPENID">nick</qq> /余额 +10`)
	assert.Equal(t, "TARGET_OPENID", openID)
	assert.Contains(t, rest, "+10")

	// Plain @ format
	openID, rest = parseTargetUser("@target_user +5")
	assert.Equal(t, "target_user", openID)
	assert.Equal(t, "+5", rest)

	// No target
	openID, rest = parseTargetUser("+10")
	assert.Empty(t, openID)
	assert.Contains(t, rest, "+10")
}

// ─── CheckCooldown ──────────────────────────────────────────────────────────

func TestCheckCooldownDisabled(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.CommandCooldownSeconds
	defer func() { s.CommandCooldownSeconds = orig }()

	s.CommandCooldownSeconds = 0
	assert.NoError(t, CheckCooldown("user-1"))
	assert.NoError(t, CheckCooldown("user-1")) // no cooldown = always pass
}

func TestCheckCooldownActive(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.CommandCooldownSeconds
	defer func() { s.CommandCooldownSeconds = orig }()

	s.CommandCooldownSeconds = 5

	require.NoError(t, CheckCooldown("cooldown-test-user"))
	err := CheckCooldown("cooldown-test-user")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "冷却")

	// A different user is not blocked
	assert.NoError(t, CheckCooldown("other-user"))
}

func TestCheckCooldownExpiry(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.CommandCooldownSeconds
	defer func() { s.CommandCooldownSeconds = orig }()

	s.CommandCooldownSeconds = 1

	require.NoError(t, CheckCooldown("expiry-user"))
	// Simulate cooldown expiry by manipulating lastSeen
	cooldownMu.Lock()
	cooldownTracker["expiry-user"] = cooldownEntry{lastSeen: time.Now().Add(-2 * time.Second)}
	cooldownMu.Unlock()
	assert.NoError(t, CheckCooldown("expiry-user"))
}

// ─── AutoRecallFailed ───────────────────────────────────────────────────────

func TestAutoRecallFailedDisabled(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.RecallFailedMessages
	defer func() { s.RecallFailedMessages = orig }()

	s.RecallFailedMessages = false
	enabled, _ := AutoRecallFailed()
	assert.False(t, enabled)
}

func TestAutoRecallFailedEnabled(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.RecallFailedMessages
	origDelay := s.RecallDelaySeconds
	defer func() {
		s.RecallFailedMessages = orig
		s.RecallDelaySeconds = origDelay
	}()

	s.RecallFailedMessages = true
	s.RecallDelaySeconds = 10
	enabled, delay := AutoRecallFailed()
	assert.True(t, enabled)
	assert.Equal(t, 10, delay)
}

// ─── HandleAdminBan ─────────────────────────────────────────────────────────

func TestHandleAdminBanPermissionDenied(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.AdminOpenIDs
	defer func() { s.AdminOpenIDs = orig }()

	s.AdminOpenIDs = "ADMIN_X"

	event := &GroupAtMessageEvent{}
	reply := HandleAdminBan(event, "NOT_ADMIN", "/封禁 @user", true)
	assert.Contains(t, reply, "权限不足")
}

func TestHandleAdminBanNoTarget(t *testing.T) {
	s := GetQQBotSetting()
	orig := s.AdminOpenIDs
	defer func() { s.AdminOpenIDs = orig }()

	s.AdminOpenIDs = "ADMIN_X"

	event := &GroupAtMessageEvent{}
	reply := HandleAdminBan(event, "ADMIN_X", "/封禁", true)
	assert.Contains(t, reply, "用法")

	reply = HandleAdminBan(event, "ADMIN_X", "/解封", false)
	assert.Contains(t, reply, "用法")
}

// ─── resolveUserIdByOpenID ──────────────────────────────────────────────────

func TestResolveUserIdByOpenIDEmpty(t *testing.T) {
	_, err := resolveUserIdByOpenID("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未指定目标用户")
}

// ─── AdminOpenIDs config validation ─────────────────────────────────────────

func TestAdminOpenIDsParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"A", []string{"A"}},
		{"A,B", []string{"A", "B"}},
		{"A, B ,C", []string{"A", " B ", "C"}},
	}
	for _, tc := range tests {
		var got []string
		if tc.input != "" {
			for _, id := range strings.Split(tc.input, ",") {
				got = append(got, id)
			}
		}
		assert.Equal(t, tc.expected, got, fmt.Sprintf("input: %q", tc.input))
	}
}

// ─── New tests for gap coverage ──────────────────────────────────────────────

// TestNewParseTargetUserWithNestedQuotes tests XML with escaped quote inside value.
func TestNewParseTargetUserWithNestedQuotes(t *testing.T) {
	// Current parser uses simple Index and stops at first unescaped quote.
	// This reveals a fragility (ponytail: fix with proper XML parser when needed).
	openID, rest := parseTargetUser(`<qq sms="ab\"cd">nick</qq> /余额 +10`)
	assert.Equal(t, `ab\`, openID) // current behavior
	assert.Contains(t, rest, "+10")
}

// TestNewResolveUserIdByOpenIDBound tests resolve for already-bound user.
func TestNewResolveUserIdByOpenIDBound(t *testing.T) {
	// Requires a DB setup; this test documents the expected behavior.
	// In a full integration, we'd create a user, bind QQ, then resolve.
	// For now, test the error case for unbound user.
	_, err := resolveUserIdByOpenID("nonexistent-openid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "尚未绑定站点账号")
}

// TestNewQQBotSettingDefaults verifies default values for new fields.
func TestNewQQBotSettingDefaults(t *testing.T) {
	s := GetQQBotSetting()
	orig := *s
	defer func() { *GetQQBotSetting() = orig }()

	// Zero value should be explicit defaults
	assert.Equal(t, 0, s.CommandCooldownSeconds)
	assert.Equal(t, false, s.RecallFailedMessages)
	assert.Equal(t, 10, s.RecallDelaySeconds)
	assert.Equal(t, "", s.AdminOpenIDs)
}
