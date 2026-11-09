package identity

import (
	"context"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/quotacache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCacheInitTokenStoresAllFields pins the token cache hash shape. The init
// script passes every field positionally as ARGV, so a field appended to the
// HSET list without bumping the argument list (or the TTL index) silently
// shifts every value after it — the exact misalignment the TTL-refresh branch
// once shipped with. Every stored value below is distinct, so any shift breaks
// the whole-map comparison, and the explicit length guard fails when a field
// is added to the struct/script pair without updating this test.
func TestCacheInitTokenStoresAllFields(t *testing.T) {
	useUserCacheMiniRedis(t)

	allowIps := "10.0.0.0/8"
	token := Token{
		Id:                 3,
		UserId:             7,
		Key:                "cache-init-all-fields-key",
		Status:             2,
		Name:               "audit-probe",
		CreatedTime:        1111,
		AccessedTime:       2222,
		ExpiredTime:        -1,
		UnlimitedQuota:     true,
		ModelLimitsEnabled: false,
		ModelLimits:        "gpt-4o,claude-3",
		AllowIps:           &allowIps,
		UsedQuota:          55,
		Group:              "auto",
		CrossGroupRetry:    true,
		AutoGroups:         `["vip","default"]`,
		RemainQuota:        500,
		Setting:            `{"system_prompt_preset":"auto","system_prompt":"keep","user_system_prompt_override":true}`,
	}

	code, err := CacheInitToken(token)
	require.NoError(t, err)
	require.Equal(t, 1, code, "a cold hash must be fully initialized, not TTL-refreshed")

	fields, err := common.RDB.HGetAll(context.Background(), quotacache.TokenKey(token.Key)).Result()
	require.NoError(t, err)
	assert.Len(t, fields, 17, "the cache script must publish exactly the fields below")
	assert.Equal(t, map[string]string{
		"Id":                 strconv.Itoa(token.Id),
		"UserId":             strconv.Itoa(token.UserId),
		"Status":             strconv.Itoa(token.Status),
		"Name":               token.Name,
		"CreatedTime":        strconv.FormatInt(token.CreatedTime, 10),
		"AccessedTime":       strconv.FormatInt(token.AccessedTime, 10),
		"ExpiredTime":        strconv.FormatInt(token.ExpiredTime, 10),
		"UnlimitedQuota":     strconv.FormatBool(token.UnlimitedQuota),
		"ModelLimitsEnabled": strconv.FormatBool(token.ModelLimitsEnabled),
		"ModelLimits":        token.ModelLimits,
		"AllowIps":           allowIps,
		"Group":              token.Group,
		"CrossGroupRetry":    strconv.FormatBool(token.CrossGroupRetry),
		"AutoGroups":         token.AutoGroups,
		"RemainQuota":        strconv.Itoa(token.RemainQuota),
		"UsedQuota":          strconv.Itoa(token.UsedQuota),
		"Setting":            token.Setting,
	}, fields)
}
