package perfsource

// 测试公共脚手架：一个内存库（提供 channel_model_routes 给别名反查）和
// 放开回环地址的取数开关（httptest 用临时端口）。

import (
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/egress"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withPerfDB(t *testing.T) {
	t.Helper()
	previous := dbx.DB
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&channelpkg.ChannelModelRoute{}))
	dbx.DB = db
	t.Cleanup(func() { dbx.DB = previous })
}

// allowLoopback 让 SSRF 防护放行本机回环，好让 httptest 的临时端口可达。
func allowLoopback(t *testing.T) {
	t.Helper()
	setting := egress.GetFetchSetting()
	original := *setting
	t.Cleanup(func() { *setting = original })
	setting.EnableSSRFProtection = true
	setting.AllowPrivateIp = true
	setting.AllowedPorts = nil
}
