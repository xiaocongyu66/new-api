package model

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// withAbilityDB installs an in-memory SQLite database holding the given
// abilities/channels, so the non-memory-cache selection path (GetChannel in
// ability.go) can be driven for real rather than approximated.
//
// This closes the test gap code-review-graph reported for GetChannel: #348
// changed its weight formula from `Weight + 10` to the shared routingBaseWeight,
// and nothing covered that path.
func withAbilityDB(t *testing.T, group, modelName string, rows []abilityFixture) {
	t.Helper()

	previousDB := DB
	prevIDM := channelsIDM
	prevAliasRoutes := group2alias2routes
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Ability{}, &Channel{}, &ChannelModelRoute{}))
	DB = db
	t.Cleanup(func() {
		DB = previousDB
		channelSyncLock.Lock()
		channelsIDM = prevIDM
		group2alias2routes = prevAliasRoutes
		channelSyncLock.Unlock()
	})

	idm := make(map[int]*Channel)
	aliasRoutes := make(map[string]map[string][]routeCandidate)
	for i := range rows {
		ch := &Channel{
			Id:     rows[i].ability.ChannelId,
			Type:   constant.ChannelTypeOpenAI,
			Key:    fmt.Sprintf("key-%d", rows[i].ability.ChannelId),
			Status: common.ChannelStatusEnabled,
			Name:   fmt.Sprintf("channel-%d", rows[i].ability.ChannelId),
			Models: rows[i].ability.Model,
			Group:  rows[i].ability.Group,
		}
		require.NoError(t, db.Create(ch).Error)
		require.NoError(t, db.Create(&rows[i].ability).Error)
		idm[ch.Id] = ch
		// The route unit row is what selection reads; abilities no longer carry a
		// weight of their own.
		require.NoError(t, db.Create(&ChannelModelRoute{
			Group:            group,
			PublicModelAlias: modelName,
			ChannelId:        rows[i].ability.ChannelId,
			KeyIndex:         0,
			UpstreamModel:    modelName,
			StaticWeight:     rows[i].staticWeight,
			Enabled:          true,
		}).Error)
		// Build memory cache for route candidates
		if aliasRoutes[group] == nil {
			aliasRoutes[group] = make(map[string][]routeCandidate)
		}
		aliasRoutes[group][modelName] = append(aliasRoutes[group][modelName], routeCandidate{
			routeId:       ch.Id,
			channelId:     ch.Id,
			keyIndex:      0,
			upstreamModel: modelName,
			staticWeight:  rows[i].staticWeight,
		})
	}

	channelSyncLock.Lock()
	channelsIDM = idm
	group2alias2routes = aliasRoutes
	channelSyncLock.Unlock()
}

// abilityFixture pairs an ability row with the route unit weight the selector
// should see for it. Weight used to live on the ability itself; scheduling now
// reads channel_model_routes.static_weight, so the test states it there.
type abilityFixture struct {
	ability      Ability
	staticWeight int
}

func ability(channelID int, group, modelName string, staticWeight int) abilityFixture {
	return abilityFixture{
		ability: Ability{
			Group:     group,
			Model:     modelName,
			ChannelId: channelID,
			Enabled:   true,
		},
		staticWeight: staticWeight,
	}
}

// TestGetChannelUsesSharedWeightFormula pins the DB path onto the same weight
// curve as the memory-cache path. Before #348 this path used `Weight + 10` while
// the memory path amplified sub-10 weights by 100, so flipping
// MEMORY_CACHE_ENABLED silently changed traffic distribution by up to ~47x for
// identical configuration.
func TestGetChannelUsesSharedWeightFormula(t *testing.T) {
	const group, modelName = "db-group", "db-model"

	// Route unit static weight is the only thing deciding the split now.
	withAbilityDB(t, group, modelName, []abilityFixture{
		ability(9501, group, modelName, 30),
		ability(9502, group, modelName, 1),
	})

	ClearRouteHealthCache()

	counts := map[int]int{}
	for range 600 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		counts[got.ChannelId]++
	}

	// routingBaseWeight maps 30 -> 31 and 1 -> 2, so the heavy channel should take
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

	withAbilityDB(t, group, modelName, []abilityFixture{
		ability(9601, group, modelName, 10),
		ability(9602, group, modelName, 10),
	})

	ClearRouteHealthCache()

	for range 50 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", map[RouteKey]bool{{ChannelId: 9601, KeyIndex: 0, Model: modelName}: true})
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 9602, got.ChannelId, "the excluded channel must never be returned")
	}

	// Excluding every candidate yields no channel rather than a stale pick.
	got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", map[RouteKey]bool{{ChannelId: 9601, KeyIndex: 0, Model: modelName}: true, {ChannelId: 9602, KeyIndex: 0, Model: modelName}: true})
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

	withAbilityDB(t, group, modelName, []abilityFixture{
		ability(9701, group, modelName, 10),
		ability(9702, group, modelName, 10),
	})
	require.NoError(t, DB.AutoMigrate(&ChannelModelHealth{}))
	ClearRouteHealthCache()
	t.Cleanup(ClearRouteHealthCache)
	// The selectors read the real clock, so the isolation window must be live.
	now := time.Now()
	require.NoError(t, RecordRetryableFailure(RouteKey{ChannelId: 9702, KeyIndex: 0, Model: modelName}, "bad_response", FailureSourceUpstream, now))

	derated := map[int]int{}
	for range 400 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		derated[got.ChannelId]++
	}
	assert.Greater(t, derated[9701], derated[9702],
		"the healthy peer must dominate while the calm route keeps a reduced share")

	// A disabled route is the one state that leaves the candidate set.
	require.NoError(t, DisableRoute(RouteKey{ChannelId: 9702, KeyIndex: 0, Model: modelName}, now))
	for range 50 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 9701, got.ChannelId, "a disabled route must never be selected")
	}

	// Admin recovery clears the ladder, so the route competes at full weight again.
	require.NoError(t, RecoverRoute(RouteKey{ChannelId: 9702, KeyIndex: 0, Model: modelName}, now))
	counts := map[int]int{}
	for range 600 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		counts[got.ChannelId]++
	}
	assert.Positive(t, counts[9702], "a recovered route is selectable again")
}
