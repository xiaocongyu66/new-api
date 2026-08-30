package channel

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withAbilityDB and ability live in track_cooldown_test.go; both drive the same
// non-memory-cache selection path (GetChannel in store_ability.go).
//
// These tests close the gap code-review-graph reported for GetChannel: #348
// changed its weight formula from `Weight + 10` to the shared RoutingBaseWeight,
// and nothing covered that path.

// TestGetChannelUsesSharedWeightFormula pins the DB path onto the same weight
// curve as the memory-cache path. Before #348 this path used `Weight + 10` while
// the memory path amplified sub-10 weights by 100, so flipping
// MEMORY_CACHE_ENABLED silently changed traffic distribution by up to ~47x for
// identical configuration.
func TestGetChannelUsesSharedWeightFormula(t *testing.T) {
	const group, modelName = "db-group", "db-model"

	// Same tier so weighting, not priority, decides the split.
	withAbilityDB(t, group, modelName, []Ability{
		ability(9501, group, modelName, 30, 100),
		ability(9502, group, modelName, 1, 100),
	})

	ClearRouteHealthCache()

	counts := map[int]int{}
	for range 600 {
		got, err := GetChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		counts[got.Id]++
	}

	// RoutingBaseWeight maps 30 -> 31 and 1 -> 2, so the heavy channel should take
	// roughly 31/33 of traffic. The legacy `Weight + 10` curve would have made it
	// 40/51, and the legacy memory-path smoothing would have inverted the order
	// outright by scaling weight 1 up to 100.
	require.Positive(t, counts[9501])
	assert.Greater(t, counts[9501], counts[9502]*5,
		"weight 30 must dominate weight 1 on the DB path too")
}

// TestGetChannelRespectsExcludeSet: request-level exclusion must work on the DB
// path as well, otherwise a retry would reselect the channel that just failed
// whenever MEMORY_CACHE_ENABLED is off.
func TestGetChannelRespectsExcludeSet(t *testing.T) {
	const group, modelName = "db-group", "db-model"

	withAbilityDB(t, group, modelName, []Ability{
		ability(9601, group, modelName, 10, 100),
		ability(9602, group, modelName, 10, 100),
	})

	ClearRouteHealthCache()

	for range 50 {
		got, err := GetChannel(group, modelName, 0, "", map[int]bool{9601: true})
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 9602, got.Id, "the excluded channel must never be returned")
	}

	// Excluding every candidate yields no channel rather than a stale pick.
	got, err := GetChannel(group, modelName, 0, "", map[int]bool{9601: true, 9602: true})
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestGetChannelDeratesIsolatedRoutes proves the state machine reaches the DB
// selection path. Since Wave C an isolated route is derated rather than dropped:
// it keeps CalmWeightScale percent of its weight, so it still wins occasional
// picks (which is the natural half-open probe) while the healthy peer takes the
// clear majority. Only a disabled route leaves the pool entirely.
func TestGetChannelDeratesIsolatedRoutes(t *testing.T) {
	const group, modelName = "db-group", "db-model"

	withAbilityDB(t, group, modelName, []Ability{
		ability(9701, group, modelName, 10, 100),
		ability(9702, group, modelName, 10, 100),
	})
	require.NoError(t, dbx.DB.AutoMigrate(&ChannelModelHealth{}))
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)
	// The selectors read the real clock, so the isolation window must be live.
	now := time.Now()
	require.NoError(t, RecordRetryableFailure(RouteKey{ChannelId: 9702, Model: modelName}, "bad_response", FailureSourceUpstream, now))

	derated := map[int]int{}
	for range 400 {
		got, err := GetChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		derated[got.Id]++
	}
	assert.Greater(t, derated[9701], derated[9702],
		"the healthy peer must dominate while the calm route keeps a reduced share")

	// A disabled route is the one state that leaves the candidate set.
	require.NoError(t, DisableRoute(RouteKey{ChannelId: 9702, Model: modelName}, now))
	for range 50 {
		got, err := GetChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 9701, got.Id, "a disabled route must never be selected")
	}

	// Admin recovery clears the ladder, so the route competes at full weight again.
	require.NoError(t, RecoverRoute(RouteKey{ChannelId: 9702, Model: modelName}, now))
	counts := map[int]int{}
	for range 400 {
		got, err := GetChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		counts[got.Id]++
	}
	assert.Positive(t, counts[9702], "a recovered route is selectable again")
}
