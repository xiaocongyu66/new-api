package identity

import "github.com/QuantumNous/new-api/model"

// Errors this domain returns to its callers, re-exported so callers match on
// identity.ErrX instead of reaching into the record package.
//
// These are aliases, not copies. Declaring fresh errors.New values here would
// produce two distinct sentinels for the same condition, and errors.Is would
// silently stop matching across the boundary — a failure with no compile error
// behind it. The single definition stays with the records until they move into
// this package, at which point these become the definitions.
var (
	// ErrInvalidCredentials and ErrUserEmptyCredentials distinguish a wrong
	// password from a request that supplied none, so a caller can answer
	// "invalid" without revealing which half was missing.
	ErrInvalidCredentials   = model.ErrInvalidCredentials
	ErrUserEmptyCredentials = model.ErrUserEmptyCredentials

	// Email lookups have three distinct outcomes; collapsing them would either
	// leak account existence or silently bind the wrong user.
	ErrEmailAlreadyTaken = model.ErrEmailAlreadyTaken
	ErrEmailNotFound     = model.ErrEmailNotFound
	ErrEmailAmbiguous    = model.ErrEmailAmbiguous

	// Token credential errors.
	ErrTokenNotProvided = model.ErrTokenNotProvided
	ErrTokenInvalid     = model.ErrTokenInvalid

	// Two-factor enrolment state errors.
	ErrTwoFANotEnabled     = model.ErrTwoFANotEnabled
	ErrTwoFAAlreadyEnabled = model.ErrTwoFAAlreadyEnabled
)
