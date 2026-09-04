package dbx

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

// lockRow is a stand-in record: the helper only shapes the emitted clause, so the
// table it targets is irrelevant, and a local type keeps this package free of any
// domain import.
type lockRow struct {
	Id int
}

// TestLockForUpdateEmitsRowLock asserts LockForUpdate emits FOR UPDATE on the
// engines that support it and skips it on SQLite, where the clause is a syntax
// error.
//
// The dummy dialector is used because SQLite drivers strip locking clauses from
// the generated SQL, which would mask what the helper itself does.
func TestLockForUpdateEmitsRowLock(t *testing.T) {
	dummyDB, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	buildSQL := func() string {
		var rows []lockRow
		return LockForUpdate(dummyDB).Where("id = ?", 1).Find(&rows).Statement.SQL.String()
	}

	t.Cleanup(func() {
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	})

	common.SetDatabaseTypes(common.DatabaseTypeMySQL, common.DatabaseTypeSQLite)
	assert.Contains(t, buildSQL(), "FOR UPDATE")

	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypeSQLite)
	assert.Contains(t, buildSQL(), "FOR UPDATE")

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	assert.NotContains(t, buildSQL(), "FOR UPDATE")
}

// TestInitColumnsQuotesReservedWordsPerDialect pins the reserved-word quoting:
// PostgreSQL needs "group"/"key" while MySQL and SQLite need backticks. Emitting
// the wrong form is a syntax error on the other engine, and AGENTS.md requires
// all three databases keep working.
func TestInitColumnsQuotesReservedWordsPerDialect(t *testing.T) {
	t.Cleanup(func() {
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
		InitColumns()
	})

	common.SetDatabaseTypes(common.DatabaseTypePostgreSQL, common.DatabaseTypePostgreSQL)
	InitColumns()
	assert.Equal(t, `"group"`, GroupCol())
	assert.Equal(t, `"key"`, KeyCol())
	assert.Equal(t, "true", TrueVal())
	assert.Equal(t, "false", FalseVal())
	assert.Equal(t, `"group"`, LogGroupCol())
	assert.Equal(t, `"key"`, LogKeyCol())

	common.SetDatabaseTypes(common.DatabaseTypeMySQL, common.DatabaseTypeMySQL)
	InitColumns()
	assert.Equal(t, "`group`", GroupCol())
	assert.Equal(t, "`key`", KeyCol())
	assert.Equal(t, "1", TrueVal())
	assert.Equal(t, "0", FalseVal())
	assert.Equal(t, "`group`", LogGroupCol())
	assert.Equal(t, "`key`", LogKeyCol())
}
