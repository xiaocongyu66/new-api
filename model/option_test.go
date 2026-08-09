package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// optionMapSnapshot swaps in a fresh in-memory option map for the test and
// restores the previous one on cleanup.
func optionMapSnapshot(t *testing.T) {
	t.Helper()
	previousMap := common.OptionMap
	t.Cleanup(func() { common.OptionMap = previousMap })
	common.OptionMap = map[string]string{}
}

func optionMapValue(t *testing.T, key string) (string, bool) {
	t.Helper()
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	value, ok := common.OptionMap[key]
	return value, ok
}

func TestUpdateOptionProxyConfig(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	optionMapSnapshot(t)

	const firstConfig = `{"outbound":{"type":"socks","server":"127.0.0.1","server_port":1080},"global_proxy_url":"socks5://127.0.0.1:1080","enabled":true}`

	// 1. UpdateOption writes proxy_config; DB and OptionMap both read it back consistently.
	require.NoError(t, UpdateOption("proxy_config", firstConfig))
	assert.Equal(t, firstConfig, requireOptionValue(t, db, "proxy_config"))
	mapValue, ok := optionMapValue(t, "proxy_config")
	assert.True(t, ok, "proxy_config must be present in OptionMap")
	assert.Equal(t, firstConfig, mapValue)

	// 2. Multiple updates: the latest value is what gets read back.
	const secondConfig = `{"outbound":{"type":"http","server":"10.0.0.1","server_port":3128},"enabled":false}`
	require.NoError(t, UpdateOption("proxy_config", secondConfig))
	assert.Equal(t, secondConfig, requireOptionValue(t, db, "proxy_config"))
	mapValue, ok = optionMapValue(t, "proxy_config")
	assert.True(t, ok)
	assert.Equal(t, secondConfig, mapValue)

	// 3. Updating an unrelated option leaves proxy_config untouched.
	require.NoError(t, UpdateOption("Notice", "system notice"))
	assert.Equal(t, secondConfig, requireOptionValue(t, db, "proxy_config"))
	mapValue, ok = optionMapValue(t, "proxy_config")
	assert.True(t, ok)
	assert.Equal(t, secondConfig, mapValue)
	assert.Equal(t, "system notice", requireOptionValue(t, db, "Notice"))
}

func TestUpdateOptionValidationFailure(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	optionMapSnapshot(t)

	err := UpdateOption("MaxTokenAutoGroups", "not-a-positive-integer")
	require.Error(t, err)
	requireOptionMissing(t, db, "MaxTokenAutoGroups")
}

func TestUpdateOptionUnknownKeySucceeds(t *testing.T) {
	db := useFrontendOptionMigrationDB(t)
	optionMapSnapshot(t)

	err := UpdateOption("nonexistent_key", "any value")
	require.NoError(t, err)
	assert.Equal(t, "any value", requireOptionValue(t, db, "nonexistent_key"))
}
