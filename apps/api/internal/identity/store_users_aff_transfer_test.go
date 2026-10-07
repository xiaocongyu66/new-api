package identity

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransferAffQuotaToQuotaSyncsCachedWallet guards the cache side of the
// affiliate transfer: pre-consumption reads the cached wallet while the hash
// exists, so the transferred credit must land in the hash, not only in the
// committed row.
func TestTransferAffQuotaToQuotaSyncsCachedWallet(t *testing.T) {
	setupIdentityTestDB(t)
	useUserCacheMiniRedis(t)

	transfer := int(common.QuotaPerUnit)
	user := User{
		Username: "aff-transfer-cache-user", Password: "unused-password-hash",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
		Group: "default", AuthVersion: 1, AffCode: "aff-transfer-cache-code",
		Quota: 0, AffQuota: 2 * transfer,
	}
	require.NoError(t, dbx.DB.Create(&user).Error)
	require.NoError(t, PopulateUserCache(user))

	require.NoError(t, user.TransferAffQuotaToQuota(transfer))

	var reloaded User
	require.NoError(t, dbx.DB.First(&reloaded, user.Id).Error)
	assert.Equal(t, transfer, reloaded.Quota)
	assert.Equal(t, transfer, reloaded.AffQuota)

	cached, err := CacheGetUserBase(user.Id)
	require.NoError(t, err)
	assert.Equal(t, transfer, cached.Quota)
}
