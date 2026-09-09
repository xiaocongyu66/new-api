package handler

// End-to-end regression for the production isolation loop: the channel health
// system isolated a failing model (abilities enabled=false), an admin then
// saved the channel from the web console, and the save rebuilt the abilities
// back to enabled — resurrecting a dead upstream model in the marketplace
// while route rows kept serving it.
//
// The test drives the real PUT /api/channel/ handler through a Fiber route
// (the exact surface the admin page hits), then asserts the database:
// channel_model_routes.enabled must agree with abilities.enabled, and a saved
// channel edit must preserve model-level isolation.

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/testutil"
	"github.com/QuantumNous/new-api/internal/usage"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelEditE2E(t *testing.T) *gorm.DB {
	t.Helper()

	originalMain := common.MainDatabaseType()
	originalLog := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")
	t.Cleanup(func() {
		common.SetDatabaseTypes(originalMain, originalLog)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	memoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = memoryCacheEnabled })

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	dbx.DB = db
	dbx.LogDB = db

	require.NoError(t, db.AutoMigrate(
		&identity.User{}, &channelpkg.Channel{}, &channelpkg.Ability{},
		&channelpkg.ChannelModelRoute{}, &channelpkg.ChannelModelHealth{},
		&channelpkg.Model{}, &channelpkg.Vendor{},
		&channelpkg.GatewayConfigRevision{}, &channelpkg.GatewayConfigOutbox{},
		&usage.Log{},
	))
	require.NoError(t, channelpkg.InitializeGatewayConfigRevision())

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// adminContextStub plays the auth middleware: it stamps the operator identity
// the real AdminAuth chain would have set.
func adminContextStub(c contract.Context) {
	c.Set("id", 1)
	c.Set("role", 100)
	c.Set("username", "root-admin")
	c.Next()
}

func putChannel(t *testing.T, payload map[string]any) {
	t.Helper()

	body, err := common.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, "/api/channel/", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp := testutil.ServeBufferedRoute(t, http.MethodPut, "/api/channel/",
		[]contract.Middleware{adminContextStub}, UpdateChannel, req)
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"the channel edit endpoint must answer 200, got body: %s", respBody)

	envelope := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{}
	require.NoError(t, common.Unmarshal(respBody, &envelope))
	require.True(t, envelope.Success,
		"the channel edit must succeed like the admin page expects, message: %s", envelope.Message)
}

func abilityEnabledByModelE2E(t *testing.T, db *gorm.DB, channelID int) map[string]bool {
	t.Helper()

	rows := []channelpkg.Ability{}
	require.NoError(t, db.Where("channel_id = ?", channelID).Find(&rows).Error)
	enabled := make(map[string]bool)
	for _, row := range rows {
		previous, seen := enabled[row.Model]
		if seen {
			require.Equal(t, previous, row.Enabled,
				"ability rows of one model must not disagree across groups")
		}
		enabled[row.Model] = row.Enabled
	}
	return enabled
}

func routeEnabledByAliasE2E(t *testing.T, db *gorm.DB, channelID int) map[string]bool {
	t.Helper()

	rows := []channelpkg.ChannelModelRoute{}
	require.NoError(t, db.Where("channel_id = ?", channelID).Find(&rows).Error)
	enabled := make(map[string]bool)
	for _, row := range rows {
		previous, seen := enabled[row.PublicModelAlias]
		if seen {
			require.Equal(t, previous, row.Enabled,
				"route rows of one alias must not disagree across groups")
		}
		enabled[row.PublicModelAlias] = row.Enabled
	}
	return enabled
}

func TestChannelEditViaPagePreservesModelIsolation(t *testing.T) {
	db := setupChannelEditE2E(t)

	baseURL := "https://upstream.example.com"
	channel := channelpkg.Channel{
		Id: 9201, Type: 1, Name: "page-edit", Key: "sk-page-edit",
		BaseURL: &baseURL,
		Models:  "dead-model,live-model", Group: "g1,g2",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, channel.Insert())

	// The health system isolated the failing model after repeated cooldowns.
	require.NoError(t, channelpkg.DisableChannelModel(9201, "dead-model"))
	assert.False(t, abilityEnabledByModelE2E(t, db, 9201)["dead-model"])
	assert.False(t, routeEnabledByAliasE2E(t, db, 9201)["dead-model"])

	// The admin opens the channel page, changes an unrelated field and saves:
	// the page PUTs the full channel object. The isolated model is still in
	// the list, so the save must not resurrect it.
	putChannel(t, map[string]any{
		"id":       9201,
		"type":     1,
		"name":     "page-edit",
		"key":      "sk-page-edit",
		"base_url": "https://upstream.example.com",
		"models":   "dead-model,live-model",
		"group":    "g1,g2",
	})

	abilities := abilityEnabledByModelE2E(t, db, 9201)
	routes := routeEnabledByAliasE2E(t, db, 9201)
	assert.False(t, abilities["dead-model"],
		"saving the channel from the page must preserve model isolation in abilities")
	assert.False(t, routes["dead-model"],
		"route rows must agree with the isolated abilities after a page save")
	assert.True(t, abilities["live-model"])
	assert.True(t, routes["live-model"])

	// Editing the model list through the page keeps the surviving isolated
	// model off and brings the new model up everywhere.
	putChannel(t, map[string]any{
		"id":       9201,
		"type":     1,
		"name":     "page-edit",
		"key":      "sk-page-edit",
		"base_url": "https://upstream.example.com",
		"models":   "dead-model,live-model,new-model",
		"group":    "g1,g2",
	})

	abilities = abilityEnabledByModelE2E(t, db, 9201)
	routes = routeEnabledByAliasE2E(t, db, 9201)
	assert.False(t, abilities["dead-model"])
	assert.False(t, routes["dead-model"])
	assert.True(t, abilities["new-model"], "a newly added model must be enabled")
	assert.True(t, routes["new-model"], "a newly added model must be routable")
}

func TestChannelEditViaPageKeepsAbilitiesAndRoutesAgreeing(t *testing.T) {
	db := setupChannelEditE2E(t)

	channel := channelpkg.Channel{
		Id: 9202, Type: 1, Name: "consistency", Key: "sk-consistency",
		Models: "model-a,model-b", Group: "default",
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, channel.Insert())

	// A page save that removes a model must drop its abilities AND its route
	// rows together; no marketplace-visible ghost may survive.
	putChannel(t, map[string]any{
		"id":     9202,
		"type":   1,
		"name":   "consistency",
		"key":    "sk-consistency",
		"models": "model-a",
		"group":  "default",
	})

	abilities := abilityEnabledByModelE2E(t, db, 9202)
	routes := routeEnabledByAliasE2E(t, db, 9202)
	assert.Equal(t, map[string]bool{"model-a": true}, abilities,
		"removed model must not keep ability rows behind")
	assert.Equal(t, map[string]bool{"model-a": true}, routes,
		"removed model must not keep route rows behind")
}
