package model

import "errors"

// Redemption errors that still live in the model package.
// All database/auth/email/token/2FA sentinels are the identity domain's single
// source of truth and are re-exported from identity in identity_fns.go.

var ErrRedeemFailed = errors.New("redeem.failed")
