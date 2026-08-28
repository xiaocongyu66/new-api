package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCacheGetRandomSatisfiedChannelExcludesFailedRoute pins the route-level
// retry contract: excluding a failed route unit (channel + key index + model)
// removes exactly that unit from the candidate pool; excluding every unit yields
// nil instead of silently re-selecting a failed route. The Model field is part of
// the key because isolation is per (channel, key, model): a dead model on an
// otherwise healthy channel must not cost the channel its other models.
func TestCacheGetRandomSatisfiedChannelExcludesFailedRoute(t *testing.T) {
	db := setupChannelSelectAutoGroupsTest(t)
	const modelName = "route-exclude-model"
	createChannelSelectAutoGroupsChannel(t, db, 3101, "default", modelName)
	createChannelSelectAutoGroupsChannel(t, db, 3102, "default", modelName)
	model.InitChannelCache()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	param := &RetryParam{
		TokenGroup:    "default",
		Ctx:           ctx,
		ModelName:     modelName,
		ExcludeRoutes: make(map[model.RouteKey]bool),
	}
	seen := map[int]bool{}
	for range 60 {
		route, _, err := CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, err)
		require.NotNil(t, route)
		seen[route.ChannelId] = true
	}
	assert.Equal(t, map[int]bool{3101: true, 3102: true}, seen)

	// Exclude channel 3101's only key: selection must collapse to 3102.
	param.ExcludeRoutes[model.RouteKey{ChannelId: 3101, KeyIndex: 0, Model: modelName}] = true
	for range 40 {
		route, _, err := CacheGetRandomSatisfiedChannel(param)
		require.NoError(t, err)
		require.NotNil(t, route)
		assert.Equal(t, 3102, route.ChannelId)
	}

	// Exclude the remaining route: no candidate at all.
	param.ExcludeRoutes[model.RouteKey{ChannelId: 3102, KeyIndex: 0, Model: modelName}] = true
	route, _, err := CacheGetRandomSatisfiedChannel(param)
	require.NoError(t, err)
	assert.Nil(t, route)

	// A different key index on an otherwise excluded channel stays selectable.
	multiKeyParam := &RetryParam{
		TokenGroup:    "default",
		Ctx:           ctx,
		ModelName:     modelName,
		RequestPath:   "",
		ExcludeRoutes: make(map[model.RouteKey]bool),
	}
	multiKeyParam.ExcludeRoutes[model.RouteKey{ChannelId: 3101, KeyIndex: 1, Model: modelName}] = true
	route, _, err = CacheGetRandomSatisfiedChannel(multiKeyParam)
	require.NoError(t, err)
	require.NotNil(t, route)
}
