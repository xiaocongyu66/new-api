package ops

import (
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/egress"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProxyNodeStorageEncryptsAndRoundTrips(t *testing.T) {
	previousDB := dbx.DB
	previousSecret := common.CryptoSecret
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&egress.ProxyNode{}))
	dbx.DB = db
	common.CryptoSecret = "test-secret"
	t.Cleanup(func() { dbx.DB = previousDB; common.CryptoSecret = previousSecret })

	node, err := createProxyNodeRecord(ProxyNodeInput{
		Name:       "edge",
		Enabled:    true,
		Proxy:      "http://user:pass@example.com:8080",
		ScopeType:  egress.ProxyNodeScopeCustom,
		ScopeValue: "",
	})
	require.NoError(t, err)
	assert.NotContains(t, node.EncryptedProxyConfig, "user")
	assert.NotContains(t, node.EncryptedProxyConfig, "pass")

	parsed, err := DecryptProxyNodeConfig(node)
	require.NoError(t, err)
	assert.Equal(t, "http", parsed.Protocol)
	assert.Contains(t, string(parsed.OutboundJSON), "example.com")
}
