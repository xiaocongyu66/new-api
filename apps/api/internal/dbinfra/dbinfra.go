// Package dbinfra owns the database bootstrap: connection setup, dialect
// columns, GORM logging, AutoMigrate, and the persisted option store.
package dbinfra

import "errors"

// Redemption errors that still live in this package.
// All database/auth/email/token/2FA sentinels are the identity domain's single
// source of truth and are re-exported from identity in forward_identity.go.

var ErrRedeemFailed = errors.New("redeem.failed")
