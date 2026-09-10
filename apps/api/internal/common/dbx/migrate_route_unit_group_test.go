package dbx

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// withGroupScopedRouteDB re-creates the pre-collapse schema: channel_model_routes
// still carrying the group column and its group-scoped unique index. Raw DDL is
// used because the Go struct no longer declares the column, which is the whole
// reason the migration addresses the table by name.
func withGroupScopedRouteDB(t *testing.T) func() {
	t.Helper()
	previous := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
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
	require.NoError(t, db.Exec(`CREATE UNIQUE INDEX idx_route_unit ON channel_model_routes
		(`+"`group`"+`, public_model_alias, channel_id, key_index, upstream_model)`).Error)
	DB = db
	return func() { DB = previous }
}

func seedGroupScopedRoute(t *testing.T, group, alias string, channelID, keyIndex int, weight int, enabled bool) {
	t.Helper()
	require.NoError(t, DB.Exec(
		"INSERT INTO channel_model_routes (`group`, public_model_alias, channel_id, key_index, upstream_model, static_weight, enabled) VALUES (?, ?, ?, ?, ?, ?, ?)",
		group, alias, channelID, keyIndex, alias, weight, enabled).Error)
}

// TestCollapseRouteUnitGroupsKeepsTunedWeight reproduces the production incident
// that motivated the change: six group-siblings of one unit, five tuned to 2 and
// one left at the seed 100 because the admin list rendered them
// indistinguishably. Collapsing must yield ONE row, and it must keep the tuned
// value rather than resurrecting the default the operator had already overridden
// five times.
func TestCollapseRouteUnitGroupsKeepsTunedWeight(t *testing.T) {
	cleanup := withGroupScopedRouteDB(t)
	defer cleanup()

	for _, group := range []string{"default", "奶酪", "牛奶", "芝士", "蓝纹奶酪"} {
		seedGroupScopedRoute(t, group, "deepseek-v4-pro", 16, 0, 2, true)
	}
	seedGroupScopedRoute(t, "高钙牛奶", "deepseek-v4-pro", 16, 0, 100, true)

	require.NoError(t, CollapseRouteUnitGroups())

	assert.False(t, columnExists("channel_model_routes", "group"),
		"the group column must be retired so the new unique index can be built")

	var rows []struct {
		Id           int
		StaticWeight int
		Enabled      bool
	}
	require.NoError(t, DB.Table("channel_model_routes").
		Select("id, static_weight, enabled").Scan(&rows).Error)
	require.Len(t, rows, 1, "six group-siblings are one scheduling unit")
	assert.Equal(t, 100, rows[0].StaticWeight,
		"the surviving row keeps the maximum of its siblings' weights")
	assert.True(t, rows[0].Enabled)
}

// TestCollapseRouteUnitGroupsPreservesDistinctUnits guards against over-merging:
// rows that differ in any part of the new identity (alias, channel, key index,
// upstream model) are separate units and must all survive.
func TestCollapseRouteUnitGroupsPreservesDistinctUnits(t *testing.T) {
	cleanup := withGroupScopedRouteDB(t)
	defer cleanup()

	// Same unit in two groups -> collapses to one.
	seedGroupScopedRoute(t, "default", "gpt-5", 1, 0, 100, true)
	seedGroupScopedRoute(t, "vip", "gpt-5", 1, 0, 100, true)
	// Genuinely distinct units.
	seedGroupScopedRoute(t, "default", "gpt-5", 1, 1, 100, true) // other key index
	seedGroupScopedRoute(t, "default", "gpt-5", 2, 0, 100, true) // other channel
	seedGroupScopedRoute(t, "default", "gpt-4", 1, 0, 100, true) // other alias

	require.NoError(t, CollapseRouteUnitGroups())

	var count int64
	require.NoError(t, DB.Table("channel_model_routes").Count(&count).Error)
	assert.Equal(t, int64(4), count,
		"only the two group-siblings merge; the other three units are distinct")

	// The collapsed table must now satisfy the new unique key, which is what
	// AutoMigrate is about to enforce.
	var dupGroups int64
	require.NoError(t, DB.Raw(`SELECT count(*) FROM (
		SELECT 1 FROM channel_model_routes
		GROUP BY public_model_alias, channel_id, key_index, upstream_model
		HAVING count(*) > 1)`).Scan(&dupGroups).Error)
	assert.Zero(t, dupGroups, "no duplicates may remain under the new unique key")
}

// TestCollapseRouteUnitGroupsKeepsEnabledWhenAnySiblingServes pins the enabled
// fold. A unit disabled in one group but serving in another is live, so the
// surviving row must stay enabled — silently disabling it would take real traffic
// off a channel the operator never retired.
func TestCollapseRouteUnitGroupsKeepsEnabledWhenAnySiblingServes(t *testing.T) {
	cleanup := withGroupScopedRouteDB(t)
	defer cleanup()

	seedGroupScopedRoute(t, "default", "gpt-5", 1, 0, 100, false)
	seedGroupScopedRoute(t, "vip", "gpt-5", 1, 0, 100, true)
	// A unit disabled everywhere stays disabled.
	seedGroupScopedRoute(t, "default", "gpt-4", 1, 0, 100, false)
	seedGroupScopedRoute(t, "vip", "gpt-4", 1, 0, 100, false)

	require.NoError(t, CollapseRouteUnitGroups())

	enabledOf := func(alias string) bool {
		var enabled bool
		require.NoError(t, DB.Raw(
			"SELECT enabled FROM channel_model_routes WHERE public_model_alias = ?", alias).
			Scan(&enabled).Error)
		return enabled
	}
	assert.True(t, enabledOf("gpt-5"), "serving in any group means the unit is live")
	assert.False(t, enabledOf("gpt-4"), "disabled everywhere stays disabled")
}

// TestCollapseRouteUnitGroupsIsIdempotent pins that a restart cannot fail on a
// column an earlier boot already removed.
func TestCollapseRouteUnitGroupsIsIdempotent(t *testing.T) {
	cleanup := withGroupScopedRouteDB(t)
	defer cleanup()

	seedGroupScopedRoute(t, "default", "gpt-5", 1, 0, 7, true)
	seedGroupScopedRoute(t, "vip", "gpt-5", 1, 0, 7, true)

	require.NoError(t, CollapseRouteUnitGroups())
	require.NoError(t, CollapseRouteUnitGroups(), "second run must be a clean no-op")

	var count int64
	require.NoError(t, DB.Table("channel_model_routes").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

// TestCollapseRouteUnitGroupsSkippedWithoutTable covers a fresh install, where the
// table does not exist yet and the migration must no-op instead of erroring and
// blocking the boot.
func TestCollapseRouteUnitGroupsSkippedWithoutTable(t *testing.T) {
	previous := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	defer func() { DB = previous }()

	require.False(t, tableExists("channel_model_routes"))
	require.NoError(t, CollapseRouteUnitGroups())
}
