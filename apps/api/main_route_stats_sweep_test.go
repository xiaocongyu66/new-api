package main

import (
	"context"
	"testing"
	"time"

	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/catalog/routestats"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestRunRouteStatsSweepStopsOnCancel pins the sweep goroutine's lifecycle.
//
// The loop used to build its ticker inline in the range clause, which left the
// *time.Ticker unreachable: nothing could stop it and the goroutine had no exit,
// so shutdown could not halt it and a test could not drive it. Cancelling the
// context must both return from the loop and release the ticker.
func TestRunRouteStatsSweepStopsOnCancel(t *testing.T) {
	routestats.ResetShares()
	// The sweep reads the live pool set from channel_model_routes when the memory
	// cache is off, so it needs a handle. Without one the keep set is unknown and
	// the sweep correctly refuses to evict anything.
	previousDB, previousMain, previousLog := dbx.DB, common.MainDatabaseType(), common.LogDatabaseType()
	previousMemoryCache := common.MemoryCacheEnabled
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dbx.InitColumns()
	common.MemoryCacheEnabled = false
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&catalog.ChannelModelRoute{}))
	dbx.DB = db
	t.Cleanup(func() {
		dbx.DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
		dbx.InitColumns()
		common.MemoryCacheEnabled = previousMemoryCache
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	t.Cleanup(routestats.ResetShares)

	// A pool with no live route unit: the sweep must evict it, which is how the
	// test observes that a tick actually ran.
	cfg := routestats.DefaultRouteStatsSetting()
	orphan := routestats.PoolKey{Group: "sweep-test", PublicModelAlias: "gone"}
	selected := routestats.RouteID{ChannelID: 9901, KeyIndex: 0, UpstreamModel: "gone"}
	routestats.RecordSelection(orphan, selected, map[routestats.RouteID]float64{selected: 1.0}, cfg)
	require.Equal(t, 1, routestats.SharePoolCount())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runRouteStatsSweep(ctx, time.Millisecond)
		close(done)
	}()

	require.Eventually(t, func() bool { return routestats.SharePoolCount() == 0 },
		2*time.Second, 5*time.Millisecond,
		"the sweep must evict a pool with no live route unit")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		assert.Fail(t, "the sweep loop must return when its context is cancelled")
	}
}
