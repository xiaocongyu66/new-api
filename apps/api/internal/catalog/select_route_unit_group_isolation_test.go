package channel

import (
	"math/rand/v2"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Group isolation is the one property of this scheduler whose failure is a
// security incident rather than a performance regression: a user reaching a
// channel their group does not grant is data leaving the boundary the operator
// paid for. Route units are no longer keyed by group, so nothing in the route
// table itself constrains who may use one — the constraint moved to selection,
// and these tests are what hold it there.
//
// The fixture builds the real cache state InitChannelCache would produce, then
// drives the real selector end to end. Both cache modes are exercised, because
// eligibility is resolved from group2model2channels when the memory cache is on
// and straight from abilities when it is off, and a divergence between the two is
// exactly how an isolation bug would hide in production.

// withGroupIsolationRouteDB installs a scratch database plus the cache globals, and
// restores everything afterwards.
func withGroupIsolationRouteDB(t *testing.T) *gorm.DB {
	t.Helper()

	prevDB := dbx.DB
	prevMain := common.MainDatabaseType()
	prevLog := common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dbx.InitColumns()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}, &ChannelModelRoute{}))
	dbx.DB = db

	prevMemoryCache := common.MemoryCacheEnabled
	prevGroups := group2model2channels
	prevIDM := channelsIDM
	prevAliasRoutes := alias2routes

	ClearRouteHealthCache()
	t.Cleanup(func() {
		channelSyncLock.Lock()
		group2model2channels = prevGroups
		channelsIDM = prevIDM
		alias2routes = prevAliasRoutes
		channelSyncLock.Unlock()
		common.MemoryCacheEnabled = prevMemoryCache
		dbx.DB = prevDB
		common.SetDatabaseTypes(prevMain, prevLog)
		dbx.InitColumns()
		ClearRouteHealthCache()
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// seedGroupIsolationChannel writes a channel together with the ability rows and route
// units that AddAbilities and ExpandChannelModelRoutes would produce for it, so
// the fixture cannot drift from the production write path.
func seedGroupIsolationChannel(t *testing.T, db *gorm.DB, id int, groups []string, alias string) {
	t.Helper()

	groupCSV := ""
	for i, g := range groups {
		if i > 0 {
			groupCSV += ","
		}
		groupCSV += g
	}
	require.NoError(t, db.Create(&Channel{
		Id: id, Type: 1, Name: "iso-channel", Key: "sk-iso",
		Models: alias, Group: groupCSV, Status: common.ChannelStatusEnabled,
	}).Error)
	for _, g := range groups {
		require.NoError(t, db.Create(&Ability{
			Group: g, Model: alias, ChannelId: id, Enabled: true,
		}).Error)
	}
	require.NoError(t, db.Create(&ChannelModelRoute{
		PublicModelAlias: alias, ChannelId: id, KeyIndex: 0,
		UpstreamModel: alias, StaticWeight: 100, Enabled: true,
	}).Error)
}

// selectManyChannelIDs drives the real selector repeatedly and returns the set of
// channel ids it handed out. Repetition matters: the draw is weighted-random, so a
// single call could miss a leaking channel by chance.
func selectManyChannelIDs(t *testing.T, group, alias string, draws int) map[int]int {
	t.Helper()
	rnd := rand.New(rand.NewPCG(20260910, 7))
	seen := make(map[int]int)
	for range draws {
		route, err := SelectRouteUnit(group, alias, "", 0, nil, rnd)
		require.NoError(t, err)
		if route == nil {
			seen[0]++
			continue
		}
		seen[route.ChannelId]++
	}
	return seen
}

// TestGroupIsolationRestrictsCandidatesToGrantedChannels is the primary guard.
// Channel 8101 serves both groups; 8102 serves only "vip". A "default" request
// must never be handed 8102, no matter how the route table looks.
func TestGroupIsolationRestrictsCandidatesToGrantedChannels(t *testing.T) {
	const alias = "iso-model"
	for _, memoryCache := range []bool{true, false} {
		name := "memory-cache"
		if !memoryCache {
			name = "db-path"
		}
		t.Run(name, func(t *testing.T) {
			db := withGroupIsolationRouteDB(t)
			seedGroupIsolationChannel(t, db, 8101, []string{"default", "vip"}, alias)
			seedGroupIsolationChannel(t, db, 8102, []string{"vip"}, alias)

			common.MemoryCacheEnabled = memoryCache
			InitChannelCache()
			if !memoryCache {
				// InitChannelCache early-returns with the cache off; the selector must
				// then resolve eligibility from abilities directly.
				channelSyncLock.Lock()
				group2model2channels = nil
				alias2routes = nil
				channelsIDM = map[int]*Channel{}
				var channels []*Channel
				require.NoError(t, db.Find(&channels).Error)
				for _, ch := range channels {
					channelsIDM[ch.Id] = ch
				}
				channelSyncLock.Unlock()
			}

			defaultSeen := selectManyChannelIDs(t, "default", alias, 60)
			assert.NotContains(t, defaultSeen, 8102,
				"a default-group request must never reach a vip-only channel")
			assert.Contains(t, defaultSeen, 8101,
				"the channel the group does grant must still serve")

			vipSeen := selectManyChannelIDs(t, "vip", alias, 60)
			assert.Contains(t, vipSeen, 8101)
			assert.Contains(t, vipSeen, 8102,
				"vip grants both channels, so both must be reachable")
		})
	}
}

// TestGroupIsolationFailsClosedForUnknownGroup pins the fail-closed direction. A
// group nobody granted must select nothing, rather than falling through to the
// alias's full candidate set. Route units carry no group of their own, so an
// "unfiltered means allow all" mistake here would expose every channel to every
// caller, including an empty group string.
func TestGroupIsolationFailsClosedForUnknownGroup(t *testing.T) {
	const alias = "iso-model"
	db := withGroupIsolationRouteDB(t)
	seedGroupIsolationChannel(t, db, 8201, []string{"vip"}, alias)

	common.MemoryCacheEnabled = true
	InitChannelCache()

	for _, group := range []string{"no-such-group", ""} {
		route, err := SelectRouteUnit(group, alias, "", 0, nil, rand.New(rand.NewPCG(1, 2)))
		require.NoError(t, err)
		assert.Nil(t, route, "group %q was never granted this alias, so it must select nothing", group)
	}

	// Sanity: the granted group still works, so the assertions above are not
	// passing merely because the fixture is broken.
	route, err := SelectRouteUnit("vip", alias, "", 0, nil, rand.New(rand.NewPCG(1, 2)))
	require.NoError(t, err)
	require.NotNil(t, route)
	assert.Equal(t, 8201, route.ChannelId)
}

// TestGroupIsolationHonoursPerModelDisable covers the interaction with
// DisableChannelModel, which flips abilities.enabled for one channel+model across
// every group. Eligibility is read from that table, so a disabled model must
// disappear from selection even though its route unit row is untouched.
func TestGroupIsolationHonoursPerModelDisable(t *testing.T) {
	const alias = "iso-model"
	db := withGroupIsolationRouteDB(t)
	seedGroupIsolationChannel(t, db, 8301, []string{"default"}, alias)
	seedGroupIsolationChannel(t, db, 8302, []string{"default"}, alias)

	common.MemoryCacheEnabled = true
	InitChannelCache()
	require.Contains(t, selectManyChannelIDs(t, "default", alias, 40), 8302)

	require.NoError(t, db.Model(&Ability{}).
		Where("channel_id = ? AND model = ?", 8302, alias).
		Update("enabled", false).Error)
	InitChannelCache()

	seen := selectManyChannelIDs(t, "default", alias, 40)
	assert.NotContains(t, seen, 8302,
		"a model disabled on a channel must leave the candidate set")
	assert.Contains(t, seen, 8301, "its healthy peer must keep serving")
}
