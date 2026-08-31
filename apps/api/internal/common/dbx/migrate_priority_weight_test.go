package dbx

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// withRouteDB gives each test a scratch database holding the two tables the
// migration touches.
//
// The tables are created with raw DDL rather than AutoMigrate'd from the catalog
// record types on purpose: catalog imports this package, so importing it back
// here is an import cycle. The migration itself has the same constraint and
// solves it the same way — it addresses both tables by name (see columnExists),
// never by Go type — so asserting against the table contract is what production
// actually depends on.
func withRouteDB(t *testing.T) func() {
	t.Helper()
	previous := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TABLE channels (id integer PRIMARY KEY, `key` text, models text, `group` text, status integer)").Error)
	require.NoError(t, db.Exec(`CREATE TABLE channel_model_routes (
		id integer PRIMARY KEY AUTOINCREMENT,
		`+"`group`"+` text,
		public_model_alias text,
		channel_id integer,
		key_index integer,
		upstream_model text,
		static_weight integer,
		enabled numeric
	)`).Error)
	DB = db
	return func() { DB = previous }
}

// seedLegacyWeightColumn re-creates the pre-cleanup shape: the current channels
// table no longer carries a weight column, so an upgrading instance is simulated
// by adding it back and writing values straight through SQL.
func seedLegacyWeightColumn(t *testing.T, weights map[int]int) {
	t.Helper()
	require.NoError(t, DB.Exec("ALTER TABLE channels ADD COLUMN weight integer DEFAULT 0").Error)
	for id, weight := range weights {
		require.NoError(t, DB.Exec(
			"INSERT INTO channels (id, `key`, models, `group`, status, weight) VALUES (?, ?, ?, ?, ?, ?)",
			id, "sk-test", "model-a", "default", 1, weight).Error)
	}
}

// seedRoute inserts one route unit row. static_weight is stated per row because
// whether it still sits at legacyRouteSeedWeight is exactly what decides if the
// carry-over may overwrite it.
func seedRoute(t *testing.T, group string, channelID, staticWeight int) {
	t.Helper()
	require.NoError(t, DB.Exec(
		"INSERT INTO channel_model_routes (`group`, public_model_alias, channel_id, key_index, upstream_model, static_weight, enabled) VALUES (?, ?, ?, ?, ?, ?, ?)",
		group, "model-a", channelID, 0, "model-a", staticWeight, true).Error)
}

// TestCarryOverChannelWeightPreservesTunedRoutes is the contract that makes
// dropping channels.weight non-destructive.
//
// The two settings were never linked: route units seed at
// legacyRouteSeedWeight and route expansion never read channel.Weight. An
// operator who tuned channel weights therefore has that intent recorded only in
// the column being dropped, and a bare DROP COLUMN would silently reset their
// traffic split to uniform. At the same time a weight already tuned in the route
// unit UI is the newer intent and must survive untouched.
func TestCarryOverChannelWeightPreservesTunedRoutes(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	seedLegacyWeightColumn(t, map[int]int{1: 25, 2: 0})

	// Channel 1 owns two routes: one still at the seed default, one already tuned.
	// Channel 2 carries no legacy weight at all.
	seedRoute(t, "default", 1, legacyRouteSeedWeight)
	seedRoute(t, "vip", 1, 7)
	seedRoute(t, "default", 2, legacyRouteSeedWeight)

	require.NoError(t, carryOverChannelWeightToRoutes())

	weightOf := func(group string, channelID int) int {
		var weight int
		require.NoError(t, DB.Raw(
			"SELECT static_weight FROM channel_model_routes WHERE `group` = ? AND channel_id = ?",
			group, channelID).Scan(&weight).Error)
		return weight
	}

	assert.Equal(t, 25, weightOf("default", 1),
		"a route still at the seed default must inherit the retiring channel weight")
	assert.Equal(t, 7, weightOf("vip", 1),
		"a route tuned in the route unit UI expresses newer intent and must not be overwritten")
	assert.Equal(t, legacyRouteSeedWeight, weightOf("default", 2),
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

// TestCarryOverSkippedWithoutRouteTable covers the other fresh-install ordering:
// an instance that still carries legacy channels.weight from a previous
// deployment but has not yet AutoMigrate'd channel_model_routes. There is nothing
// to migrate onto, so the carry-over must no-op instead of failing on the missing
// table and blocking the column drop.
func TestCarryOverSkippedWithoutRouteTable(t *testing.T) {
	cleanup := withRouteDB(t)
	defer cleanup()

	seedLegacyWeightColumn(t, map[int]int{1: 25})
	require.NoError(t, DB.Exec("DROP TABLE channel_model_routes").Error)
	require.False(t, tableExists("channel_model_routes"))

	require.NoError(t, carryOverChannelWeightToRoutes())
	require.NoError(t, dropLegacySchedulingColumns(), "the legacy column must still be droppable")
	assert.False(t, columnExists("channels", "weight"))
}
