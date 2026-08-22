package identity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseBearerCredential pins the credential-parsing rule shared by dashboard
// JWT, personal access token, and relay API key authentication. A header that is
// accepted here reaches credential validation, so over-accepting would widen the
// authentication surface.
func TestParseBearerCredential(t *testing.T) {
	for _, tc := range []struct {
		name       string
		header     string
		wantToken  string
		wantParsed bool
	}{
		{name: "bearer prefixed", header: "Bearer sk-abc123", wantToken: "sk-abc123", wantParsed: true},
		{name: "lowercase scheme", header: "bearer sk-abc123", wantToken: "sk-abc123", wantParsed: true},
		{name: "mixed case scheme", header: "BeArEr sk-abc123", wantToken: "sk-abc123", wantParsed: true},
		{name: "bare credential", header: "sk-abc123", wantToken: "sk-abc123", wantParsed: true},
		{name: "surrounding whitespace", header: "   Bearer sk-abc123   ", wantToken: "sk-abc123", wantParsed: true},
		{name: "empty header", header: "", wantToken: "", wantParsed: false},
		{name: "whitespace only", header: "    ", wantToken: "", wantParsed: false},
		{name: "scheme without credential", header: "Bearer", wantToken: "Bearer", wantParsed: true},
		{name: "bearer with blank credential", header: "Bearer    ", wantToken: "Bearer", wantParsed: true},
		{name: "too many parts", header: "Bearer sk-abc123 extra", wantToken: "", wantParsed: false},
		{name: "unsupported scheme with credential", header: "Basic dXNlcjpwYXNz", wantToken: "", wantParsed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, parsed := ParseBearerCredential(tc.header)

			assert.Equal(t, tc.wantParsed, parsed)
			assert.Equal(t, tc.wantToken, token)
		})
	}
}
