package common

import "fmt"

// SMTPQuotaStore persists the per-account daily send counts behind the SMTP
// account pool.
//
// The implementation lives in package mailer, which cannot be imported here:
// it needs the database handle owned by dbx, and dbx imports this package. The
// dependency is therefore inverted — mailer installs itself through
// SetSMTPQuotaStore during startup.
type SMTPQuotaStore interface {
	// UsedToday reports how many sends the account already made today.
	UsedToday(account string) (int, error)
	// RecordUse charges one successful send against the account.
	RecordUse(account string) error
}

var smtpQuotaStore SMTPQuotaStore

// SetSMTPQuotaStore installs the quota persistence implementation. Called once
// during startup; without it quota counting stays disabled rather than failing
// a send.
func SetSMTPQuotaStore(store SMTPQuotaStore) {
	smtpQuotaStore = store
}

// smtpQuotaUsedToday reads the account's send count for today from the
// persisted quota store, which survives restarts.
func smtpQuotaUsedToday(account string) (int, error) {
	if smtpQuotaStore == nil {
		return 0, nil
	}
	used, err := smtpQuotaStore.UsedToday(account)
	if err != nil {
		return 0, fmt.Errorf("failed to read SMTP quota for %s: %w", account, err)
	}
	return used, nil
}

// smtpRecordQuotaUse charges one successful send against the account. Accounts
// without a daily limit are not counted, so the unlimited path costs no
// database write.
func smtpRecordQuotaUse(account string, dailyLimit int) error {
	if dailyLimit <= 0 || smtpQuotaStore == nil {
		return nil
	}
	if err := smtpQuotaStore.RecordUse(account); err != nil {
		return fmt.Errorf("failed to record SMTP quota for %s: %w", account, err)
	}
	return nil
}
