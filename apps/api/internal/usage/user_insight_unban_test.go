package usage

// 回归：管理员解封走 /api/user/manage 的 action=enable，但 autoBanOnce 标记
// 没人清。被解封的用户在进程存活期内永远不会再进自动封禁评估——解封一次，
// 后续再违规也不封。本测试钉住这条接线：解封后该用户必须重新可评估。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/testutil"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAdminUnbanClearsAutoBanMark(t *testing.T) {
	previousDB, previousLogDB := dbx.DB, dbx.LogDB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	dbx.InitColumns()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	dbx.DB, dbx.LogDB = db, db
	common.RedisEnabled = false
	require.NoError(t, db.AutoMigrate(&identity.User{}, &identity.UserSession{}, &Log{}))

	t.Cleanup(func() {
		dbx.DB, dbx.LogDB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		dbx.InitColumns()
		common.RedisEnabled = previousRedis
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	target := identity.User{
		Username:    "unban-target",
		Password:    "not-a-real-hash",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusDisabled,
		Group:       "default",
		AuthVersion: 1,
	}
	require.NoError(t, db.Create(&target).Error)

	// 封禁瞬间 EvaluateInsightAutoBan 留下的进程内标记。
	autoBanOnce.Store(target.Id, struct{}{})
	t.Cleanup(func() { autoBanOnce.Delete(target.Id) })

	request := httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(
		fmt.Sprintf(`{"id":%d,"action":"enable"}`, target.Id)))
	request.Header.Set("Content-Type", "application/json")
	resp := testutil.ServeBufferedRoute(t, http.MethodPost, "/api/user/manage",
		[]contract.Middleware{rootOperatorStub}, identity.ManageUser, request)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	_, stillMarked := autoBanOnce.Load(target.Id)
	assert.False(t, stillMarked,
		"unban must clear the auto-ban mark, otherwise the user is never re-evaluated")

	var after identity.User
	require.NoError(t, db.First(&after, target.Id).Error)
	assert.Equal(t, common.UserStatusEnabled, after.Status)
}

// rootOperatorStub stands in for the AdminAuth chain: it stamps the operator
// identity the real middleware would have set.
func rootOperatorStub(c contract.Context) {
	c.Set("id", 9999)
	c.Set("role", common.RoleRootUser)
	c.Next()
}
