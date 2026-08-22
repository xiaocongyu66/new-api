package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
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

func GetProxyNodesForChannel(channel *Channel) ([]*ProxyNode, error) {
	return GetProxyNodesForChannelAndModel(channel, "")
}
func GetProxyNodesForChannelAndModel(channel *Channel, modelName string) ([]*ProxyNode, error) {
	if channel == nil {
		return nil, fmt.Errorf("channel is nil")
	}

	var enabledNodes []*ProxyNode
	if err := DB.Where("enabled = ?", true).
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
