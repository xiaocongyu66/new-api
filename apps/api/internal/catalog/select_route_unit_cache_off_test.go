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
// channelsIDM left nil, the granted channel is still selectable while a
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

	// The granted channel must be selectable despite the missing index.
	seen := selectManyChannelIDs(t, "default", alias, 60)
	require.NotEmpty(t, seen, "the cache-off path must still resolve a channel from the DB")
	assert.Contains(t, seen, 9101, "the granted channel must be selectable")
	assert.NotContains(t, seen, 9102, "a group-isolated channel must never be handed to default")
	assert.NotContains(t, seen, 0, "the nil index must not collapse every candidate to no route")

	// Sanity: the vip group still reaches its own channel.
	vipSeen := selectManyChannelIDs(t, "vip", alias, 60)
	assert.Contains(t, vipSeen, 9102, "vip must reach its granted channel")
}
