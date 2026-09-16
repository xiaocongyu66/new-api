package identity

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatSpore(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "0.0", FormatSpore(0))
	assert.Equal(t, "0.1", FormatSpore(1))
	assert.Equal(t, "1.0", FormatSpore(10))
	assert.Equal(t, "2.5", FormatSpore(25))
	assert.Equal(t, "12.3", FormatSpore(123))
	assert.Equal(t, "-1.5", FormatSpore(-15))
}

func TestUserSporeOperations(t *testing.T) {
	setupUserStoreTestDB(t)
	// Stub audit hook to prevent real LogDB insert in isolated identity unit test
	prevSystemLog := recordSystemLog
	t.Cleanup(func() { recordSystemLog = prevSystemLog })
	RegisterAuditHooks(func(int, string) {}, nil, nil, nil)

	user := &User{
		Id:          100,
		Username:    "spore-user",
		Password:    "password123",
		DisplayName: "Spore User",
		Spore:       0,
	}
	require.NoError(t, dbx.DB.Create(user).Error)

	// Initial balance is 0
	spore, err := GetUserSpore(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(0), spore)

	// Increase spore: add 2.5 spore (25 units)
	require.NoError(t, IncreaseUserSpore(user.Id, 25))
	spore, err = GetUserSpore(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(25), spore)

	// Increase with invalid arguments
	assert.Error(t, IncreaseUserSpore(user.Id, 0))
	assert.Error(t, IncreaseUserSpore(user.Id, -5))
	assert.Error(t, IncreaseUserSpore(0, 10))

	// Decrease spore: deduct 1.0 spore (10 units)
	require.NoError(t, DecreaseUserSpore(user.Id, 10))
	spore, err = GetUserSpore(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(15), spore)

	// Decrease more than balance: fails with ErrSporeInsufficient, atomic
	err = DecreaseUserSpore(user.Id, 20)
	assert.ErrorIs(t, err, ErrSporeInsufficient)
	spore, err = GetUserSpore(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(15), spore, "balance must remain untouched on failure")

	// SetUserSpore: direct override
	require.NoError(t, SetUserSpore(user.Id, 50))
	spore, err = GetUserSpore(user.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(50), spore)

	// SetUserSpore rejects negative values
	assert.Error(t, SetUserSpore(user.Id, -1))

	// AdminAdjustUserSpore modes
	require.NoError(t, AdminAdjustUserSpore(user.Id, "add", 10))
	spore, _ = GetUserSpore(user.Id)
	assert.Equal(t, int64(60), spore)

	require.NoError(t, AdminAdjustUserSpore(user.Id, "subtract", 20))
	spore, _ = GetUserSpore(user.Id)
	assert.Equal(t, int64(40), spore)

	require.NoError(t, AdminAdjustUserSpore(user.Id, "override", 5))
	spore, _ = GetUserSpore(user.Id)
	assert.Equal(t, int64(5), spore)

	assert.Error(t, AdminAdjustUserSpore(user.Id, "unknown_mode", 5))
}

func TestRewardInviterSpore(t *testing.T) {
	setupUserStoreTestDB(t)
	var logContents []string
	prevSystemLog := recordSystemLog
	t.Cleanup(func() { recordSystemLog = prevSystemLog })
	RegisterAuditHooks(func(userId int, content string) {
		if userId == 200 {
			logContents = append(logContents, content)
		}
	}, nil, nil, nil)

	previous := common.SporeInviterRewardTenths
	t.Cleanup(func() { common.SporeInviterRewardTenths = previous })
	previousCurrency := common.InviterRewardCurrency
	t.Cleanup(func() { common.InviterRewardCurrency = previousCurrency })

	inviter := &User{
		Id:       200,
		Username: "inviter-user",
		Password: "password123",
		Spore:    0,
	}
	require.NoError(t, dbx.DB.Create(inviter).Error)

	// Reward inviter: 3 tenths (0.3 spore) per configuration.
	common.InviterRewardCurrency = "spore"
	common.SporeInviterRewardTenths = 3
	rewardInviterSpore(inviter.Id)
	spore, err := GetUserSpore(inviter.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(3), spore, "inviter must receive the configured spore reward")
	// 原实现契约：每次邀请恰好一条内容为「开拓奖励」的日志（运营对账串，不拼数量）。
	assert.Equal(t, []string{"开拓奖励"}, logContents, "exactly one 开拓奖励 log per invite")

	// Zero configuration disables the reward.
	common.SporeInviterRewardTenths = 0
	rewardInviterSpore(inviter.Id)
	spore, err = GetUserSpore(inviter.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(3), spore, "zero reward must not change the balance")

	// 互斥契约：邀请奖励货币未切到 spore 时，即使配置了数额也不发菌种
	//（余额模式由 inviteUser 走 aff_quota，两种货币不得重复发放）。
	common.SporeInviterRewardTenths = 3
	common.InviterRewardCurrency = "quota"
	rewardInviterSpore(inviter.Id)
	spore, err = GetUserSpore(inviter.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(3), spore, "quota currency mode must not grant spore")

	// 累计收入契约：发放即到账的菌种同时累加 aff_spore_history，钱包推荐
	// 卡片「总收入」读这一列展示；quota 模式不得累加。
	history, err := GetUserAffSporeHistory(inviter.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(3), history, "granted spore must accumulate into aff_spore_history")

	common.InviterRewardCurrency = "spore"
	common.SporeInviterRewardTenths = 3
	rewardInviterSpore(inviter.Id)
	history, err = GetUserAffSporeHistory(inviter.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(6), history, "a second grant must accumulate")

	// Nil inviter id does nothing
	common.InviterRewardCurrency = "spore"
	rewardInviterSpore(0)
}

// TestFinishInsertInviterRewardCurrencyMutualExclusion 验证 finishInsert 的
// dispatch 级货币门控：合规门开启且 QuotaForInviter>0 时，spore 模式不得
// 发放余额奖励、quota 模式不得发放菌种（双发正是设计注释点名的线上
// 事故类别，仅测 rewardInviterSpore 内部门控锁不住被删掉的条件）；
// both 模式两种奖励同时发放。
func TestFinishInsertInviterRewardCurrencyMutualExclusion(t *testing.T) {
	setupUserStoreTestDB(t)

	type logEntry struct {
		userID  int
		content string
	}
	var logs []logEntry
	prevSystemLog := recordSystemLog
	t.Cleanup(func() { recordSystemLog = prevSystemLog })
	RegisterAuditHooks(func(userId int, content string) {
		logs = append(logs, logEntry{userId, content})
	}, nil, nil, nil)

	// 合规门 fail closed（hook 未注册时为 false），这里显式打开以走进
	// quota 奖励分支——否则互斥条件根本不会被执行到。
	prevCompliance := OnIsPaymentComplianceConfirmed
	t.Cleanup(func() { OnIsPaymentComplianceConfirmed = prevCompliance })
	OnIsPaymentComplianceConfirmed = func() bool { return true }

	prevCurrency := common.InviterRewardCurrency
	t.Cleanup(func() { common.InviterRewardCurrency = prevCurrency })
	prevQuotaForInviter := common.QuotaForInviter
	t.Cleanup(func() { common.QuotaForInviter = prevQuotaForInviter })
	prevSporeReward := common.SporeInviterRewardTenths
	t.Cleanup(func() { common.SporeInviterRewardTenths = prevSporeReward })
	common.QuotaForInviter = 1000
	common.SporeInviterRewardTenths = 3

	inviterLogs := func(inviterId int) []string {
		var contents []string
		for _, e := range logs {
			if e.userID == inviterId {
				contents = append(contents, e.content)
			}
		}
		return contents
	}

	// aff_count 必须每个合规邀请 +1（与货币无关）；aff_quota/aff_history 仅在
	// 余额奖励实际发放时增长。
	affStats := func(id int) (count, quota, history int64) {
		var s struct {
			AffCount   int64
			AffQuota   int64
			AffHistory int64
		}
		require.NoError(t, dbx.DB.Model(&User{}).Select("aff_count", "aff_quota", "aff_history").Where("id = ?", id).Scan(&s).Error)
		return s.AffCount, s.AffQuota, s.AffHistory
	}

	// spore 模式：邀请人拿菌种，aff_quota 不动，无「邀请用户赠送」日志。
	common.InviterRewardCurrency = "spore"
	// aff_code 有唯一索引，直接 Create 的邀请人必须带互不相同的邀请码。
	inviterA := &User{Id: 400, Username: "mutual-inviter-a", Password: "password123", Spore: 0, AffCode: "mut-a"}
	require.NoError(t, dbx.DB.Create(inviterA).Error)
	inviteeA := &User{Id: 401, Username: "mutual-invitee-a", Password: "password123"}
	require.NoError(t, inviteeA.Insert(inviterA.Id))

	var affQuotaA int64
	require.NoError(t, dbx.DB.Model(&User{}).Select("aff_quota").Where("id = ?", inviterA.Id).Scan(&affQuotaA).Error)
	assert.EqualValues(t, 0, affQuotaA, "spore mode must not accrue aff_quota")
	sporeA, err := GetUserSpore(inviterA.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 3, sporeA, "spore mode must grant the configured spore reward")
	assert.Equal(t, []string{"开拓奖励"}, inviterLogs(inviterA.Id))
	countA, _, historyA := affStats(inviterA.Id)
	assert.EqualValues(t, 1, countA, "spore mode must still count the invite in aff_count")
	assert.EqualValues(t, 0, historyA, "spore mode must not accrue aff_history")

	// quota 模式：邀请人拿 aff_quota，菌种不动，无「开拓奖励」日志。
	common.InviterRewardCurrency = "quota"
	inviterB := &User{Id: 402, Username: "mutual-inviter-b", Password: "password123", Spore: 0, AffCode: "mut-b"}
	require.NoError(t, dbx.DB.Create(inviterB).Error)
	inviteeB := &User{Id: 403, Username: "mutual-invitee-b", Password: "password123"}
	require.NoError(t, inviteeB.Insert(inviterB.Id))

	var affQuotaB int64
	require.NoError(t, dbx.DB.Model(&User{}).Select("aff_quota").Where("id = ?", inviterB.Id).Scan(&affQuotaB).Error)
	assert.EqualValues(t, 1000, affQuotaB, "quota mode must accrue aff_quota")
	sporeB, err := GetUserSpore(inviterB.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 0, sporeB, "quota mode must not grant spore")
	for _, content := range inviterLogs(inviterB.Id) {
		assert.NotEqual(t, "开拓奖励", content, "quota mode must not write the spore reward log")
	}
	countB, _, historyB := affStats(inviterB.Id)
	assert.EqualValues(t, 1, countB, "quota mode must count the invite in aff_count")
	assert.EqualValues(t, 1000, historyB, "quota mode must accrue aff_history")

	// both 模式：aff_quota 与菌种同时入账，两条日志都在。
	common.InviterRewardCurrency = "both"
	inviterC := &User{Id: 404, Username: "mutual-inviter-c", Password: "password123", Spore: 0, AffCode: "mut-c"}
	require.NoError(t, dbx.DB.Create(inviterC).Error)
	inviteeC := &User{Id: 405, Username: "mutual-invitee-c", Password: "password123"}
	require.NoError(t, inviteeC.Insert(inviterC.Id))

	var affQuotaC int64
	require.NoError(t, dbx.DB.Model(&User{}).Select("aff_quota").Where("id = ?", inviterC.Id).Scan(&affQuotaC).Error)
	assert.EqualValues(t, 1000, affQuotaC, "both mode must accrue aff_quota")
	sporeC, err := GetUserSpore(inviterC.Id)
	require.NoError(t, err)
	assert.EqualValues(t, 3, sporeC, "both mode must grant the configured spore reward")
	logsC := inviterLogs(inviterC.Id)
	assert.Contains(t, logsC, "开拓奖励", "both mode must write the spore reward log")
	hasQuotaLog := false
	for _, content := range logsC {
		hasQuotaLog = hasQuotaLog || strings.HasPrefix(content, "邀请用户赠送")
	}
	assert.True(t, hasQuotaLog, "both mode must write the quota reward log")
	countC, _, historyC := affStats(inviterC.Id)
	assert.EqualValues(t, 1, countC, "both mode must count the invite in aff_count")
	assert.EqualValues(t, 1000, historyC, "both mode must accrue aff_history")
}
