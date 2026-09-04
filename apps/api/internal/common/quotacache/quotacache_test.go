package quotacache

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTokenKeyHashesTheRawKey guards the invariant that makes this key safe to
// store: the raw API key must never reach Redis. It is also the compatibility
// fence for the extraction — if the derivation changes, every already-cached
// token balance is orphaned and silently re-reserved from the database.
func TestTokenKeyHashesTheRawKey(t *testing.T) {
	const raw = "sk-quotacache-fixture"

	key := TokenKey(raw)

	require.Equal(t, "token:"+common.GenerateHMAC(raw), key)
	assert.NotContains(t, key, raw, "raw API key must not appear in the cache key")
}

// TestUserKeyIsIDScoped pins the user key shape, which the guarded Lua scripts
// address by id.
func TestUserKeyIsIDScoped(t *testing.T) {
	assert.Equal(t, "user:42", UserKey(42))
}

// TestResultFromLuaMapsScriptReturns covers the contract between the Lua
// scripts and callers: 1 applied, 0 refused for insufficient balance, and
// anything else (the -1 guard trip) a miss that must fall back to the database.
// Collapsing Insufficient into Miss would let a caller reserve quota the user
// does not have.
func TestResultFromLuaMapsScriptReturns(t *testing.T) {
	for _, tc := range []struct {
		name   string
		raw    int
		expect Result
	}{
		{"applied", 1, OK},
		{"insufficient balance", 0, Insufficient},
		{"guard tripped", -1, Miss},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resultFromLua(tc.raw, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.expect, got)
		})
	}
}

// TestResultFromLuaTreatsErrorsAsMiss asserts a Redis failure degrades to the
// database path rather than being read as a successful reservation.
func TestResultFromLuaTreatsErrorsAsMiss(t *testing.T) {
	got, err := resultFromLua(1, assert.AnError)

	require.Error(t, err)
	assert.Equal(t, Miss, got)
}
