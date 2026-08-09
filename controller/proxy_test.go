package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type generateProxyResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		ConfigJSON string `json:"config_json"`
	} `json:"data"`
}

// setupProxyConfigControllerTest swaps model.DB for an in-memory SQLite
// database with the options table, mirroring the pattern used across the
// controller test suite.
func setupProxyConfigControllerTest(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Option{}))
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
	})
}

func seedProxyConfig(t *testing.T, cfg ProxyConfigRequest) {
	t.Helper()
	raw, err := common.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.Option{Key: "proxy_config", Value: string(raw)}).Error)
}

func callGenerateProxyConfig(t *testing.T) (generateProxyResponse, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/proxy/config/generate", nil)
	GenerateProxyConfig(c)
	var resp generateProxyResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	return resp, recorder
}

func TestGenerateProxyConfigNotConfigured(t *testing.T) {
	setupProxyConfigControllerTest(t)

	resp, _ := callGenerateProxyConfig(t)

	assert.False(t, resp.Success)
	assert.Equal(t, "proxy not configured", resp.Message)
}

func TestGenerateProxyConfigDisabled(t *testing.T) {
	setupProxyConfigControllerTest(t)
	seedProxyConfig(t, ProxyConfigRequest{
		Outbound: OutboundConfig{
			Type:       "vless",
			Server:     "127.0.0.1",
			ServerPort: 443,
			UUID:       "uuid-disabled-test",
		},
		Enabled: false,
	})

	resp, _ := callGenerateProxyConfig(t)

	assert.False(t, resp.Success)
	assert.Equal(t, "proxy is disabled", resp.Message)
}

func TestGenerateProxyConfigInvalidJSON(t *testing.T) {
	setupProxyConfigControllerTest(t)
	require.NoError(t, model.DB.Create(&model.Option{Key: "proxy_config", Value: "not valid json"}).Error)

	resp, _ := callGenerateProxyConfig(t)

	assert.False(t, resp.Success)
	assert.Equal(t, "invalid proxy config", resp.Message)
}

func TestGenerateProxyConfigVLESS(t *testing.T) {
	setupProxyConfigControllerTest(t)
	seedProxyConfig(t, ProxyConfigRequest{
		Outbound: OutboundConfig{
			Type:           "vless",
			Server:         "vless.example.com",
			ServerPort:     443,
			UUID:           "6f8b7c9a-1a2b-3c4d-5e6f-7a8b9c0d1e2f",
			Flow:           "xtls-rprx-vision",
			Network:        "tcp",
			PacketEncoding: "xudp",
			TLSEnabled:     true,
			TLSServerName:  "vless.example.com",
		},
		Enabled: true,
	})

	resp, _ := callGenerateProxyConfig(t)

	require.True(t, resp.Success)
	var config singBoxConfig
	require.NoError(t, json.Unmarshal([]byte(resp.Data.ConfigJSON), &config))
	require.Len(t, config.Outbounds, 2)
	outbound := config.Outbounds[0]
	assert.Equal(t, "vless", outbound.Type)
	assert.Equal(t, "proxy", outbound.Tag)
	assert.Equal(t, "vless.example.com", outbound.Server)
	assert.Equal(t, 443, outbound.ServerPort)
	assert.Equal(t, "6f8b7c9a-1a2b-3c4d-5e6f-7a8b9c0d1e2f", outbound.UUID)
	assert.Equal(t, "xtls-rprx-vision", outbound.Flow)
	assert.Equal(t, "tcp", outbound.Network)
	assert.NotNil(t, outbound.TLS)
	assert.True(t, outbound.TLS.Enabled)
	assert.Equal(t, "direct", config.Outbounds[1].Type)
	assert.Equal(t, "proxy", config.Route.Final)
	assert.Equal(t, 1080, config.Inbounds[0].ListenPort)
}

func TestGenerateProxyConfigVmess(t *testing.T) {
	setupProxyConfigControllerTest(t)
	seedProxyConfig(t, ProxyConfigRequest{
		Outbound: OutboundConfig{
			Type:       "vmess",
			Server:     "vmess.example.com",
			ServerPort: 443,
			UUID:       "6f8b7c9a-1a2b-3c4d-5e6f-7a8b9c0d1e2f",
			Network:    "tcp",
		},
		Enabled: true,
	})

	resp, _ := callGenerateProxyConfig(t)

	require.True(t, resp.Success)
	var config singBoxConfig
	require.NoError(t, json.Unmarshal([]byte(resp.Data.ConfigJSON), &config))
	require.Len(t, config.Outbounds, 2)
	outbound := config.Outbounds[0]
	assert.Equal(t, "vmess", outbound.Type)
	assert.Equal(t, "vmess.example.com", outbound.Server)
	assert.Equal(t, 443, outbound.ServerPort)
	assert.Equal(t, "6f8b7c9a-1a2b-3c4d-5e6f-7a8b9c0d1e2f", outbound.UUID)
}

func TestGenerateProxyConfigTrojan(t *testing.T) {
	setupProxyConfigControllerTest(t)
	seedProxyConfig(t, ProxyConfigRequest{
		Outbound: OutboundConfig{
			Type:       "trojan",
			Server:     "trojan.example.com",
			ServerPort: 8443,
			Password:   "trojan-password-123",
			Network:    "tcp",
		},
		Enabled: true,
	})

	resp, _ := callGenerateProxyConfig(t)

	require.True(t, resp.Success)
	var config singBoxConfig
	require.NoError(t, json.Unmarshal([]byte(resp.Data.ConfigJSON), &config))
	require.Len(t, config.Outbounds, 2)
	outbound := config.Outbounds[0]
	assert.Equal(t, "trojan", outbound.Type)
	assert.Equal(t, "trojan.example.com", outbound.Server)
	assert.Equal(t, 8443, outbound.ServerPort)
	assert.Equal(t, "trojan-password-123", outbound.Password)
}

func TestGenerateProxyConfigShadowsocks(t *testing.T) {
	setupProxyConfigControllerTest(t)
	seedProxyConfig(t, ProxyConfigRequest{
		Outbound: OutboundConfig{
			Type:       "shadowsocks",
			Server:     "ss.example.com",
			ServerPort: 8388,
			Method:     "2022-blake3-aes-128-gcm",
			Password:   "ss-password-456",
		},
		Enabled: true,
	})

	resp, _ := callGenerateProxyConfig(t)

	require.True(t, resp.Success)
	var config singBoxConfig
	require.NoError(t, json.Unmarshal([]byte(resp.Data.ConfigJSON), &config))
	require.Len(t, config.Outbounds, 2)
	outbound := config.Outbounds[0]
	assert.Equal(t, "shadowsocks", outbound.Type)
	assert.Equal(t, "ss.example.com", outbound.Server)
	assert.Equal(t, 8388, outbound.ServerPort)
	assert.Equal(t, "2022-blake3-aes-128-gcm", outbound.Method)
	assert.Equal(t, "ss-password-456", outbound.Password)
}

func TestGenerateProxyConfigHysteria2(t *testing.T) {
	setupProxyConfigControllerTest(t)
	seedProxyConfig(t, ProxyConfigRequest{
		Outbound: OutboundConfig{
			Type:         "hysteria2",
			Server:       "hysteria2.example.com",
			ServerPort:   8443,
			Password:     "hy2-password-789",
			Masquerade:   "https://example.com",
			Obfs:         "salamander",
			ObfsPassword: "obfs-secret",
			HopPorts:     "50000-60000",
		},
		Enabled: true,
	})

	resp, _ := callGenerateProxyConfig(t)

	require.True(t, resp.Success)
	var config singBoxConfig
	require.NoError(t, json.Unmarshal([]byte(resp.Data.ConfigJSON), &config))
	require.Len(t, config.Outbounds, 2)
	outbound := config.Outbounds[0]
	assert.Equal(t, "hysteria2", outbound.Type)
	assert.Equal(t, "hysteria2.example.com", outbound.Server)
	assert.Equal(t, 8443, outbound.ServerPort)
	assert.Equal(t, "hy2-password-789", outbound.Password)
	assert.Equal(t, "https://example.com", outbound.Masquerade)
	assert.Equal(t, "salamander", outbound.Obfs)
	assert.Equal(t, "obfs-secret", outbound.ObfsPassword)
	assert.Equal(t, "50000-60000", outbound.HopPorts)
}

func TestGenerateProxyConfigTuic(t *testing.T) {
	setupProxyConfigControllerTest(t)
	seedProxyConfig(t, ProxyConfigRequest{
		Outbound: OutboundConfig{
			Type:       "tuic",
			Server:     "tuic.example.com",
			ServerPort: 443,
			UUID:       "7f8b7c9a-1a2b-3c4d-5e6f-7a8b9c0d1e2f",
			Password:   "tuic-password-123",
		},
		Enabled: true,
	})

	resp, _ := callGenerateProxyConfig(t)

	require.True(t, resp.Success)
	var config singBoxConfig
	require.NoError(t, json.Unmarshal([]byte(resp.Data.ConfigJSON), &config))
	require.Len(t, config.Outbounds, 2)
	outbound := config.Outbounds[0]
	assert.Equal(t, "tuic", outbound.Type)
	assert.Equal(t, "tuic.example.com", outbound.Server)
	assert.Equal(t, 443, outbound.ServerPort)
	assert.Equal(t, "7f8b7c9a-1a2b-3c4d-5e6f-7a8b9c0d1e2f", outbound.UUID)
	assert.Equal(t, "tuic-password-123", outbound.Password)
}

func TestGenerateProxyConfigSocks5(t *testing.T) {
	setupProxyConfigControllerTest(t)
	seedProxyConfig(t, ProxyConfigRequest{
		Outbound: OutboundConfig{
			Type:       "socks5",
			Server:     "socks5.example.com",
			ServerPort: 1080,
			Password:   "socks5-password",
		},
		Enabled: true,
	})

	resp, _ := callGenerateProxyConfig(t)

	require.True(t, resp.Success)
	var config singBoxConfig
	require.NoError(t, json.Unmarshal([]byte(resp.Data.ConfigJSON), &config))
	require.Len(t, config.Outbounds, 2)
	outbound := config.Outbounds[0]
	assert.Equal(t, "socks5", outbound.Type)
	assert.Equal(t, "socks5.example.com", outbound.Server)
	assert.Equal(t, 1080, outbound.ServerPort)
	assert.Equal(t, "socks5-password", outbound.Password)
}

func TestGenerateProxyConfigHTTP(t *testing.T) {
	setupProxyConfigControllerTest(t)
	seedProxyConfig(t, ProxyConfigRequest{
		Outbound: OutboundConfig{
			Type:       "http",
			Server:     "http.example.com",
			ServerPort: 3128,
			Password:   "http-password",
		},
		Enabled: true,
	})

	resp, _ := callGenerateProxyConfig(t)

	require.True(t, resp.Success)
	var config singBoxConfig
	require.NoError(t, json.Unmarshal([]byte(resp.Data.ConfigJSON), &config))
	require.Len(t, config.Outbounds, 2)
	outbound := config.Outbounds[0]
	assert.Equal(t, "http", outbound.Type)
	assert.Equal(t, "http.example.com", outbound.Server)
	assert.Equal(t, 3128, outbound.ServerPort)
	assert.Equal(t, "http-password", outbound.Password)
}
