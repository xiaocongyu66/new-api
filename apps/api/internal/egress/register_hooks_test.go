package egress

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// identity builds absolute password-reset links and derives Passkey Origins from
// ServerAddress through identity.OnResolveServerAddress. The call sites tolerate
// an empty value, so a lost registration produces relative links instead of a
// failure.
func TestServerAddressHookIsRegistered(t *testing.T) {
	require.NotNil(t, identity.OnResolveServerAddress,
		"identity would fall back to its mirrored default instead of the configured address")
	assert.Equal(t, ServerAddress, identity.OnResolveServerAddress())
}

// identity mirrors this default so that a binary which does not link egress
// still produces absolute links. If this package's default changes, that mirror
// has to change with it.
func TestIdentityMirrorsServerAddressDefault(t *testing.T) {
	assert.Equal(t, "http://localhost:3000", identity.DefaultServerAddress,
		"identity.DefaultServerAddress must match this package's initial ServerAddress")
}
