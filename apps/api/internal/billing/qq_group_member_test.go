package billing_test

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// setupGroupMemberTestDB 建一个内存 sqlite 并迁移群成员档案表。
// 返回的 cleanup 应被 defer 调用，还原全局 DB。
func setupGroupMemberTestDB(t *testing.T) func() {
	t.Helper()
	prevDB, prevLogDB, prevRedis := dbx.DB, dbx.LogDB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&billing.QQGroupMember{}))
	dbx.DB, dbx.LogDB = db, db
	common.RedisEnabled = false
	return func() {
		dbx.DB, dbx.LogDB, common.RedisEnabled = prevDB, prevLogDB, prevRedis
	}
}

// withCleanupSetting 临时改清理配置，测完还原。
func withCleanupSetting(t *testing.T, mutate func(s *billing.QQBotSetting)) {
	t.Helper()
	original := *billing.GetQQBotSetting()
	mutate(billing.GetQQBotSetting())
	// 用 t.Cleanup 而非 defer：本函数在断言前返回，defer 会过早还原。
	t.Cleanup(func() { *billing.GetQQBotSetting() = original })
}

const testGroupOpenID = "GROUP_TEST"

// inactiveMembers 构造若干不同最后活跃时间的成员档案。
func seedMembers(t *testing.T, now int64, daysAgo ...int) {
	t.Helper()
	const day = int64(86400)
	for i, d := range daysAgo {
		m := billing.QQGroupMember{
			GroupOpenID:  testGroupOpenID,
			MemberOpenID: "MEMBER_" + string(rune('A'+i)),
			Username:     "user",
			LastActiveAt: now - int64(d)*day,
			Status:       billing.MemberStatusActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		require.NoError(t, dbx.DB.Create(&m).Error)
	}
}

// TestListInactiveMembersZeroLimitAllRows 回归：limit 0 的语义是「不限制」，
// 但 GORM 的 Limit(0) 会生成 LIMIT 0 返回空集。扫描传 0 时必须能拿到全部候选，
// 否则整个清理功能静默失效（扫描数为 0，看起来一切正常）。
func TestListInactiveMembersZeroLimitAllRows(t *testing.T) {
	cleanup := setupGroupMemberTestDB(t)
	defer cleanup()

	now := int64(1700000000)
	seedMembers(t, now, 10, 20, 30, 5)
	beforeTs := now - 7*86400 // 阈值 7 天：10/20/30 天前活跃的都算潜水

	members, err := billing.ListInactiveMembers(testGroupOpenID, beforeTs, 0)
	require.NoError(t, err)
	assert.Len(t, members, 3, "limit=0 必须返回全部候选，不能返回空集")

	// limit>0 时正常截断
	members, err = billing.ListInactiveMembers(testGroupOpenID, beforeTs, 2)
	require.NoError(t, err)
	assert.Len(t, members, 2)

	// 最老的潜水户排在最前，分批处理时优先清理
	members, err = billing.ListInactiveMembers(testGroupOpenID, beforeTs, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(now-30*86400), members[0].LastActiveAt)
}

// TestTouchGroupMemberUpsert 首次发言建档，已有行只更新活跃时间。
func TestTouchGroupMemberUpsert(t *testing.T) {
	cleanup := setupGroupMemberTestDB(t)
	defer cleanup()

	now := int64(1700000000)
	require.NoError(t, billing.TouchGroupMember(testGroupOpenID, "MEMBER_A", "Alice", now))

	var m billing.QQGroupMember
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_A").First(&m).Error)
	assert.Equal(t, "Alice", m.Username)
	assert.Equal(t, now, m.LastActiveAt)
	assert.Equal(t, billing.MemberStatusActive, m.Status)

	// 再次发言：更新活跃时间与昵称，不新建行
	require.NoError(t, billing.TouchGroupMember(testGroupOpenID, "MEMBER_A", "Alice2", now+600))
	var count int64
	require.NoError(t, dbx.DB.Model(&billing.QQGroupMember{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_A").First(&m).Error)
	assert.Equal(t, "Alice2", m.Username)
	assert.Equal(t, now+600, m.LastActiveAt)

	// 空 openid 不建档，避免脏数据
	require.NoError(t, billing.TouchGroupMember("", "MEMBER_B", "Bob", now))
	require.NoError(t, billing.TouchGroupMember(testGroupOpenID, "", "Bob", now))
	require.NoError(t, dbx.DB.Model(&billing.QQGroupMember{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	// ts<=0 时用当前时间兜底
	require.NoError(t, billing.TouchGroupMember(testGroupOpenID, "MEMBER_C", "Carol", 0))
	// 注意必须用新变量：First(&m) 会把 m 里已有的主键追加为查询条件，
	// 复用上一轮的 m（Id=1）会多出 WHERE id=1，查不到新成员
	var c billing.QQGroupMember
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_C").First(&c).Error)
	assert.True(t, c.LastActiveAt > 0)
}

// TestTouchGroupMemberResurrect 被踢出的成员重新发言，状态必须回到 active——
// 他确实又在群里了，下次清理应重新走「先警告」流程而不是直接踢。
func TestTouchGroupMemberResurrect(t *testing.T) {
	cleanup := setupGroupMemberTestDB(t)
	defer cleanup()

	now := int64(1700000000)
	m := billing.QQGroupMember{
		GroupOpenID:  testGroupOpenID,
		MemberOpenID: "MEMBER_A",
		LastActiveAt: now - 60*86400,
		WarnedAt:     now - 50*86400,
		Status:       billing.MemberStatusRemoved,
		QQNumber:     12345,
	}
	require.NoError(t, dbx.DB.Create(&m).Error)

	require.NoError(t, billing.TouchGroupMember(testGroupOpenID, "MEMBER_A", "Alice", now))

	var got billing.QQGroupMember
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_A").First(&got).Error)
	assert.Equal(t, billing.MemberStatusActive, got.Status)
	assert.Equal(t, now, got.LastActiveAt)
	// 桥接来的 QQ 号不因复活而丢失
	assert.Equal(t, int64(12345), got.QQNumber)
}

// TestSetMemberStateTransitions 警告、踢出、回填 QQ 号三个状态写操作。
func TestSetMemberStateTransitions(t *testing.T) {
	cleanup := setupGroupMemberTestDB(t)
	defer cleanup()

	now := int64(1700000000)
	m := billing.QQGroupMember{
		GroupOpenID:  testGroupOpenID,
		MemberOpenID: "MEMBER_A",
		LastActiveAt: now - 60*86400,
		Status:       billing.MemberStatusActive,
		CreatedAt:    now,
	}
	require.NoError(t, dbx.DB.Create(&m).Error)

	require.NoError(t, billing.SetMemberWarned(testGroupOpenID, "MEMBER_A", now+1))
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_A").First(&m).Error)
	assert.Equal(t, billing.MemberStatusWarned, m.Status)
	assert.Equal(t, now+1, m.WarnedAt)

	require.NoError(t, billing.SetMemberQQNumber(testGroupOpenID, "MEMBER_A", 999888))
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_A").First(&m).Error)
	assert.Equal(t, int64(999888), m.QQNumber)

	require.NoError(t, billing.SetMemberRemoved(testGroupOpenID, "MEMBER_A"))
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_A").First(&m).Error)
	assert.Equal(t, billing.MemberStatusRemoved, m.Status)

	// 对不存在的成员操作不应报错（GORM Updates 零行Affected）
	require.NoError(t, billing.SetMemberWarned(testGroupOpenID, "NOPE", now))
}

// TestGetGroupMemberStats 状态计数与潜水候选计数。
func TestGetGroupMemberStats(t *testing.T) {
	cleanup := setupGroupMemberTestDB(t)
	defer cleanup()

	now := int64(1700000000)
	seedMembers(t, now, 10, 20, 3) // 3 个 active，其中 2 个超过 7 天
	require.NoError(t, dbx.DB.Model(&billing.QQGroupMember{}).
		Where("member_open_id = ?", "MEMBER_B"). // 20 天前那个标记成已警告
		Updates(map[string]any{"status": billing.MemberStatusWarned, "warned_at": now - 86400}).Error)

	stats, err := billing.GetGroupMemberStats(testGroupOpenID, now-7*86400)
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats[billing.MemberStatusActive])
	assert.Equal(t, int64(1), stats[billing.MemberStatusWarned])
	// 潜水候选只数 active 且超阈值的：10 天的算，3 天的不算
	assert.Equal(t, int64(1), stats["inactive_candidate"])
}

// TestHandleBridgeEventAtMatching @ 桥接核心契约：NapCat 收到官方 bot 发出
// 的同一条警告消息时，把消息里的真实 QQ 号回填到对应成员档案。
//
// 注意消歧规则：所有警告共用同一套文案模板，抹掉 @ 标签后正文相同，
// 因此多条「已警告未桥接」的记录并存时，按 warned_at DESC 取最近一条。
// 这依赖实际运行中警告是一条条发出、NapCat 按序收到回填的时序。
func TestHandleBridgeEventAtMatching(t *testing.T) {
	cleanup := setupGroupMemberTestDB(t)
	defer cleanup()
	withCleanupSetting(t, func(s *billing.QQBotSetting) {
		s.CleanupEnabled = true
		s.CleanupGroups = testGroupOpenID
		s.CleanupInactiveDays = 30
		s.CleanupGraceDays = 7
		s.CleanupWarningTemplate = "{@} 已 {天数} 天未发言，{宽限天数} 天内请回来"
	})

	now := int64(1700000000)
	require.NoError(t, dbx.DB.Create(&billing.QQGroupMember{
		GroupOpenID: testGroupOpenID, MemberOpenID: "MEMBER_A",
		LastActiveAt: now - 60*86400, WarnedAt: now - 100, Status: billing.MemberStatusWarned,
	}).Error)
	require.NoError(t, dbx.DB.Create(&billing.QQGroupMember{
		GroupOpenID: testGroupOpenID, MemberOpenID: "MEMBER_B",
		LastActiveAt: now - 60*86400, WarnedAt: now - 50, Status: billing.MemberStatusWarned,
	}).Error)

	// NapCat 侧看到的警告消息（正文与官方侧一致，@ 换成真实 QQ 号）
	napcatMsg := "[CQ:at,qq=123456] 已 30 天未发言，7 天内请回来"
	billing.HandleBridgeEvent(testGroupOpenID, napcatMsg)

	var a, b billing.QQGroupMember
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_A").First(&a).Error)
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_B").First(&b).Error)
	assert.Equal(t, int64(0), a.QQNumber)
	assert.Equal(t, int64(123456), b.QQNumber, "两条正文相同，回填到 warned_at 最近的一条")

	// B 已回填退出候选，再收到同类消息时轮到 A 被回填
	billing.HandleBridgeEvent(testGroupOpenID, "[CQ:at,qq=777] 已 30 天未发言，7 天内请回来")
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_B").First(&b).Error)
	assert.Equal(t, int64(123456), b.QQNumber, "已回填的号不能被覆盖")
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_A").First(&a).Error)
	assert.Equal(t, int64(777), a.QQNumber)
}

// TestHandleBridgeEventIgnoresUnrelated 群里其它的 @ 消息不能被误桥接。
func TestHandleBridgeEventIgnoresUnrelated(t *testing.T) {
	cleanup := setupGroupMemberTestDB(t)
	defer cleanup()
	withCleanupSetting(t, func(s *billing.QQBotSetting) {
		s.CleanupEnabled = true
		s.CleanupGroups = testGroupOpenID
		s.CleanupInactiveDays = 30
		s.CleanupGraceDays = 7
		s.CleanupWarningTemplate = "{@} 已 {天数} 天未发言"
	})

	now := int64(1700000000)
	require.NoError(t, dbx.DB.Create(&billing.QQGroupMember{
		GroupOpenID: testGroupOpenID, MemberOpenID: "MEMBER_A",
		LastActiveAt: now - 60*86400, WarnedAt: now - 100, Status: billing.MemberStatusWarned,
	}).Error)

	// 正文不同的 @ 消息：不回填
	billing.HandleBridgeEvent(testGroupOpenID, "[CQ:at,qq=999] 今天天气真好")
	var m billing.QQGroupMember
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_A").First(&m).Error)
	assert.Equal(t, int64(0), m.QQNumber)

	// 没有 @ 的消息：不回填
	billing.HandleBridgeEvent(testGroupOpenID, "已 30 天未发言")
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_A").First(&m).Error)
	assert.Equal(t, int64(0), m.QQNumber)

	// 未开启清理的群：不回填
	withCleanupSetting(t, func(s *billing.QQBotSetting) {
		s.CleanupEnabled = false
		s.CleanupGroups = testGroupOpenID
		s.CleanupWarningTemplate = "{@} 已 {天数} 天未发言"
	})
	billing.HandleBridgeEvent(testGroupOpenID, "[CQ:at,qq=888] 已 30 天未发言")
	require.NoError(t, dbx.DB.Where("member_open_id = ?", "MEMBER_A").First(&m).Error)
	assert.Equal(t, int64(0), m.QQNumber)
}
