package usage

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 保护契约：TimescaleDB 会给时间分区列强制加上 NOT NULL。若 Log.CreatedAt 缺少
// `not null`，AutoMigrate 每次重启都会发出 `ALTER COLUMN created_at DROP NOT NULL`，
// 超表拒绝该语句（SQLSTATE TS101）并让启动 fatal。此测试断言迁移后的列本身不可为空，
// 因此重启不会再产生那条 DDL。
func TestLogCreatedAtIsNotNullAfterMigrate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:log_notnull?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))

	columns, err := db.Migrator().ColumnTypes(&Log{})
	require.NoError(t, err)

	var found bool
	for _, column := range columns {
		if column.Name() != "created_at" {
			continue
		}
		found = true
		nullable, ok := column.Nullable()
		require.True(t, ok, "driver must report nullability for created_at")
		assert.False(t, nullable, "created_at must be NOT NULL so AutoMigrate never emits DROP NOT NULL")
	}
	require.True(t, found, "migrated logs table must have a created_at column")
}
