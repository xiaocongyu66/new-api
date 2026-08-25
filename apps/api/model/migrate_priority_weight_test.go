package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedLegacyWeightColumn re-creates the pre-cleanup shape: the current Channel
// struct has already lost the weight column, so an upgrading instance is
// simulated by adding it back and writing values straight through SQL.
func seedLegacyWeightColumn(t *testing.T, weights map[int]int) {
	t.Helper()
	require.NoError(t, DB.Exec("ALTER TABLE channels ADD COLUMN weight integer DEFAULT 0").Error)
	for id, weight := range weights {
		require.NoError(t, DB.Exec(
			"INSERT INTO channels (id, `key`, models, `group`, status, weight) VALUES (?, ?, ?, ?, ?, ?)",
			id, "sk-test", "model-a", "default", 1, weight).Error)
	}
}

// TestCarryOverChannelWeightPreservesTunedRoutes is the contract that makes
// dropping channels.weight non-destructive.
//
// The two settings were never linked: route units seed at
// defaultRouteStaticWeight and ExpandChannelModelRoutes never read
// channel.Weight. An operator who tuned channel weights therefore has that intent
// recorded only in the column being dropped, and a bare DROP COLUMN would
// silently reset their traffic split to uniform. At the same time a weight already
// tuned in the route unit UI is the newer intent and must survive untouched.
func TestCarryOverChannelWeightPreservesTunedRoutes(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	seedLegacyWeightColumn(t, map[int]int{1: 25, 2: 0})

	// Channel 1 owns two routes: one still at the seed default, one already tuned.
	// Channel 2 carries no legacy weight at all.
	require.NoError(t, DB.Create(&[]ChannelModelRoute{
		{Group: "default", PublicModelAlias: "model-a", ChannelId: 1, KeyIndex: 0,
			UpstreamModel: "model-a", StaticWeight: defaultRouteStaticWeight, Enabled: true},
		{Group: "vip", PublicModelAlias: "model-a", ChannelId: 1, KeyIndex: 0,
			UpstreamModel: "model-a", StaticWeight: 7, Enabled: true},
		{Group: "default", PublicModelAlias: "model-a", ChannelId: 2, KeyIndex: 0,
			UpstreamModel: "model-a", StaticWeight: defaultRouteStaticWeight, Enabled: true},
	}).Error)

	require.NoError(t, carryOverChannelWeightToRoutes())

	weightOf := func(group string, channelID int) int {
		var row ChannelModelRoute
		require.NoError(t, DB.Where("`group` = ? AND channel_id = ?", group, channelID).First(&row).Error)
		return row.StaticWeight
	}

	assert.Equal(t, 25, weightOf("default", 1),
		"a route still at the seed default must inherit the retiring channel weight")
	assert.Equal(t, 7, weightOf("vip", 1),
		"a route tuned in the route unit UI expresses newer intent and must not be overwritten")
	assert.Equal(t, defaultRouteStaticWeight, weightOf("default", 2),
		"a channel with no legacy weight must leave its routes at the default")
}

// TestDropLegacySchedulingColumnsIsIdempotent pins that a restart cannot fail on
// columns a previous boot already removed, and that a fresh install with no legacy
// columns at all is a clean no-op.
func TestDropLegacySchedulingColumnsIsIdempotent(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	seedLegacyWeightColumn(t, map[int]int{1: 40})
	require.True(t, columnExists("channels", "weight"), "fixture must start with the legacy column")

	require.NoError(t, dropLegacySchedulingColumns())
	assert.False(t, columnExists("channels", "weight"), "first run drops the column")

	require.NoError(t, dropLegacySchedulingColumns(), "second run must be a no-op, not an error")
	assert.False(t, columnExists("channels", "weight"))
}

// TestCarryOverSkippedWithoutLegacyColumn covers the fresh-install path: with no
// weight column there is nothing to read, and the carry-over must not error on the
// missing column.
func TestCarryOverSkippedWithoutLegacyColumn(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	require.False(t, columnExists("channels", "weight"))
	require.NoError(t, carryOverChannelWeightToRoutes())
}
