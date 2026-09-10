package billing_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupRedPacketTestDB opens an in-memory sqlite and migrates the red packet
// tables. Returns a cleanup func the caller should defer.
func setupRedPacketTestDB(t *testing.T) func() {
	t.Helper()
	previousDB, previousLogDB, previousRedis := dbx.DB, dbx.LogDB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&billing.QQRedPacket{}, &billing.QQRedPacketGrab{}))
	dbx.DB, dbx.LogDB = db, db
	common.RedisEnabled = false
	return func() {
		dbx.DB, dbx.LogDB = previousDB, previousLogDB
		common.RedisEnabled = previousRedis
		_ = sqlDB.Close()
	}
}

func createTestRedPacket(t *testing.T, group, sender string, total, remain int, status int, createdAt int64) *billing.QQRedPacket {
	t.Helper()
	p := &billing.QQRedPacket{
		SenderUserId:    1,
		SenderOpenID:    sender,
		GroupOpenID:     group,
		Blessing:        "测试红包",
		TotalAmount:     5000,
		TotalCount:      total,
		RemainingAmount: remain,
		RemainingCount:  remain,
		Status:          status,
		CreatedAt:       createdAt,
	}
	require.NoError(t, dbx.DB.Create(p).Error)
	return p
}

func TestHandleRedPacketListEmpty(t *testing.T) {
	defer setupRedPacketTestDB(t)()

	content, kb := billing.HandleRedPacketList("GROUP_EMPTY")
	assert.Contains(t, content, "还没有红包")
	assert.Contains(t, content, "/红包")
	require.NotNil(t, kb, "菜单回调必须返回键盘")
	require.NotNil(t, kb.Content)
	assert.NotEmpty(t, kb.Content.Rows)
}

func TestHandleRedPacketListShowsStatusAndGrabButtons(t *testing.T) {
	defer setupRedPacketTestDB(t)()

	active := createTestRedPacket(t, "GROUP_1", "OPENID_A", 5, 3, billing.RedPacketStatusActive, 1000)
	createTestRedPacket(t, "GROUP_1", "OPENID_B", 3, 0, billing.RedPacketStatusFinished, 2000)

	content, kb := billing.HandleRedPacketList("GROUP_1")
	// 倒序:expired 最新,active 最老
	assert.Contains(t, content, "0/2 份 · 已过期")
	assert.Contains(t, content, "3/3 份 · 已抢完")
	assert.Contains(t, content, "2/5 份 · 进行中")

	// 只有仍可抢的红包有抢按钮;已抢完/已过期只有文案
	require.NotNil(t, kb)
	var grabButtons int
	for _, row := range kb.Content.Rows {
		for _, btn := range row.Buttons {
			if strings.HasPrefix(btn.Action.Data, billing.ButtonDataRedPacketGrab) {
				grabButtons++
				assert.Equal(t, billing.ButtonDataRedPacketGrab+strconv.Itoa(active.Id), btn.Action.Data)
			}
		}
	}
	assert.Equal(t, 1, grabButtons, "只有进行中的红包应该带抢按钮")
}

func TestGetGroupRedPacketsOrderAndLimit(t *testing.T) {
	defer setupRedPacketTestDB(t)()

	createTestRedPacket(t, "GROUP_ORDER", "OPENID_A", 2, 2, billing.RedPacketStatusActive, 100)
	createTestRedPacket(t, "GROUP_ORDER", "OPENID_B", 2, 2, billing.RedPacketStatusActive, 200)
	createTestRedPacket(t, "GROUP_ORDER", "OPENID_C", 2, 2, billing.RedPacketStatusActive, 300)
	// 其他群的红包不应混入
	createTestRedPacket(t, "GROUP_OTHER", "OPENID_D", 2, 2, billing.RedPacketStatusActive, 400)

	packets, err := billing.GetGroupRedPackets("GROUP_ORDER", 2)
	require.NoError(t, err)
	require.Len(t, packets, 2)
	assert.Equal(t, "OPENID_C", packets[0].SenderOpenID, "应按发出时间倒序")
	assert.Equal(t, "OPENID_B", packets[1].SenderOpenID)

	all, err := billing.GetGroupRedPackets("GROUP_ORDER", 0)
	require.NoError(t, err)
	assert.Len(t, all, 3, "limit<=0 时使用默认值 10")
}
