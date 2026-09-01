package conformance

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runValuesCases(t *testing.T, adapter Adapter) {
	t.Helper()

	// RoundTrip asserts per-request state set by middleware is readable through
	// the typed getters business code relies on.
	t.Run("RoundTrip", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
		adapted, _, _ := adapter.NewContext(req)

		adapted.Set("id", 42)
		adapted.Set("username", "alice")
		adapted.Set("use_access_token", true)
		adapted.Set("channel_id_64", int64(9))

		assert.Equal(t, 42, adapted.GetInt("id"))
		assert.Equal(t, "alice", adapted.GetString("username"))
		assert.True(t, adapted.GetBool("use_access_token"))
		assert.Equal(t, int64(9), adapted.GetInt64("channel_id_64"))

		value, exists := adapted.Get("username")
		require.True(t, exists)
		assert.Equal(t, "alice", value)

		_, missing := adapted.Get("not_set")
		assert.False(t, missing)
	})

	// CompositeGettersRoundTrip covers the getters middleware uses for the
	// grouped state (token group lists, cached request metadata). They are
	// separate from the scalar case because a framework may back them with a
	// different conversion path.
	t.Run("CompositeGettersRoundTrip", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
		adapted, _, _ := adapter.NewContext(req)

		issued := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
		adapted.Set("token_meta", map[string]any{"id": 7, "name": "probe"})
		adapted.Set("auto_groups", []string{"default", "vip"})
		adapted.Set("issued_at", issued)

		assert.Equal(t, map[string]any{"id": 7, "name": "probe"}, adapted.GetStringMap("token_meta"))
		assert.Equal(t, []string{"default", "vip"}, adapted.GetStringSlice("auto_groups"))
		assert.Equal(t, issued, adapted.GetTime("issued_at"))
	})

	// TypedGettersReturnZeroForAbsentKeys asserts the getters middleware calls
	// before a producer has run yield zero values instead of panicking. The
	// request pipeline reads optional state unconditionally, so a panic here
	// would be a 500 on every request that skipped the producing middleware.
	t.Run("TypedGettersReturnZeroForAbsentKeys", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/user/self", nil)
		adapted, _, _ := adapter.NewContext(req)

		assert.Equal(t, "", adapted.GetString("absent"))
		assert.Equal(t, 0, adapted.GetInt("absent"))
		assert.Equal(t, int64(0), adapted.GetInt64("absent"))
		assert.False(t, adapted.GetBool("absent"))
		assert.Nil(t, adapted.GetStringMap("absent"))
		assert.Nil(t, adapted.GetStringSlice("absent"))
		assert.True(t, adapted.GetTime("absent").IsZero())
	})

	// OverwriteWins asserts the last write is what readers observe. Middleware
	// reassigns the selected channel across retry attempts, so a first-write
	// -wins store would forward a retry to the failed channel.
	t.Run("OverwriteWins", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		adapted, _, _ := adapter.NewContext(req)

		adapted.Set("channel_id", 1)
		adapted.Set("channel_id", 2)

		assert.Equal(t, 2, adapted.GetInt("channel_id"))
	})
}
