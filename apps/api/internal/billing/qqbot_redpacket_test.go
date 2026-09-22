package billing_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
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
	createTestRedPacket(t, "GROUP_1", "OPENID_C", 2, 2, billing.RedPacketStatusExpired, 3000)

	content, kb := billing.HandleRedPacketList("GROUP_1")
	// 分段展示：可领取段不再重复“进行中”，已结束段保留状态
	assert.Contains(t, content, "**可领取**")
	assert.Contains(t, content, "2/5 份")
	assert.NotContains(t, content, "进行中")
	assert.Contains(t, content, "**已领取完**")
	assert.Contains(t, content, "3/3 份 · 已抢完")
	assert.Contains(t, content, "0/2 份 · 已过期")

	// 只有仍可抢的红包有单抢按钮;另挂一个一键领取按钮
	require.NotNil(t, kb)
	var grabButtons, grabAll int
	for _, row := range kb.Content.Rows {
		for _, btn := range row.Buttons {
			if btn.Action.Data == billing.ButtonDataRedPacketGrabAll {
				grabAll++
				continue
			}
			if strings.HasPrefix(btn.Action.Data, billing.ButtonDataRedPacketGrab) {
				grabButtons++
				assert.Equal(t, billing.ButtonDataRedPacketGrab+strconv.Itoa(active.Id), btn.Action.Data)
			}
		}
	}
	assert.Equal(t, 1, grabButtons, "只有进行中的红包应该带单抢按钮")
	assert.Equal(t, 1, grabAll, "可领取段应该挂一键领取按钮")
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

// setupGrabAllTestDB 在红包表之外再迁移 User + QQBinding，
// 一键领取需要真实的绑定用户和余额扣加。
func setupGrabAllTestDB(t *testing.T) func() {
	t.Helper()
	previousDB, previousLogDB, previousRedis := dbx.DB, dbx.LogDB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&billing.QQRedPacket{}, &billing.QQRedPacketGrab{},
		&identity.User{}, &identity.QQBinding{}))
	dbx.DB, dbx.LogDB = db, db
	common.RedisEnabled = false
	return func() {
		dbx.DB, dbx.LogDB = previousDB, previousLogDB
		common.RedisEnabled = previousRedis
		_ = sqlDB.Close()
	}
}

func TestHandleRedPacketGrabAllSummarizesOnce(t *testing.T) {
	defer setupGrabAllTestDB(t)()

	me := &identity.User{Username: "graball", Password: "x", Role: 1, Status: common.UserStatusEnabled, Group: "default", AffCode: "GAM1"}
	other := &identity.User{Username: "sender", Password: "x", Role: 1, Status: common.UserStatusEnabled, Group: "default", AffCode: "GAS2"}
	require.NoError(t, dbx.DB.Create(me).Error)
	require.NoError(t, dbx.DB.Create(other).Error)
	require.NoError(t, dbx.DB.Create(&identity.QQBinding{UserId: me.Id, OpenID: "OPENID_ME"}).Error)

	claimable := createTestRedPacket(t, "GROUP_GA", "OPENID_OTHER", 5, 3, billing.RedPacketStatusActive, 1000)
	own := createTestRedPacket(t, "GROUP_GA", "OPENID_ME", 4, 2, billing.RedPacketStatusActive, 1100)
	expireAt := time.Now().Add(time.Hour).Unix()
	require.NoError(t, dbx.DB.Model(&billing.QQRedPacket{}).Where("id IN ?", []int{claimable.Id, own.Id}).
		Updates(map[string]any{"sender_user_id": 0, "expire_at": expireAt}).Error)
	require.NoError(t, dbx.DB.Model(&billing.QQRedPacket{}).Where("id = ?", claimable.Id).
		Update("sender_user_id", other.Id).Error)
	require.NoError(t, dbx.DB.Model(&billing.QQRedPacket{}).Where("id = ?", own.Id).
		Update("sender_user_id", me.Id).Error)

	// 第一次：领到别人的 1 个，自己的被跳过，只回一条汇总
	first := billing.HandleRedPacketGrabAll("GROUP_GA", "OPENID_ME")
	assert.Contains(t, first, "一键领取 **1** 个红包")
	assert.Contains(t, first, "自己发的 1 个")
	assert.NotContains(t, first, "一键领取 **2** 个红包")
	assert.Contains(t, first, "你的余额")

	// 第二次：都不可再领，说明跳过原因而不是逐个报错刷屏
	second := billing.HandleRedPacketGrabAll("GROUP_GA", "OPENID_ME")
	assert.Contains(t, second, "没能领取到红包")
	assert.Contains(t, second, "已抢过 1 个")
	assert.Contains(t, second, "自己发的 1 个")

	// 没有可领红包的群
	empty := billing.HandleRedPacketGrabAll("GROUP_NONE", "OPENID_ME")
	assert.Contains(t, empty, "没有可领取的红包")
}
