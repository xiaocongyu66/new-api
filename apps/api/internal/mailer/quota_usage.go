// Package mailer owns the persisted state of the outbound mail path: the
// per-account daily quota rows. It lives outside package common because common
// cannot import dbx (dbx imports common), and dbx deliberately holds no records.
package mailer

import (
	"time"

	"github.com/QuantumNous/new-api/internal/common/dbx"
)

// SMTPQuotaUsage records how many messages one SMTP account delivered on a
// given calendar day (UTC+8). Persisting the count is what keeps a daily quota
// meaningful across restarts, which an in-memory counter cannot do.
type SMTPQuotaUsage struct {
	Account   string    `gorm:"primaryKey;column:account"`
	Day       string    `gorm:"primaryKey;column:day"`
	Used      int       `gorm:"column:used"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (SMTPQuotaUsage) TableName() string {
	return "smtp_quota_usage"
}

func init() {
	dbx.RegisterMigrations(dbx.Migration{Model: &SMTPQuotaUsage{}, Name: "SMTPQuotaUsage"})
}
