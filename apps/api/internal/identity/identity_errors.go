package identity

import "errors"

// Identity-domain sentinel errors.
var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrUserEmptyCredentials = errors.New("empty credentials")
	ErrEmailAlreadyTaken    = errors.New("email already taken")
	ErrEmailNotFound        = errors.New("email not found")
	ErrEmailAmbiguous       = errors.New("email matches multiple users")
	ErrTokenNotProvided     = errors.New("token not provided")
	ErrTokenInvalid         = errors.New("token invalid")
	ErrTwoFANotEnabled      = errors.New("2fa not enabled")
	ErrTwoFAAlreadyEnabled  = errors.New("2fa already enabled")
	ErrDatabase             = errors.New("database error")
)
