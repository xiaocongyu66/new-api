package compose

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/QuantumNous/new-api/internal/identity/policy"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/QuantumNous/new-api/internal/transport/handler"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelStatusRoutesUseOperatePermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodPost, "/:id/status", policy.ChannelOperate, handler.UpdateChannelStatus)
	assertChannelRoutePermission(t, http.MethodPost, "/status/batch", policy.ChannelOperate, handler.BatchUpdateChannelStatus)
	assertChannelRoutePermission(t, http.MethodPut, "/", policy.ChannelWrite, handler.UpdateChannel)
}

func TestChannelDeleteRoutesUseSensitiveWritePermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodDelete, "/:id", policy.ChannelSensitiveWrite, handler.DeleteChannel)
	assertChannelRoutePermission(t, http.MethodPost, "/batch", policy.ChannelSensitiveWrite, handler.DeleteChannelBatch)
	assertChannelRoutePermission(t, http.MethodDelete, "/disabled", policy.ChannelSensitiveWrite, handler.DeleteDisabledChannel)
	assertChannelRoutePermission(t, http.MethodPut, "/", policy.ChannelWrite, handler.UpdateChannel)
	assertChannelRoutePermission(t, http.MethodPut, "/tag", policy.ChannelWrite, handler.EditTagChannels)
	assertChannelRoutePermission(t, http.MethodPost, "/batch/tag", policy.ChannelWrite, handler.BatchSetChannelTag)
}

func TestChannelStatusRoutesRegisterWithoutConflict(t *testing.T) {
	api := fiberadapter.NewEngine(func(contract.Context, any) {}).Group("/api")

	require.NotPanics(t, func() {
		registerChannelRoutes(api)
	})
}

func assertChannelRoutePermission(t *testing.T, method string, path string, permission policy.Permission, handler any) {
	t.Helper()
	for _, route := range channelPermissionRoutes {
		if route.method == method && route.path == path {
			assert.Equal(t, permission, route.permission)
			assert.Equal(t, reflect.ValueOf(handler).Pointer(), reflect.ValueOf(route.handler).Pointer())
			return
		}
	}
	t.Fatalf("route %s %s not found", method, path)
}
