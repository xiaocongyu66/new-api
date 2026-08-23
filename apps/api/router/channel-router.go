package router

import (
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"net/http"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

type permissionRoute struct {
	method     string
	path       string
	permission authz.Permission
	handler    gin.HandlerFunc
}

func registerChannelRoutes(apiRouter *gin.RouterGroup) {
	channelRoute := apiRouter.Group("/channel")
	channelRoute.Use(ginadapter.Middleware(middleware.AdminAuth()))

	channelRoute.POST("/:id/key",
		ginadapter.Middleware(middleware.RootAuth()),
		ginadapter.Middleware(middleware.CriticalRateLimit()),
		ginadapter.Middleware(middleware.DisableCache()),
		ginadapter.Middleware(middleware.SecureVerificationRequired()),
		ginadapter.Handler(controller.GetChannelKey),
	)

	for _, route := range channelPermissionRoutes {
		channelRoute.Handle(route.method, route.path,
			ginadapter.Middleware(middleware.RequirePermission(route.permission)),
			route.handler,
		)
	}
}

var channelPermissionRoutes = []permissionRoute{
	{method: http.MethodGet, path: "/", permission: authz.ChannelRead, handler: ginadapter.Handler(controller.GetAllChannels)},
	{method: http.MethodGet, path: "/search", permission: authz.ChannelRead, handler: ginadapter.Handler(controller.SearchChannels)},
	{method: http.MethodGet, path: "/models", permission: authz.ChannelRead, handler: ginadapter.Handler(controller.ChannelListModels)},
	{method: http.MethodGet, path: "/models_enabled", permission: authz.ChannelRead, handler: ginadapter.Handler(controller.EnabledListModels)},
	{method: http.MethodGet, path: "/ops", permission: authz.ChannelRead, handler: ginadapter.Handler(controller.GetChannelOps)},
	{method: http.MethodGet, path: "/:id", permission: authz.ChannelRead, handler: ginadapter.Handler(controller.GetChannel)},
	{method: http.MethodGet, path: "/test", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.TestAllChannels)},
	{method: http.MethodGet, path: "/test/:id", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.TestChannel)},
	{method: http.MethodGet, path: "/update_balance", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.UpdateAllChannelsBalance)},
	{method: http.MethodGet, path: "/update_balance/:id", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.UpdateChannelBalance)},
	{method: http.MethodPost, path: "/", permission: authz.ChannelSensitiveWrite, handler: ginadapter.Handler(controller.AddChannel)},
	{method: http.MethodPut, path: "/", permission: authz.ChannelWrite, handler: ginadapter.Handler(controller.UpdateChannel)},
	{method: http.MethodPost, path: "/status/batch", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.BatchUpdateChannelStatus)},
	{method: http.MethodPost, path: "/:id/status", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.UpdateChannelStatus)},
	{method: http.MethodDelete, path: "/disabled", permission: authz.ChannelSensitiveWrite, handler: ginadapter.Handler(controller.DeleteDisabledChannel)},
	{method: http.MethodPost, path: "/tag/disabled", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.DisableTagChannels)},
	{method: http.MethodPost, path: "/tag/enabled", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.EnableTagChannels)},
	{method: http.MethodPut, path: "/tag", permission: authz.ChannelWrite, handler: ginadapter.Handler(controller.EditTagChannels)},
	{method: http.MethodDelete, path: "/:id", permission: authz.ChannelSensitiveWrite, handler: ginadapter.Handler(controller.DeleteChannel)},
	{method: http.MethodPost, path: "/batch", permission: authz.ChannelSensitiveWrite, handler: ginadapter.Handler(controller.DeleteChannelBatch)},
	{method: http.MethodPost, path: "/fix", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.FixChannelsAbilities)},
	{method: http.MethodGet, path: "/fetch_models/:id", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.FetchUpstreamModels)},
	{method: http.MethodPost, path: "/fetch_models", permission: authz.ChannelSensitiveWrite, handler: ginadapter.Handler(controller.FetchModels)},
	{method: http.MethodPost, path: "/:id/codex/refresh", permission: authz.ChannelSensitiveWrite, handler: ginadapter.Handler(controller.RefreshCodexChannelCredential)},
	{method: http.MethodGet, path: "/:id/codex/usage", permission: authz.ChannelRead, handler: ginadapter.Handler(controller.GetCodexChannelUsage)},
	{method: http.MethodGet, path: "/:id/codex/usage/reset-credits", permission: authz.ChannelRead, handler: ginadapter.Handler(controller.GetCodexChannelRateLimitResetCredits)},
	{method: http.MethodPost, path: "/:id/codex/usage/reset", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.ResetCodexChannelUsage)},
	{method: http.MethodPost, path: "/ollama/pull", permission: authz.ChannelSensitiveWrite, handler: ginadapter.Handler(controller.OllamaPullModel)},
	{method: http.MethodPost, path: "/ollama/pull/stream", permission: authz.ChannelSensitiveWrite, handler: ginadapter.Handler(controller.OllamaPullModelStream)},
	{method: http.MethodDelete, path: "/ollama/delete", permission: authz.ChannelSensitiveWrite, handler: ginadapter.Handler(controller.OllamaDeleteModel)},
	{method: http.MethodGet, path: "/ollama/version/:id", permission: authz.ChannelSensitiveWrite, handler: ginadapter.Handler(controller.OllamaVersion)},
	{method: http.MethodPost, path: "/batch/tag", permission: authz.ChannelWrite, handler: ginadapter.Handler(controller.BatchSetChannelTag)},
	{method: http.MethodGet, path: "/tag/models", permission: authz.ChannelRead, handler: ginadapter.Handler(controller.GetTagModels)},
	{method: http.MethodPost, path: "/copy/:id", permission: authz.ChannelSensitiveWrite, handler: ginadapter.Handler(controller.CopyChannel)},
	{method: http.MethodPost, path: "/multi_key/manage", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.ManageMultiKeys)},
	{method: http.MethodPost, path: "/upstream_updates/apply", permission: authz.ChannelWrite, handler: ginadapter.Handler(controller.ApplyChannelUpstreamModelUpdates)},
	{method: http.MethodPost, path: "/upstream_updates/apply_all", permission: authz.ChannelWrite, handler: ginadapter.Handler(controller.ApplyAllChannelUpstreamModelUpdates)},
	{method: http.MethodPost, path: "/upstream_updates/detect", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.DetectChannelUpstreamModelUpdates)},
	{method: http.MethodPost, path: "/upstream_updates/detect_all", permission: authz.ChannelOperate, handler: ginadapter.Handler(controller.DetectAllChannelUpstreamModelUpdates)},
}
