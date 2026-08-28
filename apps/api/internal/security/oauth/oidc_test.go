package oauth

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/security/oidc"
	"github.com/stretchr/testify/assert"
)

func TestOIDCProvider_GetName(t *testing.T) {
	settings := oidc.GetOIDCSettings()
	originalDisplayName := settings.DisplayName
	defer func() { settings.DisplayName = originalDisplayName }()

	p := &OIDCProvider{}

	settings.DisplayName = ""
	assert.Equal(t, "OIDC", p.GetName())

	settings.DisplayName = "  Acme SSO  "
	assert.Equal(t, "Acme SSO", p.GetName())
}
