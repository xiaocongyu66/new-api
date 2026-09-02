package usage

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils/tests"
)

// TestPerfMetricQueriesQuoteGroupPerDialect pins the quoting of the reserved
// word "group" in the perf metric queries.
//
// Both queries used to hardcode MySQL backticks, which PostgreSQL rejects
// outright: GET /api/perf-metrics/summary and GET /api/perf-metrics?group=...
// answered 500 with `syntax error at or near "group"` on every PostgreSQL
// deployment. Only the group filter carried the bad quoting, which is why the
// unfiltered GET /api/perf-metrics?model=... kept working and hid the break.
//
// The emitted SQL is asserted rather than a query result because the failure is
// a dialect syntax error: SQLite accepts the backticks, so a round trip through
// it would pass while PostgreSQL stayed broken.
func TestPerfMetricQueriesQuoteGroupPerDialect(t *testing.T) {
	previousDB := dbx.DB
	t.Cleanup(func() {
		dbx.DB = previousDB
		common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
		dbx.InitColumns()
	})

	for _, tc := range []struct {
		name     string
		database common.DatabaseType
		want     string
		reject   string
	}{
		{name: "postgres", database: common.DatabaseTypePostgreSQL, want: `"group"`, reject: "`group`"},
		{name: "mysql", database: common.DatabaseTypeMySQL, want: "`group`", reject: `"group"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.SetDatabaseTypes(tc.database, tc.database)
			dbx.InitColumns()

			var captured string
			dbx.DB = dryRunDB(t, &captured)

			_, _ = GetPerfMetricsSummaryBucketsAll(0, 1, []string{"default"})
			require.NotEmpty(t, captured, "summary query built no statement")
			assert.Contains(t, captured, tc.want, "summary query must quote group for %s", tc.name)
			assert.NotContains(t, captured, tc.reject, "summary query must not emit the other dialect's quoting")

			captured = ""
			_, _ = GetPerfMetricsInternal("gpt-4", "default", 0, 1)
			require.NotEmpty(t, captured, "group filter built no statement")
			assert.Contains(t, captured, tc.want, "group filter must quote group for %s", tc.name)
			assert.NotContains(t, captured, tc.reject, "group filter must not emit the other dialect's quoting")
		})
	}
}

// dryRunDB renders SQL without executing it and writes each rendered statement
// to sink.
//
// The dialect fragments under test are injected as raw text by dbx.GroupCol, so
// the dummy dialector's own quoting is irrelevant here; what matters is that
// observing them needs no live server.
func dryRunDB(t *testing.T, sink *string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{
		DryRun: true,
		Logger: logger.Discard,
	})
	require.NoError(t, err)
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(
		"test:capture_sql",
		func(tx *gorm.DB) { *sink = tx.Statement.SQL.String() },
	))
	return db
}
