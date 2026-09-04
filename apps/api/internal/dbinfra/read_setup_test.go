package dbinfra

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withSQLite(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previous := dbx.DB
	dbx.DB = db
	t.Cleanup(func() { dbx.DB = previous })
	require.NoError(t, db.AutoMigrate(&Setup{}))
}

func TestGetSetupReturnsNilWhenEmpty(t *testing.T) {
	withSQLite(t)
	assert.Nil(t, GetSetup())
}

func TestGetSetupReturnsRowAfterInsert(t *testing.T) {
	withSQLite(t)
	now := time.Now().Unix()
	inserted := Setup{Version: "v1.0.0", InitializedAt: now}
	require.NoError(t, dbx.DB.Create(&inserted).Error)
	row := GetSetup()
	require.NotNil(t, row)
	assert.Equal(t, "v1.0.0", row.Version)
	assert.Equal(t, now, row.InitializedAt)
}
