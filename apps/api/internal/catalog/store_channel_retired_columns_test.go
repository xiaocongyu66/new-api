package channel

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/catalog/routestats"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// withRetiredColumnDB installs a database migrated from the current structs,
// which no longer declare channels.priority or channels.weight. That is exactly
// the schema a migrated deployment runs: DropLegacySchedulingColumns removes both
// columns, so any query still ordering by one fails with "no such column".
func withRetiredColumnDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := dbx.DB
	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dbx.InitColumns()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}, &ChannelModelRoute{}))
	dbx.DB = db

	t.Cleanup(func() {
		dbx.DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
		dbx.InitColumns()
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// TestSearchTagsOnSchemaWithoutLegacyColumns is the regression gate for the tag
// mode admin channel list. SearchTags used to default its subquery order to
// "priority desc"; that column is dropped by the migration, so on every migrated
// database the endpoint failed outright. The order is now the one the rest of the
// admin list already uses, so there is nothing left to order by that can vanish.
func TestSearchTagsOnSchemaWithoutLegacyColumns(t *testing.T) {
	db := withRetiredColumnDB(t)

	require.False(t, db.Migrator().HasColumn(&Channel{}, "priority"),
		"the fixture must reproduce the migrated schema: priority is retired")
	require.False(t, db.Migrator().HasColumn(&Channel{}, "weight"),
		"the fixture must reproduce the migrated schema: weight is retired")

	tagA, tagB := "team-a", "team-b"
	require.NoError(t, db.Create(&Channel{
		Id: 7301, Type: 1, Name: "tagged-alpha", Key: "sk-a", Models: "gpt-4",
		Group: "default", Tag: &tagA, Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&Channel{
		Id: 7302, Type: 1, Name: "tagged-beta", Key: "sk-b", Models: "gpt-4",
		Group: "default", Tag: &tagB, Status: common.ChannelStatusEnabled,
	}).Error)

	tags, err := SearchTags("tagged", "default", "gpt-4")
	require.NoError(t, err, "SearchTags must not order by a retired column")
	found := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if tag != nil {
			found[*tag] = true
		}
	}
	assert.True(t, found[tagA], "tag %q must be listed", tagA)
	assert.True(t, found[tagB], "tag %q must be listed", tagB)
}

// TestGetActiveRouteStatsPoolKeysWithoutMemoryCache is the regression gate for
// the hourly sweep. group2alias2routes is only built by InitChannelCache, which
// early-returns when the memory cache is disabled — the default. Returning nil
// there made SweepSharePools read "keep nothing" and delete every share pool
// once an hour, discarding all correction history.
func TestGetActiveRouteStatsPoolKeysWithoutMemoryCache(t *testing.T) {
	db := withRetiredColumnDB(t)

	prevMemoryCache := common.MemoryCacheEnabled
	prevAliasRoutes := group2alias2routes
	common.MemoryCacheEnabled = false
	channelSyncLock.Lock()
	group2alias2routes = nil
	channelSyncLock.Unlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCache
		channelSyncLock.Lock()
		group2alias2routes = prevAliasRoutes
		channelSyncLock.Unlock()
	})

	require.NoError(t, db.Create(&ChannelModelRoute{
		Id: 1, Group: "default", PublicModelAlias: "gpt-4", ChannelId: 7401,
		KeyIndex: 0, UpstreamModel: "gpt-4", StaticWeight: 100, Enabled: true,
	}).Error)
	require.NoError(t, db.Create(&ChannelModelRoute{
		Id: 2, Group: "vip", PublicModelAlias: "gpt-4", ChannelId: 7401,
		KeyIndex: 0, UpstreamModel: "gpt-4", StaticWeight: 100, Enabled: true,
	}).Error)
	require.NoError(t, db.Create(&ChannelModelRoute{
		Id: 3, Group: "default", PublicModelAlias: "retired", ChannelId: 7401,
		KeyIndex: 0, UpstreamModel: "retired", StaticWeight: 100, Enabled: false,
	}).Error)

	keep := GetActiveRouteStatsPoolKeys()
	require.NotNil(t, keep, "a live route table must never read as an unknown keep set")
	assert.Contains(t, keep, routestats.PoolKey{Group: "default", PublicModelAlias: "gpt-4"})
	assert.Contains(t, keep, routestats.PoolKey{Group: "vip", PublicModelAlias: "gpt-4"})
	assert.NotContains(t, keep, routestats.PoolKey{Group: "default", PublicModelAlias: "retired"},
		"a disabled route unit backs no live pool")

	// The consumer's half of the contract: a pool backed by a live route survives
	// the sweep that used to wipe it.
	routestats.ResetShares()
	t.Cleanup(routestats.ResetShares)
	cfg := routestats.DefaultRouteStatsSetting()
	live := routestats.PoolKey{Group: "default", PublicModelAlias: "gpt-4"}
	orphan := routestats.PoolKey{Group: "default", PublicModelAlias: "retired"}
	selected := routestats.RouteID{ChannelID: 7401, KeyIndex: 0, UpstreamModel: "gpt-4"}
	targets := map[routestats.RouteID]float64{selected: 1.0}
	routestats.RecordSelection(live, selected, targets, cfg)
	routestats.RecordSelection(orphan, selected, targets, cfg)
	require.Equal(t, 2, routestats.SharePoolCount())

	assert.Equal(t, 1, routestats.SweepSharePools(GetActiveRouteStatsPoolKeys()),
		"only the pool with no live route may be evicted")
	assert.Equal(t, 1, routestats.SharePoolCount())
	assert.Equal(t, 1, routestats.Corrections(live, targets, cfg)[selected].Opportunities,
		"the live pool must keep its share history")
}

// TestUpdateRouteUnitConfigInvalidatesSelectionIndex is the regression gate for
// the route-unit admin write. Selection reads group2alias2routes, which only
// InitChannelCache rebuilds, so an update that skipped invalidation returned 200
// while a disabled route kept serving until the next sync tick.
func TestUpdateRouteUnitConfigInvalidatesSelectionIndex(t *testing.T) {
	db := withRetiredColumnDB(t)

	prevMemoryCache := common.MemoryCacheEnabled
	prevGroups := group2model2channels
	prevIDM := channelsIDM
	prevAliasRoutes := group2alias2routes
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = prevMemoryCache
		channelSyncLock.Lock()
		group2model2channels = prevGroups
		channelsIDM = prevIDM
		group2alias2routes = prevAliasRoutes
		channelSyncLock.Unlock()
	})

	require.NoError(t, db.Create(&Channel{
		Id: 7501, Type: 1, Name: "route-unit-cache", Key: "sk-c", Models: "gpt-4",
		Group: "default", Status: common.ChannelStatusEnabled,
	}).Error)
	// InitChannelCache keys its group map off the abilities table, so a channel
	// with no ability row is invisible to the rebuild.
	require.NoError(t, db.Create(&Ability{
		Group: "default", Model: "gpt-4", ChannelId: 7501, Enabled: true,
	}).Error)
	require.NoError(t, db.Create(&ChannelModelRoute{
		Id: 11, Group: "default", PublicModelAlias: "gpt-4", ChannelId: 7501,
		KeyIndex: 0, UpstreamModel: "gpt-4", StaticWeight: 100, Enabled: true,
	}).Error)

	InitChannelCache()
	channelSyncLock.RLock()
	initial := getCandidatesFromCache("default", "gpt-4")
	channelSyncLock.RUnlock()
	require.Len(t, initial, 1, "the seeded route must be visible to selection")
	require.Equal(t, 100, initial[0].staticWeight)

	newWeight := 250
	require.NoError(t, UpdateRouteUnitConfig(11, &newWeight, nil))
	channelSyncLock.RLock()
	reweighted := getCandidatesFromCache("default", "gpt-4")
	channelSyncLock.RUnlock()
	require.Len(t, reweighted, 1)
	assert.Equal(t, 250, reweighted[0].staticWeight,
		"a weight change must reach the in-memory index without waiting for a sync tick")

	disabled := false
	require.NoError(t, UpdateRouteUnitConfig(11, nil, &disabled))
	channelSyncLock.RLock()
	afterDisable := getCandidatesFromCache("default", "gpt-4")
	channelSyncLock.RUnlock()
	assert.Empty(t, afterDisable,
		"a disabled route unit must disappear from selection immediately")
}
