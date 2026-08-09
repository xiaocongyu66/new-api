package service

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useProxyConfigTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	previousDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	model.DB = db

	t.Cleanup(func() {
		model.DB = previousDB
	})

	return db
}

func saveProxyConfigOption(t *testing.T, db *gorm.DB, cfg ProxyConfig) {
	t.Helper()

	value, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Option{Key: "proxy_config", Value: string(value)}).Error)
}

func TestGetGlobalProxyURL(t *testing.T) {
	const proxyURL = "http://127.0.0.1:7890"

	t.Run("returns empty string when proxy config is missing", func(t *testing.T) {
		useProxyConfigTestDB(t)

		assert.Empty(t, getGlobalProxyURL())
	})

	t.Run("returns global proxy URL when configured and enabled", func(t *testing.T) {
		db := useProxyConfigTestDB(t)
		saveProxyConfigOption(t, db, ProxyConfig{
			GlobalProxyURL: proxyURL,
			Enabled:        true,
		})

		assert.Equal(t, proxyURL, getGlobalProxyURL())
	})

	t.Run("returns empty string when configured but disabled", func(t *testing.T) {
		db := useProxyConfigTestDB(t)
		saveProxyConfigOption(t, db, ProxyConfig{
			GlobalProxyURL: proxyURL,
			Enabled:        false,
		})

		assert.Empty(t, getGlobalProxyURL())
	})
}
