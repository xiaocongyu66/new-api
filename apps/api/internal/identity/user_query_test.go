package identity

import (
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScopedQueriesKeepSoftDeletePredicate is the guard for these accessors'
// reason to exist. User and Token carry gorm.DeletedAt, so a query built through
// them must exclude soft-deleted rows. Targeting the table by name instead
// (tx.Table("users")) drops that predicate, which would let quota credits and
// subscription updates land on deleted accounts — with no compile error and no
// other test failing.
func TestScopedQueriesKeepSoftDeletePredicate(t *testing.T) {
	dryRun, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)

	for _, tc := range []struct {
		name  string
		build func(*gorm.DB) *gorm.DB
		table string
	}{
		{"user", UserQuery, "users"},
		{"token", TokenQuery, "tokens"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sql := tc.build(dryRun.Session(&gorm.Session{})).
				Where("id = ?", 1).
				Update("quota", 5).
				Statement.SQL.String()

			assert.Contains(t, sql, tc.table, "must target the record's table")
			assert.Contains(t, strings.ToLower(sql), "deleted_at",
				"soft-delete predicate must survive; Table() would drop it")
		})
	}
}
