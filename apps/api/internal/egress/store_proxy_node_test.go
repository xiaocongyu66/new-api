package egress

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/model"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProxyNodeScopeNormalization(t *testing.T) {
	tests := []struct {
		name, scopeType, scopeValue, wantValue string
		wantErr                                bool
	}{
		{name: "custom", scopeType: ProxyNodeScopeCustom, scopeValue: `{"models":[],"channels":[1]}`, wantValue: `{"models":[],"channels":[1]}`},
		{name: "empty type falls back to custom", scopeType: "", scopeValue: "", wantValue: ""},
		{name: "unknown type rejected", scopeType: "all", wantErr: true},
		{name: "channel type rejected", scopeType: "channel", scopeValue: "42", wantErr: true},
		{name: "group type rejected", scopeType: "group", scopeValue: "premium", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotValue, err := NormalizeProxyNodeScope(tt.scopeType, tt.scopeValue)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, ProxyNodeScopeCustom, gotType)
			assert.Equal(t, tt.wantValue, gotValue)
		})
	}
}

func TestProxyNodePublicConversionDoesNotExposeConfiguration(t *testing.T) {
	lastProbe := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	node := ProxyNode{
		ID:                   7,
		Name:                 "edge",
		Enabled:              true,
		EncryptedProxyConfig: "ciphertext-with-secret",
		Protocol:             "vless",
		ScopeType:            ProxyNodeScopeCustom,
		ScopeValue:           `{"models":["gpt-4o"],"channels":[42]}`,
		Health:               0.8,
		FailureCount:         2,
		LastError:            "timeout",
		LastProbeAt:          &lastProbe,
	}

	public := node.Public()
	assert.Equal(t, uint(7), public.ID)
	assert.Equal(t, ProxyNodeScopeCustom, public.ScopeType)
	assert.Equal(t, `{"models":["gpt-4o"],"channels":[42]}`, public.ScopeValue)
	assert.Equal(t, "vless", public.Protocol)
	assert.Equal(t, 0.8, public.Health)
	assert.Equal(t, 2, public.FailureCount)
	assert.Equal(t, "timeout", public.LastError)
	encoded, err := json.Marshal(public)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "ciphertext-with-secret")
}

func TestProxyNodePersistenceFields(t *testing.T) {
	lastProbe := time.Now().UTC()
	node := ProxyNode{LastProbeAt: &lastProbe, CooldownUntil: &lastProbe}
	assert.Equal(t, &lastProbe, node.LastProbeAt)
	assert.Equal(t, &lastProbe, node.CooldownUntil)
}

func TestGetProxyNodesForChannelMatchesCustomScopeOnly(t *testing.T) {
	previousDB := dbx.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ProxyNode{}))
	dbx.DB = db
	t.Cleanup(func() { dbx.DB = previousDB })

	require.NoError(t, db.Create(&ProxyNode{Name: "by-channel", Enabled: true, ScopeType: ProxyNodeScopeCustom, ScopeValue: `{"models":[],"channels":[42]}`}).Error)
	require.NoError(t, db.Create(&ProxyNode{Name: "by-model", Enabled: true, ScopeType: ProxyNodeScopeCustom, ScopeValue: `{"models":["gpt-4o"],"channels":[]}`}).Error)
	require.NoError(t, db.Create(&ProxyNode{Name: "unrelated", Enabled: true, ScopeType: ProxyNodeScopeCustom, ScopeValue: `{"models":[],"channels":[7]}`}).Error)
	require.NoError(t, db.Create(&ProxyNode{Name: "disabled-hit", Enabled: false, ScopeType: ProxyNodeScopeCustom, ScopeValue: `{"models":[],"channels":[42]}`}).Error)

	channel := &model.Channel{Id: 42, Group: "premium"}

	// Channel-ID match returns only enabled nodes whose custom scope lists channel 42.
	nodes, err := GetProxyNodesForChannel(channel)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "by-channel", nodes[0].Name)

	// Model-name match returns nodes whose custom scope lists the model.
	nodes, err = GetProxyNodesForChannelAndModel(&model.Channel{Id: 99}, "GPT-4O")
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "by-model", nodes[0].Name)

	// No match anywhere returns empty.
	nodes, err = GetProxyNodesForChannelAndModel(&model.Channel{Id: 1234}, "no-such-model")
	require.NoError(t, err)
	assert.Empty(t, nodes)
}
