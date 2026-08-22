// Package identity owns the user, token, and session business rules. It holds
// use-case and topic files rather than a controller/service/model split, and it
// depends on the transport contract instead of an HTTP framework.
package identity

import "strings"

// ParseBearerCredential extracts the raw credential from an Authorization header.
//
// It accepts either a bare credential or a `Bearer <credential>` pair, matching
// the schemes existing clients send: dashboard JWTs, personal access tokens, and
// relay API keys all arrive through this header. Anything else is rejected so a
// malformed header can never be interpreted as a valid credential.
//
// The scheme comparison is case-insensitive because HTTP auth schemes are
// case-insensitive per RFC 7235.
func ParseBearerCredential(header string) (string, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", false
	}

	parts := strings.Fields(header)
	switch {
	case len(parts) == 2 && strings.EqualFold(parts[0], "Bearer"):
		header = parts[1]
	case len(parts) != 1:
		return "", false
	}

	return header, header != ""
}
