package model

import (
	"fmt"
	"testing"

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
func withAbilityDB(t *testing.T, group, modelName string, rows []Ability) {
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
			Id:       rows[i].ChannelId,
			Type:     constant.ChannelTypeOpenAI,
			Key:      fmt.Sprintf("key-%d", rows[i].ChannelId),
			Status:   common.ChannelStatusEnabled,
			Name:     fmt.Sprintf("channel-%d", rows[i].ChannelId),
			Weight:   &rows[i].Weight,
			Models:   rows[i].Model,
			Group:    rows[i].Group,
			Priority: rows[i].Priority,
		}
		require.NoError(t, db.Create(ch).Error)
		require.NoError(t, db.Create(&rows[i]).Error)
		idm[ch.Id] = ch
		// Create ChannelModelRoute entry for the new selector
		require.NoError(t, db.Create(&ChannelModelRoute{
			Group:            group,
			PublicModelAlias: modelName,
			ChannelId:        rows[i].ChannelId,
			KeyIndex:         0,
			UpstreamModel:    modelName,
			StaticWeight:     int(rows[i].Weight),
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
			staticWeight:  int(rows[i].Weight),
		})
	}

	channelSyncLock.Lock()
	channelsIDM = idm
	group2alias2routes = aliasRoutes
	channelSyncLock.Unlock()
}

func ability(channelID int, group, modelName string, weight uint, priority int64) Ability {
	p := priority
	return Ability{
		Group:     group,
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   true,
		Priority:  &p,
		Weight:    weight,
	}
}

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

	resetHealthManager()
	setTestConfig(true, 0.3, 0.05, 0)

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

	withAbilityDB(t, group, modelName, []Ability{
		ability(9601, group, modelName, 10, 100),
		ability(9602, group, modelName, 10, 100),
	})

	resetHealthManager()
	setTestConfig(true, 0.3, 0.05, 0)

	for range 50 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", map[RouteKey]bool{{ChannelId: 9601, KeyIndex: 0}: true})
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 9602, got.ChannelId, "the excluded channel must never be returned")
	}

	// Excluding every candidate yields no channel rather than a stale pick.
	got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", map[RouteKey]bool{{ChannelId: 9601, KeyIndex: 0}: true, {ChannelId: 9602, KeyIndex: 0}: true})
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestGetChannelAppliesHealthScore proves the EWMA factor reaches the DB path,
// which is the whole point of routing weight being computed in one place.
func TestGetChannelAppliesHealthScore(t *testing.T) {
	const group, modelName = "db-group", "db-model"

	withAbilityDB(t, group, modelName, []Ability{
		ability(9701, group, modelName, 10, 100),
		ability(9702, group, modelName, 10, 100),
	})

	mgr := resetHealthManager()
	setTestConfig(true, 0.3, 0.05, 0)
	for range 50 {
		mgr.RecordChannelOutcome(9702, OutcomeFatal)
	}
	require.InDelta(t, 0.05, mgr.GetScore(9702), 1e-9)

	counts := map[int]int{}
	for range 600 {
		got, err := GetRandomSatisfiedChannel(group, modelName, 0, "", nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		counts[got.ChannelId]++
	}

	assert.Greater(t, counts[9701], counts[9702]*5,
		"the degraded channel must lose share on the DB path")
	assert.Positive(t, counts[9702],
		"but the MinScore floor keeps it selectable rather than locking it out")
}
