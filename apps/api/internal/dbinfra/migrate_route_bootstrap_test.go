package dbinfra_test

// These tests prove the DB bootstrap actually WIRES the route-unit migration,
// not merely that the individual steps work in isolation.
//
// The package is external (dbinfra_test) on purpose: the assertions need the
// catalog domain, and catalog imports dbinfra, so an in-package test would be an
// import cycle. Importing catalog here also installs its init()-registered
// migrations and post-migrations, which is precisely what is under test — the
// registrations only reach the bootstrap through those inits.
//
// Each test drives dbinfra.InitDB(), the real production entry point, rather than
// calling the migration steps directly. That is what makes them fail if a call
// site is removed: deleting the ChannelModelRoute registration, the seed hook, or
// the pre-migration hook breaks these without touching the step implementations.

import (
	"fmt"
	"os"
	"strings"
	"testing"

	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/dbinfra"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// bootstrapSQLitePath gives each test its own shared-cache in-memory database and
// restores every global InitDB touches. InitDB is a process-level bootstrap, so
// the environment has to be put back or sibling tests inherit it.
func bootstrapSQLitePath(t *testing.T) string {
	t.Helper()

	path := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))

	originalIsMasterNode := common.IsMasterNode
	originalSQLitePath := common.SQLitePath
	originalMain := common.MainDatabaseType()
	originalLog := common.LogDatabaseType()
	originalDB, originalLogDB := dbx.DB, dbx.LogDB
	originalDSN, hadDSN := os.LookupEnv("SQL_DSN")

	t.Cleanup(func() {
		if dbx.DB != nil {
			if sqlDB, err := dbx.DB.DB(); err == nil {
				_ = sqlDB.Close()
			}
		}
		common.IsMasterNode = originalIsMasterNode
		common.SQLitePath = originalSQLitePath
		common.SetDatabaseTypes(originalMain, originalLog)
		dbx.DB, dbx.LogDB = originalDB, originalLogDB
		if hadDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})

	// IsMasterNode must be true: InitDB returns before migrating on a slave, and
	// migration is the whole subject here.
	common.IsMasterNode = true
	common.SQLitePath = path
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))

	return path
}

func tableExists(t *testing.T, db *gorm.DB, table string) bool {
	t.Helper()
	var count int64
	require.NoError(t, db.Raw(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name = ?", table).Scan(&count).Error)
	return count > 0
}

func columnExists(t *testing.T, db *gorm.DB, table, column string) bool {
	t.Helper()
	var count int64
	require.NoError(t, db.Raw(
		"SELECT count(*) FROM pragma_table_info(?) WHERE name = ?", table, column).Scan(&count).Error)
	return count > 0
}

// TestMigrateCreatesAndSeedsRouteTable is the wiring proof for two call sites at
// once: the ChannelModelRoute AutoMigrate registration and the SeedChannelModelRoutes
// post-migration hook.
//
// A fresh migrate must leave the route table present AND populated for channels
// that already existed, because an upgrading instance has channels but no routes.
// Remove the registration and the table is missing; remove the seed hook and the
// table is empty. Either way this fails.
func TestMigrateCreatesAndSeedsRouteTable(t *testing.T) {
	path := bootstrapSQLitePath(t)

	// Pre-create a channel the way an existing deployment would have it, before
	// the route table exists at all.
	seed, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, seed.AutoMigrate(&catalog.Channel{}))
	require.NoError(t, seed.Create(&catalog.Channel{
		Id:     4101,
		Key:    "sk-existing",
		Models: "model-a,model-b",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}).Error)
	require.False(t, tableExists(t, seed, "channel_model_routes"),
		"fixture must start without the route table")

	require.NoError(t, dbinfra.InitDB())

	require.True(t, tableExists(t, dbx.DB, "channel_model_routes"),
		"AutoMigrate must create channel_model_routes; the ChannelModelRoute registration is what does it")

	var routes []catalog.ChannelModelRoute
	require.NoError(t, dbx.DB.Where("channel_id = ?", 4101).Find(&routes).Error)
	require.Len(t, routes, 2,
		"the startup seed must expand the pre-existing channel's two models into route units")

	aliases := map[string]bool{}
	for _, route := range routes {
		aliases[route.PublicModelAlias] = true
		assert.Equal(t, "default", route.Group)
		assert.True(t, route.Enabled)
	}
	assert.Equal(t, map[string]bool{"model-a": true, "model-b": true}, aliases)
}

// TestMigrateCarriesTunedChannelWeightOntoRoutes is the upgrade-safety proof, and
// the wiring proof for the third call site: the dropLegacySchedulingColumns
// pre-migration hook.
//
// It reproduces a real upgrading SQLite deployment: populated channels.weight,
// populated abilities.weight/priority, and the indexes GORM generated from the
// `index` tags those fields used to carry. That instance must boot clean, land the
// operator's tuned weight on its route rows, and end with the legacy columns gone.
//
// The indexes matter: SQLite refuses to drop a column an index still references,
// so without explicit index cleanup this boot fails outright.
func TestMigrateCarriesTunedChannelWeightOntoRoutes(t *testing.T) {
	path := bootstrapSQLitePath(t)

	seed, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, seed.AutoMigrate(&catalog.Channel{}, &catalog.Ability{}))

	// Re-add the retired columns and their indexes: the structs no longer declare
	// them, so an upgrading instance is simulated through raw DDL.
	require.NoError(t, seed.Exec("ALTER TABLE channels ADD COLUMN weight integer DEFAULT 0").Error)
	require.NoError(t, seed.Exec("ALTER TABLE channels ADD COLUMN priority integer DEFAULT 0").Error)
	require.NoError(t, seed.Exec("ALTER TABLE abilities ADD COLUMN weight integer DEFAULT 0").Error)
	require.NoError(t, seed.Exec("ALTER TABLE abilities ADD COLUMN priority integer DEFAULT 0").Error)
	require.NoError(t, seed.Exec("CREATE INDEX idx_abilities_weight ON abilities(weight)").Error)
	require.NoError(t, seed.Exec("CREATE INDEX idx_abilities_priority ON abilities(priority)").Error)

	// The operator tuned this channel to weight 25; channel 4202 was left alone.
	require.NoError(t, seed.Exec(
		"INSERT INTO channels (id, `key`, models, `group`, status, weight, priority) VALUES (?, ?, ?, ?, ?, ?, ?)",
		4201, "sk-tuned", "model-a", "default", common.ChannelStatusEnabled, 25, 3).Error)
	require.NoError(t, seed.Exec(
		"INSERT INTO channels (id, `key`, models, `group`, status, weight, priority) VALUES (?, ?, ?, ?, ?, ?, ?)",
		4202, "sk-default", "model-a", "default", common.ChannelStatusEnabled, 0, 0).Error)
	require.NoError(t, seed.Exec(
		"INSERT INTO abilities (`group`, model, channel_id, enabled, weight, priority) VALUES (?, ?, ?, ?, ?, ?)",
		"default", "model-a", 4201, true, 25, 3).Error)

	require.NoError(t, dbinfra.InitDB())

	// The legacy columns and their indexes are gone.
	assert.False(t, columnExists(t, dbx.DB, "channels", "weight"))
	assert.False(t, columnExists(t, dbx.DB, "channels", "priority"))
	assert.False(t, columnExists(t, dbx.DB, "abilities", "weight"))
	assert.False(t, columnExists(t, dbx.DB, "abilities", "priority"))

	var indexNames []string
	require.NoError(t, dbx.DB.Raw(
		"SELECT name FROM sqlite_master WHERE type='index' AND tbl_name='abilities'").Scan(&indexNames).Error)
	assert.NotContains(t, indexNames, "idx_abilities_weight")
	assert.NotContains(t, indexNames, "idx_abilities_priority")

	// The tuned weight survived onto the route rows, and the untuned channel kept
	// the seed default. This is the carry-over's whole purpose: without it the
	// operator's distribution would silently reset to uniform.
	weightOf := func(channelID int) int {
		var weight int
		require.NoError(t, dbx.DB.Table("channel_model_routes").
			Where("channel_id = ? AND public_model_alias = ?", channelID, "model-a").
			Select("static_weight").Scan(&weight).Error)
		return weight
	}
	assert.Equal(t, 25, weightOf(4201),
		"the tuned channel weight must land on its route units before the column disappears")
	assert.Equal(t, 100, weightOf(4202),
		"a channel that was never tuned must stay at the route seed default")

	// The abilities table must still be writable: a surviving stale index would
	// make every ability insert fail after the drop.
	require.NoError(t, dbx.DB.Create(&catalog.Ability{
		Group:     "default",
		Model:     "model-b",
		ChannelId: 4202,
		Enabled:   true,
	}).Error)
}

// TestMigrateIsIdempotentAcrossRestarts pins that the wired bootstrap survives a
// second boot: the pre-migration finds no legacy columns, the seed finds every
// route already present, and neither errors or duplicates rows.
func TestMigrateIsIdempotentAcrossRestarts(t *testing.T) {
	path := bootstrapSQLitePath(t)

	seed, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, seed.AutoMigrate(&catalog.Channel{}))
	require.NoError(t, seed.Create(&catalog.Channel{
		Id:     4301,
		Key:    "sk-restart",
		Models: "model-a",
		Group:  "default",
		Status: common.ChannelStatusEnabled,
	}).Error)

	require.NoError(t, dbinfra.InitDB())

	var first int64
	require.NoError(t, dbx.DB.Model(&catalog.ChannelModelRoute{}).Count(&first).Error)
	require.Equal(t, int64(1), first)

	require.NoError(t, dbinfra.InitDB(), "a restart must not fail on already-migrated schema")

	var second int64
	require.NoError(t, dbx.DB.Model(&catalog.ChannelModelRoute{}).Count(&second).Error)
	assert.Equal(t, first, second, "reseeding must not duplicate route rows")
}
