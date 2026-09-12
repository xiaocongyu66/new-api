package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupStealTestDB(t *testing.T) func() {
	t.Helper()
	previousDB, previousLogDB := dbx.DB, dbx.LogDB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&identity.User{}, &QQSteal{}))
	dbx.DB, dbx.LogDB = db, db
	return func() { dbx.DB, dbx.LogDB = previousDB, previousLogDB }
}

// createQuotaUser 建一个有指定余额的站点用户
func createQuotaUser(t *testing.T, name string, quota int) int {
	t.Helper()
	u := &identity.User{Username: name, AffCode: name, Quota: quota}
	require.NoError(t, dbx.DB.Create(u).Error)
	return u.Id
}

func userQuota(t *testing.T, id int) int {
	t.Helper()
	var q int
	require.NoError(t, dbx.DB.Model(&identity.User{}).Where("id = ?", id).
		Pluck("quota", &q).Error)
	return q
}

func stealParams(thief, victim, amount int) *QQStealParams {
	return &QQStealParams{
		ThiefUserId: thief, VictimUserId: victim,
		ThiefOpenID: "thief-open", VictimOpenID: "victim-open",
		GroupOpenID: "group-open",
		Amount:      amount, SuccessRate: 100, DailyLimit: 0, GraceSeconds: 0,
	}
}

// TestDoQQStealSuccessMovesQuota 掷骰必中：受害者减、小偷加、审计记录成功。
func TestDoQQStealSuccessMovesQuota(t *testing.T) {
	defer setupStealTestDB(t)()
	victim := createQuotaUser(t, "victim", 5000000)
	thief := createQuotaUser(t, "thief", 1000000)

	steal, err := DoQQSteal(stealParams(thief, victim, 1200000))
	require.NoError(t, err)
	require.True(t, steal.Success)
	assert.Empty(t, steal.Reason)
	assert.Equal(t, 1200000, steal.Amount)
	assert.Equal(t, 3800000, userQuota(t, victim))
	assert.Equal(t, 2200000, userQuota(t, thief))

	var cnt int64
	require.NoError(t, dbx.DB.Model(&QQSteal{}).
		Where("thief_user_id = ? AND victim_user_id = ? AND success = ?", thief, victim, true).
		Count(&cnt).Error)
	assert.Equal(t, int64(1), cnt)
}

// TestDoQQStealFailurePaths 掷骰落空与余额不足：记录失败原因，双方额度分毫不动。
func TestDoQQStealFailurePaths(t *testing.T) {
	cases := []struct {
		name        string
		rate        int
		victimQuota int
		wantReason  string
	}{
		{"掷骰落空", 0, 5000000, stealFailLucky},
		{"余额不足", 100, 1000, stealFailBroke},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer setupStealTestDB(t)()
			victim := createQuotaUser(t, "victim", tc.victimQuota)
			thief := createQuotaUser(t, "thief", 1000000)

			p := stealParams(thief, victim, 1200000)
			p.SuccessRate = tc.rate
			steal, err := DoQQSteal(p)
			require.NoError(t, err)
			assert.False(t, steal.Success)
			assert.Equal(t, tc.wantReason, steal.Reason)
			assert.Equal(t, 0, steal.Amount)
			assert.Equal(t, tc.victimQuota, userQuota(t, victim))
			assert.Equal(t, 1000000, userQuota(t, thief))
		})
	}
}

// TestDoQQStealSelfRejected 偷自己不产生记录。
func TestDoQQStealSelfRejected(t *testing.T) {
	defer setupStealTestDB(t)()
	u := createQuotaUser(t, "solo", 1000000)
	_, err := DoQQSteal(stealParams(u, u, 100))
	assert.ErrorIs(t, err, ErrStealSelf)
}

// TestDoQQStealDailyLimitCountsFailures 每日次数按尝试计：失败的掷骰也刷次数。
func TestDoQQStealDailyLimitCountsFailures(t *testing.T) {
	defer setupStealTestDB(t)()
	victim := createQuotaUser(t, "victim", 10000000)
	thief := createQuotaUser(t, "thief", 0)

	p := stealParams(thief, victim, 100000)
	p.DailyLimit = 3
	p.SuccessRate = 0 // 全落空也占次数
	for range 3 {
		steal, err := DoQQSteal(p)
		require.NoError(t, err)
		assert.False(t, steal.Success)
	}
	_, err := DoQQSteal(p)
	assert.ErrorIs(t, err, ErrStealLimit)

	// 另一个小偷不受影响
	thief2 := createQuotaUser(t, "thief2", 0)
	p2 := stealParams(thief2, victim, 100000)
	p2.DailyLimit = 3
	steal, err := DoQQSteal(p2)
	require.NoError(t, err)
	assert.True(t, steal.Success, "限额只约束刷满次数的 thief")
}

// TestDoQQStealGraceWindow 得手后的冷却窗口内不能再次偷同一受害者；
// 掷骰落空不触发窗口；换受害者不受影响。
func TestDoQQStealGraceWindow(t *testing.T) {
	defer setupStealTestDB(t)()
	victimA := createQuotaUser(t, "victim-a", 5000000)
	victimB := createQuotaUser(t, "victim-b", 5000000)
	thief := createQuotaUser(t, "thief", 0)

	miss := stealParams(thief, victimA, 100000)
	miss.SuccessRate = 0
	steal, err := DoQQSteal(miss)
	require.NoError(t, err)
	assert.False(t, steal.Success, "落空不应触发冷却")

	hit := stealParams(thief, victimA, 100000)
	steal, err = DoQQSteal(hit)
	require.NoError(t, err)
	require.True(t, steal.Success)

	again := stealParams(thief, victimA, 100000)
	again.GraceSeconds = 60
	steal, err = DoQQSteal(again)
	require.NoError(t, err)
	assert.False(t, steal.Success)
	assert.Equal(t, stealFailGrace, steal.Reason)
	assert.Equal(t, 4900000, userQuota(t, victimA), "被冷却拦下时分毫不动")

	// 冷却只保护被偷过的那个人
	other := stealParams(thief, victimB, 100000)
	other.GraceSeconds = 60
	steal, err = DoQQSteal(other)
	require.NoError(t, err)
	assert.True(t, steal.Success)
}

// TestIsStealCommand 裸动词识别，兼容 @机器人 前缀标签
func TestIsStealCommand(t *testing.T) {
	cases := []struct {
		content string
		want    bool
	}{
		{"偷奶酪", true},
		{"/偷奶酪", true},
		{"<qqbot-at-user uid=\"xx\"/>偷奶酪", true},
		{"偷奶酪 <qqbot-at-user uid=\"yy\"/>", true},
		{"偷奶酪 0.3 <qqbot-at-user uid=\"yy\"/>", true},
		{"偷", false},
		{"别偷奶酪", false}, // 与转账/红包别名规则一致：动词前带其他文字不触发
		{"偷奶酪0.3", false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, isStealCommand(tc.content), tc.content)
	}
}

// TestParseStealAmount 可选数量参数解析
func TestParseStealAmount(t *testing.T) {
	v, ok := parseStealAmount("偷奶酪 0.3 @x")
	assert.True(t, ok)
	assert.InDelta(t, 0.3, v, 1e-9)

	v, ok = parseStealAmount("偷奶酪 0.3🧀 @x")
	assert.True(t, ok)
	assert.InDelta(t, 0.3, v, 1e-9)

	_, ok = parseStealAmount("偷奶酪 <qqbot-at-user uid=\"x\"/>")
	assert.False(t, ok)
}
