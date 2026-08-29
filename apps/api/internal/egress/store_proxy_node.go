package egress

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/model"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
)

const (
	ProxyNodeScopeCustom = "custom"
)

type ProxyNode struct {
	ID                   uint       `json:"id" gorm:"primaryKey"`
	Name                 string     `json:"name" gorm:"not null;index"`
	Enabled              bool       `json:"enabled"`
	EncryptedProxyConfig string     `json:"-" gorm:"type:text;not null"`
	Protocol             string     `json:"protocol" gorm:"size:32;index"`
	ScopeType            string     `json:"scope_type" gorm:"size:16;not null;index:idx_proxy_node_scope,priority:1"`
	ScopeValue           string     `json:"scope_value" gorm:"size:128;index:idx_proxy_node_scope,priority:2"`
	Health               float64    `json:"health"`
	FailureCount         int        `json:"failure_count"`
	CooldownUntil        *time.Time `json:"cooldown_until,omitempty"`
	LastError            string     `json:"last_error,omitempty" gorm:"type:text"`
	LastProbeAt          *time.Time `json:"last_probe_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type ProxyNodePublic struct {
	ID              uint       `json:"id"`
	Name            string     `json:"name"`
	Enabled         bool       `json:"enabled"`
	ProxyConfigured bool       `json:"proxy_configured"`
	Protocol        string     `json:"protocol"`
	ScopeType       string     `json:"scope_type"`
	ScopeValue      string     `json:"scope_value,omitempty"`
	ScopeName       string     `json:"scope_name,omitempty"`
	Health          float64    `json:"health"`
	FailureCount    int        `json:"failure_count"`
	CooldownUntil   *time.Time `json:"cooldown_until,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	LastProbeAt     *time.Time `json:"last_probe_at,omitempty"`
	ProbeTotal      int64      `json:"probe_total"`
	ProbeSuccess    int64      `json:"probe_success"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func NormalizeProxyNodeScope(scopeType, scopeValue string) (string, string, error) {
	scopeValue = strings.TrimSpace(scopeValue)
	scopeType = strings.ToLower(strings.TrimSpace(scopeType))
	switch scopeType {
	case ProxyNodeScopeCustom, "":
		// Custom JSON mapping is the only scope; empty type falls back to it.
		return ProxyNodeScopeCustom, scopeValue, nil
	default:
		return "", "", fmt.Errorf("unsupported proxy node scope type %q", scopeType)
	}
}

func (node ProxyNode) Public() ProxyNodePublic {
	return ProxyNodePublic{
		ID:              node.ID,
		Name:            node.Name,
		Enabled:         node.Enabled,
		ProxyConfigured: strings.TrimSpace(node.EncryptedProxyConfig) != "",
		Protocol:        node.Protocol,
		ScopeType:       node.ScopeType,
		ScopeValue:      node.ScopeValue,
		Health:          node.Health,
		FailureCount:    node.FailureCount,
		CooldownUntil:   node.CooldownUntil,
		LastError:       node.LastError,
		LastProbeAt:     node.LastProbeAt,
		CreatedAt:       node.CreatedAt,
		UpdatedAt:       node.UpdatedAt,
	}
}

func GetProxyNodesForChannel(channel *model.Channel) ([]*ProxyNode, error) {
	return GetProxyNodesForChannelAndModel(channel, "")
}
func GetProxyNodesForChannelAndModel(channel *model.Channel, modelName string) ([]*ProxyNode, error) {
	if channel == nil {
		return nil, fmt.Errorf("channel is nil")
	}

	var enabledNodes []*ProxyNode
	if err := dbx.DB.Where("enabled = ?", true).
		Where("scope_type = ?", ProxyNodeScopeCustom).
		Find(&enabledNodes).Error; err != nil {
		return nil, err
	}

	var matched []*ProxyNode
	for _, node := range enabledNodes {
		if node.ScopeValue == "" {
			continue
		}
		var customScope struct {
			Channels []int    `json:"channels"`
			Models   []string `json:"models"`
		}
		if err := common.UnmarshalJsonStr(node.ScopeValue, &customScope); err != nil {
			continue
		}
		hit := false
		for _, chID := range customScope.Channels {
			if chID == channel.Id {
				hit = true
				break
			}
		}
		if !hit && modelName != "" {
			for _, m := range customScope.Models {
				if strings.EqualFold(strings.TrimSpace(m), strings.TrimSpace(modelName)) {
					hit = true
					break
				}
			}
		}
		if hit {
			matched = append(matched, node)
		}
	}
	return matched, nil
}

// ProxyNode registers itself for AutoMigrate: a record moving into its own
// domain takes its registration with it (see internal/common/dbx).
func init() {
	dbx.RegisterMigrations(dbx.Migration{Model: &ProxyNode{}, Name: "ProxyNode"})
}

// Proxy outbound configuration shapes. egress owns them because it is the
// consumer that decodes the stored config when dialing upstream.
type ProxyConfig struct {
	Outbound       OutboundConfig `json:"outbound"`
	GlobalProxyURL string         `json:"global_proxy_url"`
	Enabled        bool           `json:"enabled"`
}

type OutboundConfig struct {
	Type           string            `json:"type"`
	Server         string            `json:"server"`
	ServerPort     int               `json:"server_port"`
	UUID           string            `json:"uuid,omitempty"`
	Password       string            `json:"password,omitempty"`
	Flow           string            `json:"flow,omitempty"`
	Encryption     string            `json:"encryption,omitempty"`
	Method         string            `json:"method,omitempty"`
	Network        string            `json:"network,omitempty"`
	PacketEncoding string            `json:"packet_encoding,omitempty"`
	Masquerade     string            `json:"masquerade,omitempty"`
	Obfs           string            `json:"obfs,omitempty"`
	ObfsPassword   string            `json:"obfs_password,omitempty"`
	HopPorts       string            `json:"hop_ports,omitempty"`
	TLSEnabled     bool              `json:"tls_enabled,omitempty"`
	TLSServerName  string            `json:"tls_server_name,omitempty"`
	Transport      *transportOptions `json:"transport,omitempty"`
}

type transportOptions struct {
	Type        string `json:"type,omitempty"`
	Path        string `json:"path,omitempty"`
	Host        string `json:"host,omitempty"`
	ServiceName string `json:"service_name,omitempty"`
	Security    string `json:"security,omitempty"`
	Key         string `json:"key,omitempty"`
}
