package compose

import (
	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/QuantumNous/new-api/internal/transport/handler"
	"net/http"

	"github.com/QuantumNous/new-api/internal/identity/policy"
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/QuantumNous/new-api/internal/transport/middleware"
)

type permissionRoute struct {
	method     string
	path       string
	permission policy.Permission
	handler    contract.Handler
}

func registerChannelRoutes(apiRouter contract.Routes) {
	channelRoute := apiRouter.Group("/channel")
	channelRoute.Use(security.AdminAuth())

	channelRoute.POST("/:id/key",
		security.RootAuth(),
		middleware.CriticalRateLimit(),
		middleware.DisableCache(),
		middleware.SecureVerificationRequired(),
		handler.GetChannelKey,
	)

	for _, route := range channelPermissionRoutes {
		channelRoute.Handle(route.method, route.path,
			security.RequirePermission(route.permission),
			route.handler,
		)
	}
}

var channelPermissionRoutes = []permissionRoute{
	{method: http.MethodGet, path: "/", permission: policy.ChannelRead, handler: handler.GetAllChannels},
	{method: http.MethodGet, path: "/search", permission: policy.ChannelRead, handler: handler.SearchChannels},
	{method: http.MethodGet, path: "/models", permission: policy.ChannelRead, handler: handler.ChannelListModels},
	{method: http.MethodGet, path: "/models_enabled", permission: policy.ChannelRead, handler: handler.EnabledListModels},
	{method: http.MethodGet, path: "/ops", permission: policy.ChannelRead, handler: handler.GetChannelOps},
	{method: http.MethodGet, path: "/:id", permission: policy.ChannelRead, handler: handler.GetChannel},
	{method: http.MethodGet, path: "/test", permission: policy.ChannelOperate, handler: handler.TestAllChannels},
	{method: http.MethodGet, path: "/test/:id", permission: policy.ChannelOperate, handler: handler.TestChannel},
	{method: http.MethodGet, path: "/update_balance", permission: policy.ChannelOperate, handler: billing.UpdateAllChannelsBalance},
	{method: http.MethodGet, path: "/update_balance/:id", permission: policy.ChannelOperate, handler: billing.UpdateChannelBalance},
	{method: http.MethodPost, path: "/", permission: policy.ChannelSensitiveWrite, handler: handler.AddChannel},
	{method: http.MethodPut, path: "/", permission: policy.ChannelWrite, handler: handler.UpdateChannel},
	{method: http.MethodPost, path: "/status/batch", permission: policy.ChannelOperate, handler: handler.BatchUpdateChannelStatus},
	{method: http.MethodPost, path: "/:id/status", permission: policy.ChannelOperate, handler: handler.UpdateChannelStatus},
	{method: http.MethodDelete, path: "/disabled", permission: policy.ChannelSensitiveWrite, handler: handler.DeleteDisabledChannel},
	{method: http.MethodPost, path: "/tag/disabled", permission: policy.ChannelOperate, handler: handler.DisableTagChannels},
	{method: http.MethodPost, path: "/tag/enabled", permission: policy.ChannelOperate, handler: handler.EnableTagChannels},
	{method: http.MethodPut, path: "/tag", permission: policy.ChannelWrite, handler: handler.EditTagChannels},
	{method: http.MethodDelete, path: "/:id", permission: policy.ChannelSensitiveWrite, handler: handler.DeleteChannel},
	{method: http.MethodPost, path: "/batch", permission: policy.ChannelSensitiveWrite, handler: handler.DeleteChannelBatch},
	{method: http.MethodPost, path: "/fix", permission: policy.ChannelOperate, handler: handler.FixChannelsAbilities},
	{method: http.MethodGet, path: "/fetch_models/:id", permission: policy.ChannelOperate, handler: handler.FetchUpstreamModels},
	{method: http.MethodPost, path: "/fetch_models", permission: policy.ChannelSensitiveWrite, handler: handler.FetchModels},
	{method: http.MethodPost, path: "/:id/codex/refresh", permission: policy.ChannelSensitiveWrite, handler: handler.RefreshCodexChannelCredential},
	{method: http.MethodGet, path: "/:id/codex/usage", permission: policy.ChannelRead, handler: handler.GetCodexChannelUsage},
	{method: http.MethodGet, path: "/:id/codex/usage/reset-credits", permission: policy.ChannelRead, handler: handler.GetCodexChannelRateLimitResetCredits},
	{method: http.MethodPost, path: "/:id/codex/usage/reset", permission: policy.ChannelOperate, handler: handler.ResetCodexChannelUsage},
	{method: http.MethodPost, path: "/ollama/pull", permission: policy.ChannelSensitiveWrite, handler: handler.OllamaPullModel},
	{method: http.MethodPost, path: "/ollama/pull/stream", permission: policy.ChannelSensitiveWrite, handler: handler.OllamaPullModelStream},
	{method: http.MethodDelete, path: "/ollama/delete", permission: policy.ChannelSensitiveWrite, handler: handler.OllamaDeleteModel},
	{method: http.MethodGet, path: "/ollama/version/:id", permission: policy.ChannelSensitiveWrite, handler: handler.OllamaVersion},
	{method: http.MethodPost, path: "/batch/tag", permission: policy.ChannelWrite, handler: handler.BatchSetChannelTag},
	{method: http.MethodGet, path: "/tag/models", permission: policy.ChannelRead, handler: handler.GetTagModels},
	{method: http.MethodPost, path: "/copy/:id", permission: policy.ChannelSensitiveWrite, handler: handler.CopyChannel},
	{method: http.MethodPost, path: "/multi_key/manage", permission: policy.ChannelOperate, handler: handler.ManageMultiKeys},
	{method: http.MethodPost, path: "/upstream_updates/apply", permission: policy.ChannelWrite, handler: handler.ApplyChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/upstream_updates/apply_all", permission: policy.ChannelWrite, handler: handler.ApplyAllChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/upstream_updates/detect", permission: policy.ChannelOperate, handler: handler.DetectChannelUpstreamModelUpdates},
	{method: http.MethodPost, path: "/upstream_updates/detect_all", permission: policy.ChannelOperate, handler: handler.DetectAllChannelUpstreamModelUpdates},
	{method: http.MethodGet, path: "/route_unit/", permission: policy.ChannelRead, handler: handler.GetRouteUnitViews},
	{method: http.MethodGet, path: "/route_unit/aliases", permission: policy.ChannelRead, handler: handler.ListRouteUnitAliases},
	{method: http.MethodPut, path: "/route_unit/:id", permission: policy.ChannelWrite, handler: handler.UpdateRouteUnit},
	{method: http.MethodGet, path: "/health", permission: policy.ChannelRead, handler: handler.GetChannelModelHealth},
	{method: http.MethodPost, path: "/health/:action", permission: policy.ChannelOperate, handler: handler.UpdateChannelModelHealth},
}
