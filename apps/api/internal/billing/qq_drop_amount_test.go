package billing

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// swapDBForDropTest 打开一个只含 users + qq_drops 的内存库供 SQLite 无事务路径使用。
func swapDBForDropTest(t *testing.T) func() {
	t.Helper()
	previousDB, previousLogDB := dbx.DB, dbx.LogDB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&identity.User{}, &QQDrop{}))
	dbx.DB, dbx.LogDB = db, db
	return func() { dbx.DB, dbx.LogDB = previousDB, previousLogDB }
}

// snapshotQQBotSetting 还原测试对全局配置结构体的改动。
func snapshotQQBotSetting() func() {
	before := *GetQQBotSetting()
	return func() { *GetQQBotSetting() = before }
}

// TestComputeDropAward 掉落额度计算：余额加权（锚点为 0 关闭）与每日保底（0 关闭）。
// 期望值全部为精确输出。
func TestComputeDropAward(t *testing.T) {
	const (
		unit  = 500000 // QuotaPerUnit
		limit = 3
	)
	cases := []struct {
		name       string
		drawn      int
		balance    int
		todayCount int
		weekSum    int64
		dailyLimit int
		anchor     int
		guarantee  int
		dropMin    int
		dropMax    int
		want       int
	}{
		{"anchor为0关闭加权", 1 * unit, 10 * unit, 0, 0, limit, 0, 0, 150000, 3 * unit, 1 * unit},
		{"余额低于锚点放大部分", 800000, 800000, 0, 0, limit, 1 * unit, 0, 150000, 3 * unit, 1 * unit},  // w=1.25
		{"权重上钳2.0", 2 * unit, 100000, 0, 0, limit, 2 * unit, 0, 150000, 5 * unit, 4 * unit},   // w 封顶 2
		{"权重下钳0.5", 1 * unit, 100 * unit, 0, 0, limit, 1 * unit, 0, 150000, 5 * unit, 250000}, // w 封底 0.5
		{"加权后夹回单发上限", 2 * unit, 100000, 0, 0, limit, 2 * unit, 0, 150000, 3 * unit, 3 * unit},
		{"加权后抬到单发下限", 1, 100000, 0, 0, limit, 4 * unit, 0, 150000, 3 * unit, 150000}, // w=2→2→clamp min
		{"余额为0跳过加权", 1 * unit, 0, 0, 0, limit, 1 * unit, 0, 150000, 3 * unit, 1 * unit},
		{"保底为0关闭", 1 * unit, 4 * unit, 2, 2 * unit, limit, 0, 0, 150000, 3 * unit, 1 * unit},
		{"最后一发补足到保底", 1 * unit, 8 * unit, 2, 2 * unit, limit, 0, 4 * unit, 150000, 3 * unit, 2 * unit},
		{"非最后一发不补足", 1 * unit, 8 * unit, 1, 1 * unit, limit, 0, 4 * unit, 150000, 3 * unit, 1 * unit},
		{"7日已超保底不动", 1 * unit, 8 * unit, 2, 3600000, limit, 0, 4 * unit, 150000, 3 * unit, 1 * unit},
		{"前几日已拿满当日不补", 1 * unit, 8 * unit, 0, 4 * unit, limit, 0, 4 * unit, 150000, 3 * unit, 1 * unit}, // 防日刷：weekSum 已含前 6 日入账
		{"保底优先于单发上限", 1 * unit, 8 * unit, 0, 0, 1, 0, 3 * unit, 150000, 2 * unit, 3 * unit},
		{"不限次时保底不生效", 1 * unit, 8 * unit, 5, 500000, 0, 0, 4 * unit, 150000, 3 * unit, 1 * unit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeDropAward(tc.drawn, tc.balance, tc.todayCount, tc.weekSum,
				tc.dailyLimit, tc.anchor, tc.guarantee, tc.dropMin, tc.dropMax)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestComputeDropAwardWeightThenGuarantee 加权后仍不足保底的，最后一次补足到保底线。
// anchor=4*unit、balance=unit → w=2.0（上钳），drawn=unit → 2*unit；
// 保底 3*unit、weekSum=0、limit=1 → 最终 3*unit。
func TestComputeDropAwardWeightThenGuarantee(t *testing.T) {
	got := computeDropAward(500000, 500000, 0, 0, 1, 2000000, 1500000, 150000, 2500000)
	require.Equal(t, 1500000, got) // w=2 得 1,000,000，仍低于保底 1,500,000，补足差额
}

// TestAwardQQDropGuaranteeEndToEnd SQLite 路径接线：7 日累计查询、补足与入账一致性。
func TestAwardQQDropGuaranteeEndToEnd(t *testing.T) {
	defer swapDBForDropTest(t)()
	defer snapshotQQBotSetting()()

	s := GetQQBotSetting()
	s.DropMinQuota = 150000
	s.DropMaxQuota = 1500000
	s.DropDailyLimit = 3
	s.DropBalanceAnchor = 0
	s.DropDailyGuarantee = 1000000

	user := &identity.User{Username: "drop-e2e", AffCode: "drop-e2e", Quota: 0}
	require.NoError(t, dbx.DB.Create(user).Error)

	today := time.Now().Format("2006-01-02")
	// 预置今日前两发，累计 300,000
	for _, q := range []int{150000, 150000} {
		require.NoError(t, dbx.DB.Create(&QQDrop{
			UserId: user.Id, DropDate: today, QuotaAwarded: q, CreatedAt: time.Now().Unix(),
		}).Error)
	}

	// 第三发（最后一次机会）drawn=200,000 → 补足到 1,000,000 - weekSum 300,000 = 700,000
	drop, err := AwardQQDrop(user.Id, "openid-x", "group-x", 200000, s.DropDailyLimit)
	require.NoError(t, err)
	assert.Equal(t, 700000, drop.QuotaAwarded)

	var balance int64
	require.NoError(t, dbx.DB.Model(&identity.User{}).Where("id = ?", user.Id).
		Select("quota").Scan(&balance).Error)
	assert.Equal(t, int64(700000), balance, "入账必须与实际发放额一致")

	// 第四次：次数刷满，拒绝
	_, err = AwardQQDrop(user.Id, "openid-x", "group-x", 200000, s.DropDailyLimit)
	assert.True(t, IsQQDropLimitReached(err))
}

// TestAwardQQDropGuaranteeNoRefarmAcrossDays 回归：前一日已拿满保底额时，
// 当日第 3 次不再补足——封顶看 7 日累计而非当日累计。
func TestAwardQQDropGuaranteeNoRefarmAcrossDays(t *testing.T) {
	defer swapDBForDropTest(t)()
	defer snapshotQQBotSetting()()

	s := GetQQBotSetting()
	s.DropMinQuota = 150000
	s.DropMaxQuota = 1500000
	s.DropDailyLimit = 3
	s.DropBalanceAnchor = 0
	s.DropDailyGuarantee = 1000000

	user := &identity.User{Username: "refarm", AffCode: "refarm", Quota: 0}
	require.NoError(t, dbx.DB.Create(user).Error)

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	today := time.Now().Format("2006-01-02")
	// 昨日已拿满保底 1,000,000；今日前两发共 300,000 → 7 日累计 1,300,000 ≥ 保底
	require.NoError(t, dbx.DB.Create(&QQDrop{
		UserId: user.Id, DropDate: yesterday, QuotaAwarded: 1000000, CreatedAt: time.Now().Unix(),
	}).Error)
	for _, q := range []int{150000, 150000} {
		require.NoError(t, dbx.DB.Create(&QQDrop{
			UserId: user.Id, DropDate: today, QuotaAwarded: q, CreatedAt: time.Now().Unix(),
		}).Error)
	}

	// 若补差只看当日累计会错误地补到 700,000；7 日封顶后按摇点值 200,000 发放
	drop, err := AwardQQDrop(user.Id, "openid-y", "group-y", 200000, s.DropDailyLimit)
	require.NoError(t, err)
	assert.Equal(t, 200000, drop.QuotaAwarded)

	// 边界用户：大额入账落在 today-7（窗口外，drop_date >= today-6 才计入），
	// 7 日累计只剩今日 300,000 → 第 3 发重新享受补足，保底窗口滚动过期。
	expired := &identity.User{Username: "window", AffCode: "window", Quota: 0}
	require.NoError(t, dbx.DB.Create(expired).Error)
	weekAgo := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
	require.NoError(t, dbx.DB.Create(&QQDrop{
		UserId: expired.Id, DropDate: weekAgo, QuotaAwarded: 1000000, CreatedAt: time.Now().Unix(),
	}).Error)
	for _, q := range []int{150000, 150000} {
		require.NoError(t, dbx.DB.Create(&QQDrop{
			UserId: expired.Id, DropDate: today, QuotaAwarded: q, CreatedAt: time.Now().Unix(),
		}).Error)
	}
	next, err := AwardQQDrop(expired.Id, "openid-z", "group-z", 200000, s.DropDailyLimit)
	require.NoError(t, err)
	assert.Equal(t, 700000, next.QuotaAwarded, "窗口外入账不计入封顶，deficit=1,000,000-300,000-200,000")
}
