package identity

import (
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

	// Nil inviter id does nothing
	common.InviterRewardCurrency = "spore"
	rewardInviterSpore(0)
}
