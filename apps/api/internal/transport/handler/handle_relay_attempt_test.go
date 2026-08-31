package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/routestats"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/constant"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupRelayAttemptDB installs a scratch database with the dialect fragments
// initialised. dbx.InitColumns matters here: the route lookup inside
// SelectedRouteFromChannel builds raw SQL from dbx.GroupCol(), so an
// uninitialised column name would silently turn the lookup into an error and
// mask the accounting this test is about.
func setupRelayAttemptDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := dbx.DB
	previousLogDB := dbx.LogDB
	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	previousMemoryCache := common.MemoryCacheEnabled
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dbx.InitColumns()
	common.MemoryCacheEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	dbx.DB = db
	dbx.LogDB = db
	require.NoError(t, db.AutoMigrate(&catalog.Channel{}, &catalog.ChannelModelRoute{}))

	t.Cleanup(func() {
		dbx.DB = previousDB
		dbx.LogDB = previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		dbx.InitColumns()
		common.MemoryCacheEnabled = previousMemoryCache
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// TestGetChannelReusesRouteResolvedByDistribute is the regression gate for the
// first-attempt branch of getChannel.
//
// ChannelMeta is first assigned by InitChannelMeta, which runs after getChannel
// returns, so iteration 0 of EVERY request takes the ChannelMeta == nil branch —
// not just a specific-channel replay. Re-deriving the route there costs twice:
// recordBypassSelection folds a second entry into the share window on top of the
// one Distribute already recorded, and GetNextEnabledKey draws a key independently
// of the one actually serving the request, so isolation and success are charged
// against a key index that never handled the attempt.
func TestGetChannelReusesRouteResolvedByDistribute(t *testing.T) {
	db := setupRelayAttemptDB(t)
	routestats.ResetShares()
	t.Cleanup(routestats.ResetShares)

	const (
		channelID = 8301
		alias     = "gpt-4"
		group     = "default"
	)
	// Two keys, no explicit multi-key mode: an independent re-draw through
	// GetNextEnabledKey deterministically returns the lowest enabled index (0),
	// while the route Distribute resolved serves key index 1. That gap is what
	// makes the misattribution observable instead of coin-flip flaky.
	channel := &catalog.Channel{
		Id: channelID, Type: 1, Name: "relay-attempt", Key: "key-zero\nkey-one",
		Models: alias, Group: group, Status: common.ChannelStatusEnabled,
		ChannelInfo: catalog.ChannelInfo{IsMultiKey: true, MultiKeySize: 2},
	}
	require.NoError(t, db.Create(channel).Error)
	for keyIndex, id := range map[int]int{0: 1, 1: 2} {
		require.NoError(t, db.Create(&catalog.ChannelModelRoute{
			Id: id, Group: group, PublicModelAlias: alias, ChannelId: channelID,
			KeyIndex: keyIndex, UpstreamModel: alias, StaticWeight: 100, Enabled: true,
		}).Error)
	}

	// What Distribute produced: the route for key index 1, with its share entry
	// already recorded by selectByWeight.
	servingRoute, err := catalog.SelectedRouteFromChannel(channel, alias)
	require.NoError(t, err)
	require.Equal(t, 0, servingRoute.KeyIndex,
		"sanity: an independent draw lands on key index 0, so the fixture can tell the two apart")
	servingRoute.KeyIndex = 1
	servingRoute.Key = "key-one"
	servingRoute.RouteId = 2

	pool := routestats.PoolKey{Group: group, PublicModelAlias: alias}
	selected := routestats.RouteID{ChannelID: channelID, KeyIndex: 1, UpstreamModel: alias}
	targets := map[routestats.RouteID]float64{
		selected: 0.5,
		{ChannelID: channelID, KeyIndex: 0, UpstreamModel: alias}: 0.5,
	}
	cfg := routestats.GetRouteStatsSetting()
	// The share entry Distribute's own selection recorded. SelectedRouteFromChannel
	// above already recorded one for key 0, so reset and record exactly one.
	routestats.ResetShares()
	routestats.RecordSelection(pool, selected, targets, cfg)
	require.Equal(t, 1, routestats.Corrections(pool, targets, cfg)[selected].Opportunities,
		"fixture must start from exactly one recorded selection")

	ctx, _ := ginadapter.NewSyntheticContext(
		httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	common.SetCtxKey(ctx, constant.ContextKeySelectedRoute, servingRoute)
	common.SetCtxKey(ctx, constant.ContextKeyChannelId, channelID)

	info := &relaycommon.RelayInfo{OriginModelName: alias}
	require.Nil(t, info.ChannelMeta,
		"the branch under test is exactly the one every first attempt takes")

	route, apiErr := getChannel(ctx, info, &catalog.SelectParams{
		Ctx: ctx, ModelName: alias, TokenGroup: group,
		Retry: common.GetPointer(0), ExcludeRoutes: make(map[catalog.RouteKey]bool),
	})
	require.Nil(t, apiErr)
	require.NotNil(t, route)

	assert.Same(t, servingRoute, route, "the first attempt must reuse the route Distribute resolved")
	assert.Equal(t, catalog.RouteKey{ChannelId: channelID, KeyIndex: 1, Model: alias},
		catalog.RouteKey{ChannelId: route.ChannelId, KeyIndex: route.KeyIndex, Model: route.Alias},
		"health and isolation must be charged against the key that actually serves the attempt")

	assert.Equal(t, 1, routestats.Corrections(pool, targets, cfg)[selected].Opportunities,
		"one request must contribute exactly one entry to the share window")
}

// TestGetChannelResolvesRouteForSpecificChannelReplay keeps the genuine replay
// path alive: with no route in context the channel identity still comes from
// context and the route unit is resolved from it.
func TestGetChannelResolvesRouteForSpecificChannelReplay(t *testing.T) {
	db := setupRelayAttemptDB(t)
	routestats.ResetShares()
	t.Cleanup(routestats.ResetShares)

	const (
		channelID = 8401
		alias     = "gpt-4"
		group     = "default"
	)
	require.NoError(t, db.Create(&catalog.Channel{
		Id: channelID, Type: 1, Name: "replay", Key: "sk-replay",
		Models: alias, Group: group, Status: common.ChannelStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&catalog.ChannelModelRoute{
		Id: 5, Group: group, PublicModelAlias: alias, ChannelId: channelID,
		KeyIndex: 0, UpstreamModel: alias, StaticWeight: 100, Enabled: true,
	}).Error)

	ctx, _ := ginadapter.NewSyntheticContext(
		httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))
	common.SetCtxKey(ctx, constant.ContextKeyChannelId, channelID)

	route, apiErr := getChannel(ctx, &relaycommon.RelayInfo{OriginModelName: alias},
		&catalog.SelectParams{Ctx: ctx, ModelName: alias, TokenGroup: group,
			Retry: common.GetPointer(0), ExcludeRoutes: make(map[catalog.RouteKey]bool)})
	require.Nil(t, apiErr)
	require.NotNil(t, route)
	assert.Equal(t, channelID, route.ChannelId)
	assert.Equal(t, 5, route.RouteId, "the replay must still resolve its route unit")
}

// TestAttemptTTFTMsIsScopedToTheAttempt is the regression gate for the TTFT
// charge. StartTime is whole-request scoped, so charging from it hands a retry
// the accumulated latency of every attempt before it — and because
// isFirstResponse is armed once at construction and never reset per attempt, a
// frozen timestamp from an earlier attempt would be re-charged to every later
// one. ObserveTTFT is peak-sensitive and TTFT carries 25% of quality synthesis,
// so either mistake makes a healthy retry target inherit its slow sibling's score.
func TestAttemptTTFTMsIsScopedToTheAttempt(t *testing.T) {
	start := time.Now()

	t.Run("retry charges its own TTFT, not the whole request", func(t *testing.T) {
		attemptStart := start.Add(5 * time.Second)
		info := &relaycommon.RelayInfo{
			IsStream:          true,
			StartTime:         start,
			FirstResponseTime: attemptStart.Add(200 * time.Millisecond),
		}
		ttftMs, ok := attemptTTFTMs(info, attemptStart)
		require.True(t, ok)
		assert.InDelta(t, 200.0, ttftMs, 1.0,
			"the retry must be charged its own 200ms, not the 5200ms since request start")
	})

	t.Run("a timestamp frozen by an earlier attempt is skipped", func(t *testing.T) {
		info := &relaycommon.RelayInfo{
			IsStream:  true,
			StartTime: start,
			// Attempt 1 streamed at +200ms and cleared isFirstResponse, so this
			// value is stale by the time attempt 2 starts at +5s.
			FirstResponseTime: start.Add(200 * time.Millisecond),
		}
		_, ok := attemptTTFTMs(info, start.Add(5*time.Second))
		assert.False(t, ok, "a frozen first-response time must not be charged again")
	})

	t.Run("first attempt still charges normally", func(t *testing.T) {
		info := &relaycommon.RelayInfo{
			IsStream:          true,
			StartTime:         start,
			FirstResponseTime: start.Add(300 * time.Millisecond),
		}
		ttftMs, ok := attemptTTFTMs(info, start)
		require.True(t, ok)
		assert.InDelta(t, 300.0, ttftMs, 1.0)
	})

	t.Run("nothing is charged without a streamed response", func(t *testing.T) {
		nonStream := &relaycommon.RelayInfo{
			IsStream:          false,
			StartTime:         start,
			FirstResponseTime: start.Add(300 * time.Millisecond),
		}
		_, ok := attemptTTFTMs(nonStream, start)
		assert.False(t, ok, "a non-streaming attempt has no TTFT")

		noResponse := &relaycommon.RelayInfo{
			IsStream:          true,
			StartTime:         start,
			FirstResponseTime: start.Add(-time.Second),
		}
		_, ok = attemptTTFTMs(noResponse, start)
		assert.False(t, ok, "an attempt that sent nothing has no TTFT")
	})
}
