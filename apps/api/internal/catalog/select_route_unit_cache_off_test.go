package channel

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSelectRouteUnitCacheOffResolvesChannelFromDB is the regression guard for
// the routing defect where a bare binary + SQLite deployment with
// MEMORY_CACHE_ENABLED unset (default false) returned 503 model_not_found for
// every request. InitChannelCache early-returns when the memory cache is off,
// so channelsIDM is never built; the request-path filter and the final channel
// resolution must then read the channel from the database instead of the
// missing index. This test pins that: with MemoryCacheEnabled=false and
// channelsIDM left nil, the granted channel is resolved from the DB while a
// group-isolated (non-granted) channel is never handed out.
func TestSelectRouteUnitCacheOffResolvesChannelFromDB(t *testing.T) {
	const alias = "cache-off-model"
	db := withGroupIsolationRouteDB(t)

	// One channel granted to "default", one granted only to "vip".
	seedGroupIsolationChannel(t, db, 9101, []string{"default"}, alias)
	seedGroupIsolationChannel(t, db, 9102, []string{"vip"}, alias)

	common.MemoryCacheEnabled = false
	// Leave the memory index unbuilt: this is the real cache-off state
	// InitChannelCache produces (it early-returns before assigning channelsIDM).
	channelSyncLock.Lock()
	channelsIDM = nil
	group2model2channels = nil
	alias2routes = nil
	channelSyncLock.Unlock()

	ClearUnitHealthCache()
	t.Cleanup(ClearUnitHealthCache)

	// The granted channel must be selectable despite the missing index. With a
	// single eligible route, SelectRouteUnit is deterministic for any rnd value.
	selected, err := SelectRouteUnit("default", alias, "", 0, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, selected, "cache-off path must resolve the granted channel from the DB")
	assert.Equal(t, 9101, selected.ChannelId, "the granted channel must be selected")
	assert.Equal(t, alias, selected.UpstreamModel)

	// A group-isolated (vip-only) channel must never be handed to default, and
	// an unknown group must select nothing (fail-closed isolation boundary).
	vipOnly, vipErr := SelectRouteUnit("default", alias, "", 0,
		map[RouteKey]bool{{ChannelId: 9101, KeyIndex: 0, Model: alias}: true}, nil)
	require.NoError(t, vipErr)
	assert.Nil(t, vipOnly, "excluding the only granted route must leave nothing selectable")
	assert.Nil(t, selectForUnknownGroup(t, alias), "an ungranted group must select nothing")

	// Sanity: the vip group still reaches its own granted channel.
	vipSelected, vipErr := SelectRouteUnit("vip", alias, "", 0, nil, nil)
	require.NoError(t, vipErr)
	require.NotNil(t, vipSelected, "vip must reach its granted channel")
	assert.Equal(t, 9102, vipSelected.ChannelId)
}

func selectForUnknownGroup(t *testing.T, alias string) *SelectedRoute {
	t.Helper()
	route, err := SelectRouteUnit("no-such-group", alias, "", 0, nil, nil)
	require.NoError(t, err)
	return route
}
