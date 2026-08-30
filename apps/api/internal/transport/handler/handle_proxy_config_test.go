package handler

import (
	"bytes"
	"encoding/json"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	ops "github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/dbinfra"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupProxyConfigControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := dbx.DB
	previousSecret := common.CryptoSecret
	dsn := "file:proxy-config-controller-test?mode=memory&cache=private"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&dbinfra.Option{}))
	dbx.DB = db
	common.CryptoSecret = "proxy-config-controller-test-secret"
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	t.Cleanup(func() {
		dbx.DB = previousDB
		common.CryptoSecret = previousSecret
	})
	return db
}

func proxyConfigContext(t *testing.T, method, path, body string) (contract.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, recorder := ginadapter.NewSyntheticContext(httptest.NewRequest(method, path, bytes.NewBufferString(body)))
	ctx.Headers().Set("Content-Type", "application/json")
	return ctx, recorder
}

func TestGetProxyConfigMasksSecrets(t *testing.T) {
	setupProxyConfigControllerTest(t)

	// Save a config with real secrets.
	require.NoError(t, ops.SaveProxyConfigJSON(`{"enabled":true,"outbound":{"type":"trojan","password":"real-password","uuid":"real-uuid"}}`))

	ctx, recorder := proxyConfigContext(t, http.MethodGet, "/api/proxy/config", "")
	ops.GetProxyConfig(ctx)

	var resp struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))

	var cfg ops.ProxyConfigRequest
	require.NoError(t, json.Unmarshal(resp.Data, &cfg))
	assert.Equal(t, ops.ProxyMaskedSecret, cfg.Outbound.Password)
	assert.Equal(t, ops.ProxyMaskedSecret, cfg.Outbound.UUID)
}

func TestUpdateProxyConfigRestoresMaskedSecrets(t *testing.T) {
	setupProxyConfigControllerTest(t)

	// Save initial config with real secrets.
	require.NoError(t, ops.SaveProxyConfigJSON(`{"enabled":true,"outbound":{"type":"trojan","password":"real-password","uuid":"real-uuid"}}`))

	// Simulate frontend round-trip: masked values sent back unchanged.
	body := `{"enabled":true,"outbound":{"type":"trojan","password":"********","uuid":"********"}}`
	ctx, recorder := proxyConfigContext(t, http.MethodPut, "/api/proxy/config", body)
	ops.UpdateProxyConfig(ctx)
	assert.Equal(t, http.StatusOK, recorder.Code)

	// Verify stored config still has real secrets, not the sentinel.
	loaded, err := ops.LoadProxyConfigJSON()
	require.NoError(t, err)
	var cfg ops.ProxyConfigRequest
	require.NoError(t, json.Unmarshal([]byte(loaded), &cfg))
	assert.Equal(t, "real-password", cfg.Outbound.Password)
	assert.Equal(t, "real-uuid", cfg.Outbound.UUID)
}
