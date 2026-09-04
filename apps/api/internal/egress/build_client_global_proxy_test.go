package egress

import (
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupProxyConfigTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	previousDB := dbx.DB
	dbx.DB = db
	t.Cleanup(func() { dbx.DB = previousDB })
	require.NoError(t, db.AutoMigrate(&optionRow{}))
	// OptionMap must be non-nil for updateOptionMap.
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
}

func TestGetGlobalProxyURLWithEncryptedConfig(t *testing.T) {
	setupProxyConfigTestDB(t)
	originalSecret := common.CryptoSecret
	common.CryptoSecret = "test-secret-key"
	t.Cleanup(func() { common.CryptoSecret = originalSecret })

	plaintext := `{"enabled":true,"global_proxy_url":"socks5://127.0.0.1:1080"}`
	encrypted, encErr := common.EncryptAESGCM(plaintext, "proxy-config")
	require.NoError(t, encErr)
	require.NoError(t, dbx.DB.Save(&optionRow{Key: "proxy_config", Value: encrypted}).Error)

	url := getGlobalProxyURL()
	assert.Equal(t, "socks5://127.0.0.1:1080", url)
}
