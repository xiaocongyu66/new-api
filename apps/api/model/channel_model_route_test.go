package model

import (
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func withRouteDB(t *testing.T) func() {
	t.Helper()

	previousDB := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}, &ChannelModelRoute{}))
	DB = db
	return func() { DB = previousDB }
}

// makeSingleKeyChannel builds a single-key channel (IsMultiKey=false). Channel
// weight is gone: route expansion seeds every row at defaultRouteStaticWeight and
// scheduling reads that, so a channel carries no weight of its own.
func makeSingleKeyChannel(id int, models, group string, modelMapping *string) *Channel {
	ch := &Channel{
		Id:           id,
		Models:       models,
		Group:        group,
		Status:       1,
		ModelMapping: modelMapping,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   false,
			MultiKeySize: 1,
		},
	}
	return ch
}

// makeMultiKeyChannel builds a multi-key channel (IsMultiKey=true).
func makeMultiKeyChannel(id int, models, group, keys string, modelMapping *string, keyCount int) *Channel {
	ch := &Channel{
		Id:           id,
		Models:       models,
		Group:        group,
		Status:       1,
		ModelMapping: modelMapping,
		Key:          keys,
		ChannelInfo: ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: keyCount,
		},
	}
	return ch
}

// TestExpandDimensionSingleKey verifies expansion for a single-key channel.
// 2 models × 2 groups → exactly 4 rows; multi-key 3 keys × 1 group × 1 model → 3 rows (KeyIndex 0/1/2).
func TestExpandDimensionSingleKey(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	// Single-key: 2 models × 2 groups = 4 rows
	mapping := `{"alias-a":"upstream-x"}`
	ch := makeSingleKeyChannel(1, "alias-a,model-b", "group-1,group-2", &mapping)
	require.NoError(t, DB.Create(ch).Error)

	routes := ExpandChannelModelRoutes(ch)
	require.Len(t, routes, 4)
	// Verify each combination exists via typed struct key
	type routeKey struct {
		Group, Alias, Upstream string
		ChannelID, KeyIndex    int
	}
	expected := map[routeKey]struct{}{
		{Group: "group-1", Alias: "alias-a", ChannelID: 1, KeyIndex: 0, Upstream: "upstream-x"}: {},
		{Group: "group-1", Alias: "model-b", ChannelID: 1, KeyIndex: 0, Upstream: "model-b"}:    {},
		{Group: "group-2", Alias: "alias-a", ChannelID: 1, KeyIndex: 0, Upstream: "upstream-x"}: {},
		{Group: "group-2", Alias: "model-b", ChannelID: 1, KeyIndex: 0, Upstream: "model-b"}:    {},
	}
	for _, r := range routes {
		key := routeKey{r.Group, r.PublicModelAlias, r.UpstreamModel, r.ChannelId, r.KeyIndex}
		_, ok := expected[key]
		require.True(t, ok, "unexpected route: %+v", key)
		delete(expected, key)
	}
	require.Empty(t, expected)
}

// TestExpandDimensionMultiKey verifies expansion for a multi-key channel.
// 3 keys × 1 group × 1 model → 3 rows (KeyIndex 0/1/2).
func TestExpandDimensionMultiKey(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	ch := makeMultiKeyChannel(2, "model-x", "group-1", "k1\nk2\nk3", nil, 3)
	require.NoError(t, DB.Create(ch).Error)

	routes := ExpandChannelModelRoutes(ch)
	require.Len(t, routes, 3)

	keyIndices := make(map[int]bool)
	for _, r := range routes {
		assert.Equal(t, "group-1", r.Group)
		assert.Equal(t, "model-x", r.PublicModelAlias)
		assert.Equal(t, "model-x", r.UpstreamModel)
		assert.Equal(t, 2, r.ChannelId)
		keyIndices[r.KeyIndex] = true
	}
	require.Equal(t, map[int]bool{0: true, 1: true, 2: true}, keyIndices)
}

// TestModelMappingApplied verifies model_mapping {"alias-a":"upstream-x"} → alias-a row has UpstreamModel=upstream-x; unmapped model equals itself.
func TestModelMappingApplied(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	mapping := `{"alias-a":"upstream-x"}`
	ch := makeSingleKeyChannel(1, "alias-a,model-b", "group-1", &mapping)
	require.NoError(t, DB.Create(ch).Error)

	routes := ExpandChannelModelRoutes(ch)
	require.Len(t, routes, 2)

	upstreams := make(map[string]string)
	for _, r := range routes {
		upstreams[r.PublicModelAlias] = r.UpstreamModel
	}
	assert.Equal(t, "upstream-x", upstreams["alias-a"])
	assert.Equal(t, "model-b", upstreams["model-b"])
}

// TestWeightNotAmplifiedByKeyCount pins that a multi-key channel does not hand
// itself extra traffic. Each key becomes its own route unit, so multiplying the
// seed weight by the key count would give a 3-key channel three times the share
// of a single-key peer for identical configuration.
func TestWeightNotAmplifiedByKeyCount(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	ch := makeMultiKeyChannel(1, "model-x", "group-1", "k1\nk2\nk3", nil, 3)
	require.NoError(t, DB.Create(ch).Error)

	routes := ExpandChannelModelRoutes(ch)
	require.Len(t, routes, 3)
	for _, r := range routes {
		assert.Equal(t, defaultRouteStaticWeight, r.StaticWeight,
			"every key seeds at the default weight regardless of key count")
	}
}

// TestSyncIdempotent verifies two consecutive SyncChannelModelRoutesWithTx calls produce identical rows (ON CONFLICT DO NOTHING prevents dup insert).
func TestSyncIdempotent(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	ch := makeSingleKeyChannel(1, "model-a,model-b", "group-1", nil)
	require.NoError(t, DB.Create(ch).Error)

	// First sync
	require.NoError(t, SyncChannelModelRoutesWithTx(DB, 1))
	var count1 int64
	require.NoError(t, DB.Model(&ChannelModelRoute{}).Where("channel_id = ?", 1).Count(&count1).Error)
	require.Equal(t, int64(2), count1)

	// Second sync
	require.NoError(t, SyncChannelModelRoutesWithTx(DB, 1))
	var count2 int64
	require.NoError(t, DB.Model(&ChannelModelRoute{}).Where("channel_id = ?", 1).Count(&count2).Error)
	require.Equal(t, int64(2), count2)

	// Content identical
	var routes1, routes2 []ChannelModelRoute
	require.NoError(t, DB.Where("channel_id = ?", 1).Find(&routes1).Error)
	require.NoError(t, DB.Where("channel_id = ?", 1).Find(&routes2).Error)
	require.Equal(t, len(routes1), len(routes2))
	for i := range routes1 {
		assert.Equal(t, routes1[i].Group, routes2[i].Group)
		assert.Equal(t, routes1[i].PublicModelAlias, routes2[i].PublicModelAlias)
		assert.Equal(t, routes1[i].KeyIndex, routes2[i].KeyIndex)
		assert.Equal(t, routes1[i].UpstreamModel, routes2[i].UpstreamModel)
		assert.Equal(t, routes1[i].StaticWeight, routes2[i].StaticWeight)
	}
}

// TestSyncStaleCleanup verifies:
// - dropping a model from channel.Models → corresponding routes disappear, others remain
// - reducing keys 3→1 → KeyIndex 1/2 rows disappear
// - deleting channel row → all its routes cleared
func TestSyncStaleCleanup(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	// Seed: single-key channel with 2 models, 2 groups = 4 rows
	mapping := `{"alias-a":"upstream-x"}`
	ch := makeSingleKeyChannel(1, "alias-a,model-b", "group-1,group-2", &mapping)
	require.NoError(t, DB.Create(ch).Error)
	require.NoError(t, SyncChannelModelRoutesWithTx(DB, 1))

	var count int64
	require.NoError(t, DB.Model(&ChannelModelRoute{}).Where("channel_id = ?", 1).Count(&count).Error)
	require.Equal(t, int64(4), count)

	// Drop one model (keep alias-a only)
	ch.Models = "alias-a"
	require.NoError(t, DB.Save(ch).Error)
	require.NoError(t, SyncChannelModelRoutesWithTx(DB, 1))

	require.NoError(t, DB.Model(&ChannelModelRoute{}).Where("channel_id = ?", 1).Count(&count).Error)
	require.Equal(t, int64(2), count) // alias-a × 2 groups

	var remaining []ChannelModelRoute
	require.NoError(t, DB.Where("channel_id = ?", 1).Find(&remaining).Error)
	for _, r := range remaining {
		assert.Equal(t, "alias-a", r.PublicModelAlias)
	}

	// Multi-key: reduce keys 3→1
	ch2 := makeMultiKeyChannel(2, "model-x", "group-1", "k1\nk2\nk3", nil, 3)
	require.NoError(t, DB.Create(ch2).Error)
	require.NoError(t, SyncChannelModelRoutesWithTx(DB, 2))

	require.NoError(t, DB.Model(&ChannelModelRoute{}).Where("channel_id = ?", 2).Count(&count).Error)
	require.Equal(t, int64(3), count)

	// Reduce to 1 key
	ch2.Key = "k1-only"
	ch2.ChannelInfo.MultiKeySize = 1
	require.NoError(t, DB.Save(ch2).Error)
	require.NoError(t, SyncChannelModelRoutesWithTx(DB, 2))

	require.NoError(t, DB.Model(&ChannelModelRoute{}).Where("channel_id = ?", 2).Count(&count).Error)
	require.Equal(t, int64(1), count)

	var r2 ChannelModelRoute
	require.NoError(t, DB.Where("channel_id = ?", 2).First(&r2).Error)
	assert.Equal(t, 0, r2.KeyIndex)

	// Delete channel row → routes cleared
	require.NoError(t, DB.Delete(&Channel{}, 1).Error)
	require.NoError(t, SyncChannelModelRoutesWithTx(DB, 1))

	require.NoError(t, DB.Model(&ChannelModelRoute{}).Where("channel_id = ?", 1).Count(&count).Error)
	require.Equal(t, int64(0), count)
}

// TestUniqueConstraintNoDuplicate verifies manual duplicate insert (group,alias,channel,key_index,upstream) followed by Sync does not error and produces no duplicates, and that ON CONFLICT DO NOTHING preserves manual StaticWeight edits.
func TestUniqueConstraintNoDuplicate(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	ch := makeSingleKeyChannel(1, "model-a", "group-1", nil)
	require.NoError(t, DB.Create(ch).Error)
	require.NoError(t, SyncChannelModelRoutesWithTx(DB, 1))

	// Manually edit the existing row's StaticWeight to 999 (simulates human/admin override)
	require.NoError(t, DB.Model(&ChannelModelRoute{}).
		Where("channel_id = ? AND "+commonGroupCol+" = ? AND public_model_alias = ? AND key_index = ? AND upstream_model = ?",
			1, "group-1", "model-a", 0, "model-a").
		Update("static_weight", 999).Error)

	// Manually insert a duplicate row matching the unique index — should be ignored by ON CONFLICT DO NOTHING
	dup := ChannelModelRoute{
		Group:            "group-1",
		PublicModelAlias: "model-a",
		ChannelId:        1,
		KeyIndex:         0,
		UpstreamModel:    "model-a",
		StaticWeight:     100, // different weight, should NOT overwrite the manual 999
		Enabled:          true,
	}
	require.NoError(t, DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&dup).Error)

	// Sync again — should not error, no duplicate rows, manual 999 preserved
	require.NoError(t, SyncChannelModelRoutesWithTx(DB, 1))

	var count int64
	require.NoError(t, DB.Model(&ChannelModelRoute{}).Where("channel_id = ?", 1).Count(&count).Error)
	require.Equal(t, int64(1), count)

	var r ChannelModelRoute
	require.NoError(t, DB.Where("channel_id = ?", 1).First(&r).Error)
	assert.Equal(t, 999, r.StaticWeight, "manual StaticWeight must be preserved by ON CONFLICT DO NOTHING")
}

// TestSeedChannelModelRoutes verifies seeding pre-seeds 2 channels (one multi-key) → row count equals manual expansion; repeated Seed does not change row count.
func TestSeedChannelModelRoutes(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	// Channel 1: single-key, 2 models × 2 groups = 4 rows
	ch1 := makeSingleKeyChannel(1, "m1,m2", "g1,g2", nil)
	require.NoError(t, DB.Create(ch1).Error)

	// Channel 2: multi-key 3 keys × 1 model × 1 group = 3 rows
	ch2 := makeMultiKeyChannel(2, "m3", "g1", "k1\nk2\nk3", nil, 3)
	require.NoError(t, DB.Create(ch2).Error)

	// First seed
	require.NoError(t, SeedChannelModelRoutes())

	var count int64
	require.NoError(t, DB.Model(&ChannelModelRoute{}).Count(&count).Error)
	require.Equal(t, int64(7), count, "4 + 3 = 7 rows")

	// Second seed — count unchanged
	require.NoError(t, SeedChannelModelRoutes())
	require.NoError(t, DB.Model(&ChannelModelRoute{}).Count(&count).Error)
	require.Equal(t, int64(7), count)
}

// TestChannelLifecycleSyncsRouteRows verifies route rows stay in sync
// across the channel lifecycle: create → update (models change) → delete.
// Uses the internal tx methods (insertWithTx/updateWithTx/deleteWithTx)
// instead of the public Insert/Update/Delete because those route through
// MutateGatewayRouting which requires GatewayConfigRevision and
// GatewayConfigOutbox tables not migrated in the withRouteDB fixture.
// The internal methods still exercise the same SyncChannelModelRoutesWithTx
// code path and are accessible in the same package.
func TestChannelLifecycleSyncsRouteRows(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	// Create: single-key channel with 2 models × 1 group = 2 route rows
	ch := makeSingleKeyChannel(1, "model-a,model-b", "group-1", nil)
	require.NoError(t, ch.insertWithTx(DB))

	var count int64
	require.NoError(t, DB.Model(&ChannelModelRoute{}).Where("channel_id = ?", 1).Count(&count).Error)
	require.Equal(t, int64(2), count, "expected 2 rows after create (model-a,model-b × group-1)")

	// Update: change models to only model-a → 1 row
	ch.Models = "model-a"
	require.NoError(t, ch.updateWithTx(DB))

	require.NoError(t, DB.Model(&ChannelModelRoute{}).Where("channel_id = ?", 1).Count(&count).Error)
	require.Equal(t, int64(1), count, "expected 1 row after update to single model")

	var remaining []ChannelModelRoute
	require.NoError(t, DB.Where("channel_id = ?", 1).Find(&remaining).Error)
	for _, r := range remaining {
		assert.Equal(t, "model-a", r.PublicModelAlias, "only model-a route should remain")
	}

	// Delete: remove channel → 0 rows
	require.NoError(t, ch.deleteWithTx(DB))

	require.NoError(t, DB.Model(&ChannelModelRoute{}).Where("channel_id = ?", 1).Count(&count).Error)
	require.Equal(t, int64(0), count, "expected 0 rows after delete")
}

// TestGetRouteUnitViewsByAlias verifies GetRouteUnitViewsByAlias joins channel info
// and computes ExpectedShare correctly (weight/total rounded to 4 decimals).
func TestGetRouteUnitViewsByAlias(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	// Create channels
	ch1 := makeSingleKeyChannel(1, "model-a", "group-1", nil)
	ch1.Name = "channel-1"
	ch1.BaseURL = strPtr("https://api.example.com/v1")
	ch1.Status = 1
	require.NoError(t, DB.Create(ch1).Error)

	ch2 := makeSingleKeyChannel(2, "model-a", "group-1", nil)
	ch2.Name = "channel-2"
	ch2.BaseURL = strPtr("https://api.example.com/v2")
	ch2.Status = 2
	require.NoError(t, DB.Create(ch2).Error)

	// Seed routes
	require.NoError(t, SeedChannelModelRoutes())

	// Test normal case: two channels, weights 100 and 200
	views, err := GetRouteUnitViewsByAlias("model-a")
	require.NoError(t, err)
	require.Len(t, views, 2)

	// Find views by channel ID
	var v1, v2 RouteUnitView
	for _, v := range views {
		if v.ChannelId == 1 {
			v1 = v
		} else if v.ChannelId == 2 {
			v2 = v
		}
	}

	// Verify channel 1 (weight 100, total 200 -> share 0.5)
	assert.Equal(t, 1, v1.Id)
	assert.Equal(t, "group-1", v1.Group)
	assert.Equal(t, "model-a", v1.PublicModelAlias)
	assert.Equal(t, 1, v1.ChannelId)
	assert.Equal(t, "channel-1", v1.ChannelName)
	assert.Equal(t, 1, v1.ChannelStatus)
	assert.Equal(t, "https://api.example.com/v1", v1.BaseURL)
	assert.Equal(t, 0, v1.KeyIndex)
	assert.Equal(t, "model-a", v1.UpstreamModel)
	assert.Equal(t, 100, v1.StaticWeight)
	assert.True(t, v1.Enabled)
	// 100/200 = 0.5
	assert.InDelta(t, 0.5, v1.ExpectedShare, 0.0001)
	assert.Equal(t, 1.0, v1.HealthScore, "new channel should have default health score 1.0")

	// Verify channel 2 (weight 100, total 200 -> share 0.5)
	assert.Equal(t, 2, v2.ChannelId)
	assert.Equal(t, "channel-2", v2.ChannelName)
	assert.Equal(t, 2, v2.ChannelStatus)
	assert.Equal(t, "https://api.example.com/v2", v2.BaseURL)
	assert.Equal(t, 100, v2.StaticWeight)
	// 100/200 = 0.5
	assert.InDelta(t, 0.5, v2.ExpectedShare, 0.0001)
	assert.Equal(t, 1.0, v2.HealthScore)

	// Test total=0 scenario: manually set route weight to 0
	ch3 := makeSingleKeyChannel(3, "model-zero", "group-1", nil)
	ch3.Name = "channel-zero"
	require.NoError(t, DB.Create(ch3).Error)
	require.NoError(t, SeedChannelModelRoutes())
	// Force the route weight to 0 so total=0
	require.NoError(t, DB.Model(&ChannelModelRoute{}).
		Where("public_model_alias = ?", "model-zero").
		Update("static_weight", 0).Error)

	zeroViews, err := GetRouteUnitViewsByAlias("model-zero")
	require.NoError(t, err)
	require.Len(t, zeroViews, 1)
	assert.Equal(t, 0.0, zeroViews[0].ExpectedShare, "total=0 should yield share=0")
	assert.Equal(t, 0, zeroViews[0].StaticWeight)

	// Test alias not found returns empty slice
	emptyViews, err := GetRouteUnitViewsByAlias("non-existent")
	require.NoError(t, err)
	assert.Empty(t, emptyViews)

	// Test empty alias returns error
	_, err = GetRouteUnitViewsByAlias("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "alias is required")
}

// TestListRouteUnitAliases verifies ListRouteUnitAliases aggregates correctly.
func TestListRouteUnitAliases(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	// Create channels with different aliases and weights
	ch1 := makeSingleKeyChannel(1, "model-a,model-b", "group-1", nil)
	require.NoError(t, DB.Create(ch1).Error)

	ch2 := makeSingleKeyChannel(2, "model-a", "group-1", nil)
	require.NoError(t, DB.Create(ch2).Error)

	ch3 := makeSingleKeyChannel(3, "model-c", "group-1", nil)
	require.NoError(t, DB.Create(ch3).Error)

	require.NoError(t, SeedChannelModelRoutes())

	summaries, err := ListRouteUnitAliases()
	require.NoError(t, err)
	require.Len(t, summaries, 3)

	// Find by alias
	var sModelA, sModelB, sModelC RouteUnitAliasSummary
	for _, s := range summaries {
		switch s.Alias {
		case "model-a":
			sModelA = s
		case "model-b":
			sModelB = s
		case "model-c":
			sModelC = s
		}
	}

	// model-a: 2 routes (ch1 + ch2), total weight 100 + 100 = 200
	assert.Equal(t, "model-a", sModelA.Alias)
	assert.Equal(t, 2, sModelA.RouteCount)
	assert.Equal(t, 200, sModelA.TotalWeight)

	// model-b: 1 route (ch1), total weight 100
	assert.Equal(t, "model-b", sModelB.Alias)
	assert.Equal(t, 1, sModelB.RouteCount)
	assert.Equal(t, 100, sModelB.TotalWeight)

	// model-c: 1 route (ch3), total weight 100
	assert.Equal(t, "model-c", sModelC.Alias)
	assert.Equal(t, 1, sModelC.RouteCount)
	assert.Equal(t, 100, sModelC.TotalWeight)
}

// TestUpdateRouteUnitConfig verifies partial updates and error cases.
func TestUpdateRouteUnitConfig(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	ch := makeSingleKeyChannel(1, "model-a", "group-1", nil)
	require.NoError(t, DB.Create(ch).Error)
	require.NoError(t, SeedChannelModelRoutes())

	var route ChannelModelRoute
	require.NoError(t, DB.Where("channel_id = ?", 1).First(&route).Error)
	routeID := route.Id

	// Test update weight only
	err := UpdateRouteUnitConfig(routeID, intPtr(250), nil)
	require.NoError(t, err)

	var updated ChannelModelRoute
	require.NoError(t, DB.First(&updated, routeID).Error)
	assert.Equal(t, 250, updated.StaticWeight)
	assert.True(t, updated.Enabled, "enabled should remain unchanged")

	// Test update enabled only
	err = UpdateRouteUnitConfig(routeID, nil, boolPtr(false))
	require.NoError(t, err)

	require.NoError(t, DB.First(&updated, routeID).Error)
	assert.Equal(t, 250, updated.StaticWeight, "weight should remain unchanged")
	assert.False(t, updated.Enabled)

	// Test update both
	err = UpdateRouteUnitConfig(routeID, intPtr(300), boolPtr(true))
	require.NoError(t, err)

	require.NoError(t, DB.First(&updated, routeID).Error)
	assert.Equal(t, 300, updated.StaticWeight)
	assert.True(t, updated.Enabled)

	// Test non-existent ID returns ErrRecordNotFound
	err = UpdateRouteUnitConfig(99999, intPtr(100), nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound), "should return ErrRecordNotFound for non-existent ID")

	// Test empty body (no fields) returns error
	err = UpdateRouteUnitConfig(routeID, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no fields to update")

	// Test negative weight returns error
	err = UpdateRouteUnitConfig(routeID, intPtr(-10), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "static_weight must be >= 0")
}

// Helpers for test pointers
func strPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}
