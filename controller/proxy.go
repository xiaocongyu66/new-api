package controller

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// ProxyConfigRequest is the JSON shape for saving proxy configuration.
type ProxyConfigRequest struct {
	Outbound       OutboundConfig `json:"outbound"`
	GlobalProxyURL string         `json:"global_proxy_url"`
	Enabled        bool           `json:"enabled"`
}

// OutboundConfig mirrors service.OutboundConfig so the controller can
// accept and marshal proxy configuration without importing service.
type OutboundConfig struct {
	Type          string `json:"type"`
	Server        string `json:"server"`
	ServerPort    int    `json:"server_port"`
	UUID          string `json:"uuid,omitempty"`
	Password      string `json:"password,omitempty"`
	Flow          string `json:"flow,omitempty"`
	Encryption    string `json:"encryption,omitempty"`
	TLSEnabled    bool   `json:"tls_enabled,omitempty"`
	TLSServerName string `json:"tls_server_name,omitempty"`
}

// singBoxConfig is the complete sing-box JSON configuration structure.
type singBoxConfig struct {
	Log       logConfig       `json:"log"`
	Inbounds  []inboundConfig `json:"inbounds"`
	Outbounds []outboundCfg   `json:"outbounds"`
	Route     routeConfig     `json:"route"`
}

type logConfig struct {
	Level string `json:"level"`
}

type inboundConfig struct {
	Type string       `json:"type"`
	Tag  string       `json:"tag"`
	ListenOptions     `json:",inline"`
}

type ListenOptions struct {
	Listen     string `json:"listen"`
	ListenPort int    `json:"listen_port"`
}

type outboundCfg struct {
	Type string      `json:"type"`
	Tag  string      `json:"tag"`
	Options          `json:",inline,omitempty"`
}

type Options struct {
	Server     string `json:"server,omitempty"`
	ServerPort int    `json:"server_port,omitempty"`
	UUID       string `json:"uuid,omitempty"`
	Password   string `json:"password,omitempty"`
	Flow       string `json:"flow,omitempty"`
	Encryption string `json:"encryption,omitempty"`
	TLS        *tlsCfg `json:"tls,omitempty"`
}

type tlsCfg struct {
	Enabled    bool   `json:"enabled"`
	ServerName string `json:"server_name,omitempty"`
}

type routeConfig struct {
	Final string `json:"final"`
}

// GetProxyConfig returns the current proxy configuration and global proxy URL.
func GetProxyConfig(c *gin.Context) {
	var opt model.Option
	if err := model.DB.Where("key = ?", "proxy_config").First(&opt).Error; err != nil {
		common.ApiSuccess(c, gin.H{
			"enabled": false,
			"outbound": nil,
			"global_proxy_url": "",
		})
		return
	}
	var cfg ProxyConfigRequest
	if err := common.Unmarshal([]byte(opt.Value), &cfg); err != nil {
		common.ApiErrorMsg(c, "invalid proxy config in database")
		return
	}
	common.ApiSuccess(c, cfg)
}

// UpdateProxyConfig saves the proxy configuration to the Option table.
func UpdateProxyConfig(c *gin.Context) {
	var req ProxyConfigRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "invalid request body")
		return
	}
	jsonBytes, err := common.Marshal(req)
	if err != nil {
		common.ApiErrorMsg(c, "failed to marshal config")
		return
	}
	if err := model.UpdateOption("proxy_config", string(jsonBytes)); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// GenerateProxyConfig returns a complete sing-box config.json for the current
// proxy configuration. It uses encoding/json directly (no sing-box dependency).
func GenerateProxyConfig(c *gin.Context) {
	var opt model.Option
	if err := model.DB.Where("key = ?", "proxy_config").First(&opt).Error; err != nil {
		common.ApiErrorMsg(c, "proxy not configured")
		return
	}
	var cfg ProxyConfigRequest
	if err := common.Unmarshal([]byte(opt.Value), &cfg); err != nil {
		common.ApiErrorMsg(c, "invalid proxy config")
		return
	}
	if !cfg.Enabled {
		common.ApiErrorMsg(c, "proxy is disabled")
		return
	}

	outbound := outboundCfg{
		Type: cfg.Outbound.Type,
		Tag:  "proxy",
	}
	switch cfg.Outbound.Type {
	case "vless", "trojan", "shadowsocks", "vmess", "hysteria2", "tuic":
		outbound.Options = Options{
			Server:     cfg.Outbound.Server,
			ServerPort: cfg.Outbound.ServerPort,
			UUID:       cfg.Outbound.UUID,
			Password:   cfg.Outbound.Password,
			Flow:       cfg.Outbound.Flow,
			Encryption: cfg.Outbound.Encryption,
		}
		if cfg.Outbound.TLSEnabled {
			outbound.Options.TLS = &tlsCfg{
				Enabled:    true,
				ServerName: cfg.Outbound.TLSServerName,
			}
		}
	case "socks5", "http":
		outbound.Options = Options{
			Server:     cfg.Outbound.Server,
			ServerPort: cfg.Outbound.ServerPort,
			Password:   cfg.Outbound.Password,
		}
	default:
		// fallback: pass through raw fields
		outbound.Options = Options{
			Server:     cfg.Outbound.Server,
			ServerPort: cfg.Outbound.ServerPort,
		}
	}

	config := singBoxConfig{
		Log: logConfig{Level: "info"},
		Inbounds: []inboundConfig{{
			Type: "socks",
			Tag:  "socks-in",
			ListenOptions: ListenOptions{
				Listen:     "0.0.0.0",
				ListenPort: 1080,
			},
		}},
		Outbounds: []outboundCfg{
			outbound,
			{Type: "direct", Tag: "direct"},
		},
		Route: routeConfig{Final: "proxy"},
	}

	jsonBytes, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		common.ApiErrorMsg(c, "failed to generate config")
		return
	}
	common.ApiSuccess(c, gin.H{
		"config_json": string(jsonBytes),
	})
}

// GetProxyStatus checks whether the local proxy port (127.0.0.1:1080) is reachable.
func GetProxyStatus(c *gin.Context) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:1080", 3*time.Second)
	if err != nil {
		common.ApiSuccess(c, gin.H{
			"running": false,
			"error":   err.Error(),
		})
		return
	}
	conn.Close()
	common.ApiSuccess(c, gin.H{
		"running": true,
	})
}

// ReloadProxy sends SIGHUP to the sing-box container for hot reload.
func ReloadProxy(c *gin.Context) {
	containerName := os.Getenv("SINGBOX_CONTAINER_NAME")
	if containerName == "" {
		containerName = "sing-box-new-api"
	}
	cmd := exec.Command("docker", "kill", "-s", "HUP", containerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		common.ApiSuccess(c, gin.H{
			"success": false,
			"message": fmt.Sprintf("热加载失败（%v），请手动执行：docker kill -s HUP %s", err, containerName),
			"output":  string(output),
		})
		return
	}
	common.ApiSuccess(c, gin.H{
		"success": true,
		"message": "热加载成功",
	})
}