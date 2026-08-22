package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const (
	ProxyNodeScopeAll     = "all"
	ProxyNodeScopeChannel = "channel"
	ProxyNodeScopeGroup   = "group"
	ProxyNodeScopeCustom  = "custom"
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
	scopeType = strings.ToLower(strings.TrimSpace(scopeType))
	scopeValue = strings.TrimSpace(scopeValue)
	switch scopeType {
	case ProxyNodeScopeAll:
		if scopeValue != "" {
			return "", "", fmt.Errorf("scope value must be empty for all scope")
		}
	case ProxyNodeScopeChannel:
		id, err := strconv.Atoi(scopeValue)
		if err != nil || id <= 0 {
			return "", "", fmt.Errorf("channel scope value must be a positive channel ID")
		}
		scopeValue = strconv.Itoa(id)
	case ProxyNodeScopeGroup:
		if scopeValue == "" {
			return "", "", fmt.Errorf("group scope value must not be empty")
		}
	case ProxyNodeScopeCustom, "":
		// Custom JSON mapping or fallback: allow structured JSON string
		if scopeType == "" {
			scopeType = ProxyNodeScopeAll
			scopeValue = ""
		}
	default:
		return "", "", fmt.Errorf("unsupported proxy node scope type %q", scopeType)
	}
	return scopeType, scopeValue, nil
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
	if err := DB.Where("enabled = ?", true).Find(&enabledNodes).Error; err != nil {
		return nil, err
	}

	// 1. Direct Channel match
	channelIDStr := strconv.Itoa(channel.Id)
	var channelMatched []*ProxyNode
	for _, node := range enabledNodes {
		if node.ScopeType == ProxyNodeScopeChannel && node.ScopeValue == channelIDStr {
			channelMatched = append(channelMatched, node)
		}
	}
	if len(channelMatched) > 0 {
		return channelMatched, nil
	}

	// 2. Custom JSON scope match (channels or models)
	var customMatched []*ProxyNode
	for _, node := range enabledNodes {
		if node.ScopeType == ProxyNodeScopeCustom && node.ScopeValue != "" {
			var customScope struct {
				Channels []int    `json:"channels"`
				Models   []string `json:"models"`
			}
			if err := common.UnmarshalJsonStr(node.ScopeValue, &customScope); err == nil {
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
					customMatched = append(customMatched, node)
				}
			}
		}
	}
	if len(customMatched) > 0 {
		return customMatched, nil
	}

	// 3. Group match
	for _, group := range strings.Split(channel.Group, ",") {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		var groupMatched []*ProxyNode
		for _, node := range enabledNodes {
			if node.ScopeType == ProxyNodeScopeGroup && node.ScopeValue == group {
				groupMatched = append(groupMatched, node)
			}
		}
		if len(groupMatched) > 0 {
			return groupMatched, nil
		}
	}

	// 4. All scope fallback
	var allMatched []*ProxyNode
	for _, node := range enabledNodes {
		if node.ScopeType == ProxyNodeScopeAll {
			allMatched = append(allMatched, node)
		}
	}
	return allMatched, nil
}
