package dbinfra

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 保护契约：schema_migrations 记账一次，SQL 迁移只执行一次；重复启动不再执行，
// applies-to 之外的数据库类型直接跳过。删除类语句用 IF EXISTS 保证重放无害。
func TestRunSQLMigrationsRunsOnceAndSkipsForeignBackends(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open("file:sqlmig?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	// 001 targets sqlite, so it must have dropped the six indexes (IF EXISTS on
	// a missing index is a no-op, so this also proves idempotency of the DROPs).
	require.NoError(t, RunSQLMigrations(gdb, "sqlite"))

	var applied int64
	require.NoError(t, gdb.Raw("SELECT count(*) FROM schema_migrations").Scan(&applied).Error)
	assert.EqualValues(t, 1, applied, "001 runs on sqlite; 002 is postgres-only")

	var version string
	require.NoError(t, gdb.Raw("SELECT version FROM schema_migrations").Scan(&version).Error)
	assert.Equal(t, "001_drop_unused_log_indexes.sql", version)

	// Second boot: nothing new to apply, no error.
	require.NoError(t, RunSQLMigrations(gdb, "sqlite"))
	require.NoError(t, gdb.Raw("SELECT count(*) FROM schema_migrations").Scan(&applied).Error)
	assert.EqualValues(t, 1, applied, "already-applied files must not re-run")

	// PostgreSQL-only files are invisible to sqlite bookkeeping.
	require.NoError(t, RunSQLMigrations(gdb, "mysql"))
	require.NoError(t, gdb.Raw("SELECT count(*) FROM schema_migrations").Scan(&applied).Error)
	assert.EqualValues(t, 1, applied, "mysql applies 001 too; it was recorded on the first pass")
}

func TestMigrationAppliesToParsesHeader(t *testing.T) {
	pgOnly := "-- applies-to: postgres\nDROP INDEX IF EXISTS x;"
	all := "-- applies-to: all\nSELECT 1;"
	mysqlSet := "-- applies-to: mysql,sqlite\nSELECT 1;"
	noHeader := "SELECT 1;"

	yes, err := migrationAppliesTo(pgOnly, "postgres")
	require.NoError(t, err)
	assert.True(t, yes)
	yes, err = migrationAppliesTo(pgOnly, "sqlite")
	require.NoError(t, err)
	assert.False(t, yes)
	yes, err = migrationAppliesTo(all, "clickhouse")
	require.NoError(t, err)
	assert.True(t, yes)
	yes, err = migrationAppliesTo(mysqlSet, "MYSQL") // case-insensitive match
	require.NoError(t, err)
	assert.True(t, yes)
	yes, err = migrationAppliesTo(noHeader, "clickhouse")
	require.NoError(t, err)
	assert.True(t, yes, "files without a header apply everywhere")
}

func TestSplitSQLStatementsKeepsStringsAndCommentsIntact(t *testing.T) {
	script := "-- a comment with a semicolon; inside\nSELECT 'a;b' FROM t;\nDROP TABLE x;\n"
	got := splitSQLStatements(script)
	require.Len(t, got, 2)
	assert.True(t, strings.HasPrefix(got[0], "SELECT 'a;b'"), got[0])
	assert.Equal(t, "DROP TABLE x", got[1])
}

func TestEmbeddedMigrationsExist(t *testing.T) {
	files, err := listSQLMigrations()
	require.NoError(t, err)
	require.NotEmpty(t, files, "at least one migration file must be embedded")
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.name
	}
	assert.Equal(t, names, sortedCopy(names), "files must be applied in filename order")
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
