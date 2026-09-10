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
