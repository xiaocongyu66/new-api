package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func resetSMTPQuotaState(t *testing.T) {
	t.Helper()
	smtpSendCursor.Store(0)
	require.NoError(t, smtpDailyQuotaCache().Purge())
}

func TestSendEmailPoolSkipsAccountThatReachedDailyLimit(t *testing.T) {
	first := newFakeSMTPServer(t)
	defer first.close()
	second := newFakeSMTPServer(t)
	defer second.close()
	withSMTPSettings(t)
	resetSMTPQuotaState(t)

	SMTPAccountPool = []SMTPAccountConfig{
		{Server: first.host, Port: first.port, Account: "a@example.com", Token: "secret", StartTLS: true, DailyLimit: 1},
		{Server: second.host, Port: second.port, Account: "b@example.com", Token: "secret", StartTLS: true},
	}
	SystemName = "New API"

	require.NoError(t, SendEmail("Verification", "receiver@example.com", "<p>1</p>"))
	select {
	case message := <-first.messages:
		require.Contains(t, message, "From: New API <a@example.com>")
	case <-time.After(2 * time.Second):
		t.Fatal("first send did not use the account with remaining quota")
	}

	// The first account is now at its daily limit, so the second send must move on.
	require.NoError(t, SendEmail("Verification", "receiver@example.com", "<p>2</p>"))
	select {
	case message := <-second.messages:
		require.Contains(t, message, "From: New API <b@example.com>")
	case <-time.After(2 * time.Second):
		t.Fatal("second send did not skip the account that hit its daily limit")
	}
}

func TestSendEmailPoolUsesLowerPriorityTierWhenTopTierIsExhausted(t *testing.T) {
	primary := newFakeSMTPServer(t)
	defer primary.close()
	fallback := newFakeSMTPServer(t)
	defer fallback.close()
	withSMTPSettings(t)
	resetSMTPQuotaState(t)

	SMTPAccountPool = []SMTPAccountConfig{
		{
			Server: primary.host, Port: primary.port,
			Account: "primary@example.com", Token: "secret", StartTLS: true,
			DailyLimit: 1, Priority: 0,
		},
		{
			Server: fallback.host, Port: fallback.port,
			Account: "fallback@example.com", Token: "secret", StartTLS: true,
			Priority: 1,
		},
	}
	SystemName = "New API"

	require.NoError(t, SendEmail("Verification", "receiver@example.com", "<p>1</p>"))
	select {
	case message := <-primary.messages:
		require.Contains(t, message, "From: New API <primary@example.com>")
	case <-time.After(2 * time.Second):
		t.Fatal("first send did not use the primary tier")
	}

	require.NoError(t, SendEmail("Verification", "receiver@example.com", "<p>2</p>"))
	select {
	case message := <-fallback.messages:
		require.Contains(t, message, "From: New API <fallback@example.com>")
	case <-time.After(2 * time.Second):
		t.Fatal("send did not fall through to the lower priority tier")
	}
}

func TestSendEmailPoolPrefersLowerPriorityTierOverArrayOrder(t *testing.T) {
	lower := newFakeSMTPServer(t)
	defer lower.close()
	withSMTPSettings(t)
	resetSMTPQuotaState(t)

	// The higher-priority account is listed second on purpose.
	SMTPAccountPool = []SMTPAccountConfig{
		{Server: "127.0.0.1", Port: 1, Account: "unused@example.com", Token: "secret", Priority: 5},
		{Server: lower.host, Port: lower.port, Account: "preferred@example.com", Token: "secret", StartTLS: true, Priority: 0},
	}
	SystemName = "New API"

	require.NoError(t, SendEmail("Verification", "receiver@example.com", "<p>1</p>"))
	select {
	case message := <-lower.messages:
		require.Contains(t, message, "From: New API <preferred@example.com>")
	case <-time.After(2 * time.Second):
		t.Fatal("send did not prefer the lowest priority tier")
	}
}

func TestSendEmailPoolFailsWhenEveryAccountIsOverDailyLimit(t *testing.T) {
	withSMTPSettings(t)
	resetSMTPQuotaState(t)

	SMTPAccountPool = []SMTPAccountConfig{
		{Server: "127.0.0.1", Port: 1, Account: "a@example.com", Token: "secret", DailyLimit: 1},
	}
	SystemName = "New API"

	require.NoError(t, smtpDailyQuotaCache().SetWithTTL(smtpQuotaKey("a@example.com"), 1, smtpQuotaTTL))

	err := SendEmail("Verification", "receiver@example.com", "<p>1</p>")
	require.Error(t, err)
	require.Contains(t, err.Error(), "额度")
}

func TestSMTPQuotaIsScopedToTheAccountAndTheDay(t *testing.T) {
	resetSMTPQuotaState(t)

	account := SMTPAccountConfig{Account: "a@example.com", DailyLimit: 2}
	require.True(t, smtpAccountHasQuota(account))

	smtpRecordQuotaUse(account)
	used, err := smtpQuotaUsed(account)
	require.NoError(t, err)
	require.Equal(t, 1, used)

	// A different account has its own counter, so it is untouched.
	other := SMTPAccountConfig{Account: "b@example.com", DailyLimit: 2}
	used, err = smtpQuotaUsed(other)
	require.NoError(t, err)
	require.Equal(t, 0, used)

	// An unlimited account is never blocked.
	require.True(t, smtpAccountHasQuota(SMTPAccountConfig{Account: "a@example.com"}))
}

func TestSMTPSendOrderRotatesWithinThePreferredTier(t *testing.T) {
	resetSMTPQuotaState(t)

	accounts := []SMTPAccountConfig{
		{Account: "a@example.com", Priority: 0},
		{Account: "b@example.com", Priority: 0},
		{Account: "c@example.com", Priority: 1},
	}

	first := smtpSendOrder(accounts)
	second := smtpSendOrder(accounts)

	require.Len(t, first, 3)
	require.Equal(t, "a@example.com", first[0].Account)
	require.Equal(t, "b@example.com", first[1].Account)
	require.Equal(t, "c@example.com", first[2].Account)

	require.Equal(t, "b@example.com", second[0].Account)
	require.Equal(t, "a@example.com", second[1].Account)
	require.Equal(t, "c@example.com", second[2].Account)
}

func TestSMTPSendOrderDropsAccountsOverTheirDailyLimit(t *testing.T) {
	resetSMTPQuotaState(t)

	accounts := []SMTPAccountConfig{
		{Account: "a@example.com", Priority: 0, DailyLimit: 1},
		{Account: "b@example.com", Priority: 0, DailyLimit: 1},
	}
	require.NoError(t, smtpDailyQuotaCache().SetWithTTL(smtpQuotaKey("a@example.com"), 1, smtpQuotaTTL))

	ordered := smtpSendOrder(accounts)
	require.Len(t, ordered, 1)
	require.Equal(t, "b@example.com", ordered[0].Account)

	require.NoError(t, smtpDailyQuotaCache().SetWithTTL(smtpQuotaKey("b@example.com"), 1, smtpQuotaTTL))
	require.Nil(t, smtpSendOrder(accounts))
}
