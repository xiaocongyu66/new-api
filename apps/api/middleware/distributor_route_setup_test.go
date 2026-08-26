package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withProxyNodeDB(t *testing.T) {
	t.Helper()
	prevDB := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.ProxyNode{}))
	model.DB = db
	t.Cleanup(func() { model.DB = prevDB })
}

// TestSetupContextForSelectedChannelUsesSelectedRoute pins key attribution:
// the context must carry exactly the key, key index and channel of the
// selected route unit — repeated setups must not rotate to another key.
func TestSetupContextForSelectedChannelUsesSelectedRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withProxyNodeDB(t)

	channel := &model.Channel{
		Id:     77,
		Name:   "multi-key-channel",
		Type:   constant.ChannelTypeOpenAI,
		Key:    "sk-a,sk-b,sk-c",
		Status: common.ChannelStatusEnabled,
	}
	channel.ChannelInfo.IsMultiKey = true

	newCtx := func() *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		return ctx
	}

	route := &model.SelectedRoute{
		RouteId:       1,
		Group:         "default",
		Alias:         "gpt-x",
		ChannelId:     77,
		KeyIndex:      2,
		Key:           "sk-c",
		UpstreamModel: "upstream-x",
		Channel:       channel,
	}

	ctx := newCtx()
	setupErr := SetupContextForSelectedChannel(ctx, route, "gpt-x")
	_ = setupErr
	require.Nil(t, setupErr)
	assert.Equal(t, 77, common.GetContextKeyInt(ctx, constant.ContextKeyChannelId))
	assert.Equal(t, "sk-c", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
	assert.Equal(t, true, common.GetContextKeyBool(ctx, constant.ContextKeyChannelIsMultiKey))
	assert.Equal(t, 2, common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex))

	// Key attribution is deterministic: no rotation on re-setup.
	ctx2 := newCtx()
	require.Nil(t, SetupContextForSelectedChannel(ctx2, route, "gpt-x"))
	assert.Equal(t, "sk-c", common.GetContextKeyString(ctx2, constant.ContextKeyChannelKey))
	assert.Equal(t, 2, common.GetContextKeyInt(ctx2, constant.ContextKeyChannelMultiKeyIndex))

	// Single-key channel reports index 0 explicitly.
	single := &model.Channel{Id: 78, Type: constant.ChannelTypeOpenAI, Key: "sk-solo", Status: common.ChannelStatusEnabled}
	singleRoute := &model.SelectedRoute{
		RouteId: 2, Group: "default", Alias: "gpt-x",
		ChannelId: 78, KeyIndex: 0, Key: "sk-solo",
		UpstreamModel: "upstream-x", Channel: single,
	}
	ctx3 := newCtx()
	require.Nil(t, SetupContextForSelectedChannel(ctx3, singleRoute, "gpt-x"))
	assert.Equal(t, false, common.GetContextKeyBool(ctx3, constant.ContextKeyChannelIsMultiKey))
	assert.Equal(t, 0, common.GetContextKeyInt(ctx3, constant.ContextKeyChannelMultiKeyIndex))
	assert.Equal(t, "sk-solo", common.GetContextKeyString(ctx3, constant.ContextKeyChannelKey))
}
