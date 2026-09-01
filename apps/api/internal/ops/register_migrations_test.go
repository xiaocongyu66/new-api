package ops

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/dbinfra"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMigration(t *testing.T) dbx.Migration {
	t.Helper()
	for _, migration := range dbx.Migrations() {
		if migration.Name == "Setup" {
			require.IsType(t, &dbinfra.Setup{}, migration.Model)
			return migration
		}
	}
	t.Fatal("Setup migration is not registered")
	return dbx.Migration{}
}

func TestSetupMigrationCreatesAndPreservesPersistedSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previous := dbx.DB
	dbx.DB = db
	t.Cleanup(func() { dbx.DB = previous })

	migration := setupMigration(t)
	require.NoError(t, db.AutoMigrate(migration.Model))
	require.True(t, db.Migrator().HasColumn("setups", "version"))
	require.True(t, db.Migrator().HasColumn("setups", "initialized_at"))

	initializedAt := time.Now().Unix()
	require.NoError(t, db.Create(&dbinfra.Setup{Version: "v1.0.0", InitializedAt: initializedAt}).Error)
	setup := dbinfra.GetSetup()
	require.NotNil(t, setup)
	require.Equal(t, "v1.0.0", setup.Version)
	require.Equal(t, initializedAt, setup.InitializedAt)

	require.NoError(t, db.AutoMigrate(migration.Model))
	require.True(t, db.Migrator().HasColumn("setups", "version"))
	require.True(t, db.Migrator().HasColumn("setups", "initialized_at"))
	var count int64
	require.NoError(t, db.Model(&dbinfra.Setup{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}
