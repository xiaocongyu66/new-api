package dbinfra_test

// The transform -> validate contract of MigrateRetiredFrontendOptions can only
// be asserted with a real OnValidateConsoleSettings installed. usage owns that
// validator and imports dbinfra, so the registration lives in an external test
// package (same pattern as internal/settings/validate_channel_health_option_test.go).
// In the in-package tests the hook is nil and every validation branch is a no-op.

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/dbinfra"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// A legacy value that survives the transform (valid JSON, under the cap) but
// fails the console-settings validator must not reach its target key: the
// legacy option stays for a human to fix, and the migration reports no error
// so one bad setting cannot block the others.
func TestMigrateRetiredFrontendOptionsRejectsValuesFailingRealValidator(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&dbinfra.Option{}))

	previousDB, previousType := dbx.DB, common.MainDatabaseType()
	previousValidator := dbinfra.OnValidateConsoleSettings
	dbx.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	dbinfra.OnValidateConsoleSettings = usage.ValidateConsoleSettings
	t.Cleanup(func() {
		dbx.DB = previousDB
		common.SetMainDatabaseType(previousType)
		dbinfra.OnValidateConsoleSettings = previousValidator
	})

	// Shape the transform accepts (a JSON array of objects) but the validator
	// rejects: an API info entry without route/description/color.
	incomplete := `[{"url":"https://api.example.com"}]`
	require.NoError(t, db.Create(&dbinfra.Option{Key: "ApiInfo", Value: incomplete}).Error)

	require.NoError(t, dbinfra.MigrateRetiredFrontendOptions())

	var source dbinfra.Option
	require.NoError(t, db.Where(&dbinfra.Option{Key: "ApiInfo"}).First(&source).Error)
	assert.Equal(t, incomplete, source.Value, "a value the validator rejects must be left untouched")

	err = db.Where(&dbinfra.Option{Key: "console_setting.api_info"}).First(&dbinfra.Option{}).Error
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound, "an invalid payload must not reach the target key")
}
