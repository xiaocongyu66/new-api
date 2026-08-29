package compose

import (
	"github.com/QuantumNous/new-api/internal/billing"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/identity/policy"
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/QuantumNous/new-api/internal/transport/handler"
	"github.com/QuantumNous/new-api/internal/transport/middleware"
	"github.com/QuantumNous/new-api/internal/usage"

	// Import oauth package to register providers via init()
	_ "github.com/QuantumNous/new-api/internal/security/oauth"

	"github.com/QuantumNous/new-api/internal/security"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func SetApiRouter(router *gin.Engine) {
	apiRouter := router.Group("/api")
	apiRouter.Use(middleware.RouteTag("api"))
	apiRouter.Use(gzip.Gzip(gzip.DefaultCompression))
	apiRouter.Use(ginadapter.Middleware(middleware.BodyStorageCleanup())) // 清理请求体存储
	apiRouter.Use(ginadapter.Middleware(middleware.GlobalAPIRateLimit()))
	anonymousRequestBodyLimit := ginadapter.Middleware(middleware.AnonymousRequestBodyLimit())
	{
		apiRouter.GET("/setup", ginadapter.Handler(ops.GetSetup))
		apiRouter.POST("/setup", anonymousRequestBodyLimit, ginadapter.Handler(ops.PostSetup))
		apiRouter.GET("/status", ginadapter.Handler(handler.GetStatus))
		apiRouter.GET("/uptime/status", ginadapter.Handler(ops.GetUptimeKumaStatus))
		apiRouter.GET("/models", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(handler.DashboardListModels))
		apiRouter.GET("/status/test", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(handler.TestStatus))
		apiRouter.POST("/karmada/session", ginadapter.Middleware(security.RootAuth()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(handler.CreateKarmadaDashboardSession))
		apiRouter.GET("/notice", ginadapter.Handler(handler.GetNotice))
		apiRouter.GET("/user-agreement", ginadapter.Handler(handler.GetUserAgreement))
		apiRouter.GET("/privacy-policy", ginadapter.Handler(handler.GetPrivacyPolicy))
		apiRouter.GET("/about", ginadapter.Handler(handler.GetAbout))
		//apiRouter.GET("/midjourney", ginadapter.Handler(handler.GetMidjourney))
		apiRouter.GET("/home_page_content", ginadapter.Handler(handler.GetHomePageContent))
		apiRouter.GET("/pricing", ginadapter.Middleware(middleware.HeaderNavModuleAuth("pricing")), ginadapter.Handler(handler.GetPricing))
		// /api/log routes
		logRoute := apiRouter.Group("/log")
		logRoute.GET("/", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(handler.GetAllLogs))
		logRoute.GET("/stat", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(handler.GetLogsStat))
		logRoute.GET("/self/stat", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(handler.GetLogsSelfStat))
		logRoute.GET("/channel_affinity_usage_cache", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(handler.GetChannelAffinityUsageCacheStats))
		logRoute.GET("/search", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(handler.SearchAllLogs))
		logRoute.GET("/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(handler.GetUserLogs))
		logRoute.GET("/self/search", ginadapter.Middleware(security.UserAuth()), ginadapter.Middleware(middleware.SearchRateLimit()), ginadapter.Handler(handler.SearchUserLogs))
		logRoute.Use(middleware.CORS(), ginadapter.Middleware(middleware.CriticalRateLimit()))
		{
			logRoute.GET("/token", ginadapter.Middleware(security.TokenAuthReadOnly()), ginadapter.Handler(handler.GetLogByKey))
		}

		// /api/data routes
		dataRoute := apiRouter.Group("/data")
		dataRoute.GET("/", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(handler.GetAllQuotaDates))
		dataRoute.GET("/users", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(handler.GetQuotaDatesByUser))
		dataRoute.GET("/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(handler.GetUserQuotaDates))
		dataRoute.GET("/flow", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(handler.GetAllFlowQuotaDates))
		dataRoute.GET("/flow/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(handler.GetUserFlowQuotaDates))

		// /api/rankings route
		apiRouter.GET("/rankings", ginadapter.Middleware(middleware.HeaderNavModuleAuth("rankings")), ginadapter.Handler(handler.GetRankings))

		// /api/perf-metrics routes
		perfMetricsRoute := apiRouter.Group("/perf-metrics")
		perfMetricsRoute.Use(ginadapter.Middleware(middleware.HeaderNavModulePublicOrUserAuth("pricing")))
		{
			perfMetricsRoute.GET("/summary", ginadapter.Handler(handler.GetPerfMetricsSummary))
			perfMetricsRoute.GET("", ginadapter.Handler(handler.GetPerfMetrics))
		}

		// /api/performance routes
		performanceRoute := apiRouter.Group("/performance")
		performanceRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(policy.SystemSettings)))
		{
			performanceRoute.GET("/stats", ginadapter.Handler(usage.GetPerformanceStats))
			performanceRoute.DELETE("/disk_cache", ginadapter.Handler(usage.ClearDiskCache))
			performanceRoute.POST("/reset_stats", ginadapter.Handler(usage.ResetPerformanceStats))
			performanceRoute.POST("/gc", ginadapter.Handler(usage.ForceGC))
			performanceRoute.GET("/logs", ginadapter.Handler(usage.GetLogFiles))
			performanceRoute.DELETE("/logs", ginadapter.Handler(usage.CleanupLogFiles))
		}
		apiRouter.GET("/verification", ginadapter.Middleware(middleware.EmailVerificationRateLimit()), ginadapter.Middleware(middleware.TurnstileCheck()), ginadapter.Handler(identity.SendEmailVerification))
		apiRouter.GET("/reset_password", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.TurnstileCheck()), ginadapter.Handler(identity.SendPasswordResetEmail))
		apiRouter.POST("/user/reset", ginadapter.Middleware(middleware.CriticalRateLimit()), anonymousRequestBodyLimit, ginadapter.Handler(identity.ResetPassword))
		// OAuth routes - specific routes must come before :provider wildcard
		apiRouter.POST("/oauth/state", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Middleware(security.TryUserAuth()), anonymousRequestBodyLimit, ginadapter.Handler(handler.GenerateOAuthCode))
		apiRouter.POST("/oauth/email/bind", ginadapter.Middleware(security.UserAuth()), ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(identity.EmailBind))
		// Non-standard OAuth (WeChat, Telegram) - keep original routes
		apiRouter.GET("/oauth/wechat", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.WeChatAuth))
		apiRouter.POST("/oauth/wechat/bind", ginadapter.Middleware(security.UserAuth()), ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(identity.WeChatBind))
		apiRouter.GET("/oauth/telegram/login", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.TelegramLogin))
		apiRouter.POST("/oauth/telegram/bind/start", ginadapter.Middleware(security.UserAuth()), ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.TelegramBindStart))
		apiRouter.GET("/oauth/telegram/bind/:flow_token", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.TelegramBind))
		// Standard OAuth providers (GitHub, Discord, OIDC, LinuxDO) - unified route
		apiRouter.GET("/oauth/:provider", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Middleware(security.TryUserAuth()), ginadapter.Handler(handler.HandleOAuth))
		apiRouter.GET("/ratio_config", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(handler.GetRatioConfig))

		apiRouter.POST("/stripe/webhook", anonymousRequestBodyLimit, ginadapter.Handler(billing.StripeWebhook))
		apiRouter.POST("/creem/webhook", anonymousRequestBodyLimit, ginadapter.Handler(billing.CreemWebhook))
		apiRouter.POST("/waffo/webhook", anonymousRequestBodyLimit, ginadapter.Handler(billing.WaffoWebhook))
		// :env separates test vs prod URLs so the operator can register each
		// in Pancake's matching webhook slot; handler enforces env match.
		apiRouter.POST("/waffo-pancake/webhook/:env", anonymousRequestBodyLimit, ginadapter.Handler(billing.WaffoPancakeWebhook))

		// Universal secure verification routes
		apiRouter.POST("/verify", ginadapter.Middleware(security.UserAuth()), ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.UniversalVerify))

		userRoute := apiRouter.Group("/user")
		{
			userRoute.POST("/auth/refresh", ginadapter.Middleware(security.SessionCookieOriginGuard()), ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.RefreshAuth))
			userRoute.POST("/auth/logout", ginadapter.Middleware(security.SessionCookieOriginGuard()), ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.AuthLogout))
			userRoute.POST("/register", ginadapter.Middleware(middleware.CriticalRateLimit()), anonymousRequestBodyLimit, ginadapter.Middleware(middleware.TurnstileCheck()), ginadapter.Handler(identity.Register))
			userRoute.POST("/login", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), anonymousRequestBodyLimit, ginadapter.Middleware(middleware.TurnstileCheck()), ginadapter.Handler(identity.Login))
			userRoute.POST("/login/2fa", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), anonymousRequestBodyLimit, ginadapter.Handler(identity.Verify2FALogin))
			userRoute.POST("/passkey/login/begin", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), anonymousRequestBodyLimit, ginadapter.Handler(identity.PasskeyLoginBegin))
			userRoute.POST("/passkey/login/finish", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), anonymousRequestBodyLimit, ginadapter.Handler(identity.PasskeyLoginFinish))
			//userRoute.POST("/tokenlog", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.TokenLog))
			userRoute.POST("/epay/notify", anonymousRequestBodyLimit, ginadapter.Handler(billing.EpayNotify))
			userRoute.GET("/epay/notify", ginadapter.Handler(billing.EpayNotify))
			userRoute.GET("/groups", ginadapter.Handler(handler.GetUserGroups))

			selfRoute := userRoute.Group("/")
			selfRoute.Use(ginadapter.Middleware(security.UserAuth()))
			{
				selfRoute.GET("/sessions", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.GetLoginSessions))
				selfRoute.DELETE("/sessions/:sid", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.DeleteLoginSession))
				selfRoute.POST("/sessions/revoke-others", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.RevokeOtherLoginSessions))
				selfRoute.GET("/self/groups", ginadapter.Handler(handler.GetUserGroups))
				selfRoute.GET("/self", ginadapter.Handler(identity.GetSelf))
				selfRoute.GET("/models", ginadapter.Handler(identity.GetUserModels))
				selfRoute.PUT("/self", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.UpdateSelf))
				selfRoute.DELETE("/self", ginadapter.Handler(identity.DeleteSelf))
				selfRoute.GET("/token", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.UserCriticalRateLimit("access-token")), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.GenerateAccessToken))
				selfRoute.GET("/passkey", ginadapter.Handler(identity.PasskeyStatus))
				selfRoute.POST("/passkey/register/begin", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.PasskeyRegisterBegin))
				selfRoute.POST("/passkey/register/finish", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.PasskeyRegisterFinish))
				selfRoute.POST("/passkey/verify/begin", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.PasskeyVerifyBegin))
				selfRoute.POST("/passkey/verify/finish", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.PasskeyVerifyFinish))
				selfRoute.DELETE("/passkey", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.PasskeyDelete))
				selfRoute.GET("/aff", ginadapter.Handler(identity.GetAffCode))
				selfRoute.GET("/topup/info", ginadapter.Handler(billing.GetTopUpInfo))
				selfRoute.GET("/topup/self", ginadapter.Handler(billing.GetUserTopUps))
				selfRoute.POST("/topup", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(identity.TopUp))
				selfRoute.POST("/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(billing.RequestEpay))
				selfRoute.POST("/amount", ginadapter.Handler(billing.RequestAmount))
				selfRoute.POST("/stripe/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(billing.RequestStripePay))
				selfRoute.POST("/stripe/amount", ginadapter.Handler(billing.RequestStripeAmount))
				selfRoute.POST("/creem/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(billing.RequestCreemPay))
				selfRoute.POST("/waffo/amount", ginadapter.Handler(billing.RequestWaffoAmount))
				selfRoute.POST("/waffo/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(billing.RequestWaffoPay))
				selfRoute.POST("/waffo-pancake/amount", ginadapter.Handler(billing.RequestWaffoPancakeAmount))
				selfRoute.POST("/waffo-pancake/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(billing.RequestWaffoPancakePay))
				selfRoute.POST("/aff_transfer", ginadapter.Middleware(middleware.UserCriticalRateLimit("aff-transfer")), ginadapter.Middleware(billing.RequirePaymentComplianceMiddleware), ginadapter.Handler(identity.TransferAffQuota))
				selfRoute.PUT("/setting", ginadapter.Handler(identity.UpdateUserSettingHandler))

				// 2FA routes
				selfRoute.GET("/2fa/status", ginadapter.Handler(identity.Get2FAStatus))
				selfRoute.POST("/2fa/setup", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.Setup2FA))
				selfRoute.POST("/2fa/enable", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.Enable2FA))
				selfRoute.POST("/2fa/disable", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.Disable2FA))
				selfRoute.POST("/2fa/backup_codes", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.RegenerateBackupCodes))

				// Check-in routes
				selfRoute.GET("/checkin", ginadapter.Handler(billing.GetCheckinStatus))
				selfRoute.POST("/checkin", ginadapter.Middleware(middleware.TurnstileCheck()), ginadapter.Handler(billing.DoCheckin))

				// Custom OAuth bindings
				selfRoute.GET("/oauth/bindings", ginadapter.Handler(identity.GetUserOAuthBindings))
				selfRoute.DELETE("/oauth/bindings/:provider_id", ginadapter.Handler(identity.UnbindCustomOAuth))
			}

			adminRoute := userRoute.Group("/")
			adminRoute.Use(ginadapter.Middleware(security.AdminAuth()))
			{
				adminRoute.GET("/", ginadapter.Handler(identity.GetAllUsers))
				adminRoute.GET("/topup", ginadapter.Handler(billing.GetAllTopUps))
				adminRoute.POST("/topup/complete", ginadapter.Handler(billing.AdminCompleteTopUp))
				adminRoute.GET("/search", ginadapter.Handler(identity.SearchUsersHandler))
				adminRoute.GET("/:id/oauth/bindings", ginadapter.Handler(identity.GetUserOAuthBindingsByAdmin))
				adminRoute.DELETE("/:id/oauth/bindings/:provider_id", ginadapter.Handler(identity.UnbindCustomOAuthByAdmin))
				adminRoute.DELETE("/:id/bindings/:binding_type", ginadapter.Handler(identity.AdminClearUserBinding))
				adminRoute.GET("/:id", ginadapter.Handler(identity.GetUser))
				adminRoute.POST("/", ginadapter.Handler(identity.CreateUser))
				adminRoute.POST("/manage", ginadapter.Handler(identity.ManageUser))
				adminRoute.PUT("/", ginadapter.Handler(identity.UpdateUser))
				adminRoute.DELETE("/:id", ginadapter.Handler(identity.DeleteUser))
				adminRoute.DELETE("/:id/reset_passkey", ginadapter.Handler(identity.AdminResetPasskey))

				// Admin 2FA routes
				adminRoute.GET("/2fa/stats", ginadapter.Handler(identity.Admin2FAStats))
				adminRoute.DELETE("/:id/2fa", ginadapter.Handler(identity.AdminDisable2FA))
			}
		}

		// Subscription billing (plans, purchase, admin management)
		subscriptionRoute := apiRouter.Group("/subscription")
		subscriptionRoute.Use(ginadapter.Middleware(security.UserAuth()))
		{
			subscriptionRoute.GET("/plans", ginadapter.Handler(billing.GetSubscriptionPlans))
			subscriptionRoute.GET("/self", ginadapter.Handler(billing.GetSubscriptionSelf))
			subscriptionRoute.PUT("/self/preference", ginadapter.Handler(billing.UpdateSubscriptionPreference))
			subscriptionRoute.POST("/balance/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(billing.SubscriptionRequestBalancePay))
			subscriptionRoute.POST("/epay/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(billing.SubscriptionRequestEpay))
			subscriptionRoute.POST("/stripe/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(billing.SubscriptionRequestStripePay))
			subscriptionRoute.POST("/creem/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(billing.SubscriptionRequestCreemPay))
			subscriptionRoute.POST("/waffo-pancake/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(billing.SubscriptionRequestWaffoPancakePay))
		}
		subscriptionAdminRoute := apiRouter.Group("/subscription/admin")
		subscriptionAdminRoute.Use(ginadapter.Middleware(security.AdminAuth()))
		{
			subscriptionAdminRoute.GET("/plans", ginadapter.Handler(billing.AdminListSubscriptionPlans))
			subscriptionAdminRoute.POST("/plans", ginadapter.Handler(billing.AdminCreateSubscriptionPlan))
			subscriptionAdminRoute.PUT("/plans/:id", ginadapter.Handler(billing.AdminUpdateSubscriptionPlan))
			subscriptionAdminRoute.PATCH("/plans/:id", ginadapter.Handler(billing.AdminUpdateSubscriptionPlanStatus))
			subscriptionAdminRoute.POST("/bind", ginadapter.Handler(billing.AdminBindSubscription))
			subscriptionAdminRoute.POST("/plans/:id/subscriptions/reset", ginadapter.Handler(billing.AdminResetPlanSubscriptions))

			// User subscription management (admin)
			subscriptionAdminRoute.GET("/users/:id/subscriptions", ginadapter.Handler(billing.AdminListUserSubscriptions))
			subscriptionAdminRoute.POST("/users/:id/subscriptions", ginadapter.Handler(billing.AdminCreateUserSubscription))
			subscriptionAdminRoute.POST("/users/:id/subscriptions/reset", ginadapter.Handler(billing.AdminResetUserSubscriptionsByPlan))
			subscriptionAdminRoute.POST("/user_subscriptions/:id/invalidate", ginadapter.Handler(billing.AdminInvalidateUserSubscription))
			subscriptionAdminRoute.DELETE("/user_subscriptions/:id", ginadapter.Handler(billing.AdminDeleteUserSubscription))
		}

		// Subscription payment callbacks (no auth)
		apiRouter.POST("/subscription/epay/notify", anonymousRequestBodyLimit, ginadapter.Handler(billing.SubscriptionEpayNotify))
		apiRouter.GET("/subscription/epay/notify", ginadapter.Handler(billing.SubscriptionEpayNotify))
		apiRouter.GET("/subscription/epay/return", ginadapter.Handler(billing.SubscriptionEpayReturn))
		optionRoute := apiRouter.Group("/option")
		optionRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(policy.SystemSettings)))
		{
			optionRoute.GET("/", ginadapter.Handler(ops.GetOptions))
			optionRoute.PUT("/", ginadapter.Handler(ops.UpdateOption))
			optionRoute.POST("/payment_compliance", ginadapter.Handler(billing.ConfirmPaymentCompliance))
			optionRoute.GET("/channel_affinity_cache", ginadapter.Handler(handler.GetChannelAffinityCacheStats))
			optionRoute.DELETE("/channel_affinity_cache", ginadapter.Handler(handler.ClearChannelAffinityCache))
			optionRoute.POST("/rest_model_ratio", ginadapter.Handler(handler.ResetModelRatio))
			optionRoute.GET("/waffo-pancake/catalog", ginadapter.Handler(billing.ListWaffoPancakeCatalog))
			optionRoute.POST("/waffo-pancake/pair", ginadapter.Handler(billing.CreateWaffoPancakePair))
			optionRoute.POST("/waffo-pancake/save", ginadapter.Handler(billing.SaveWaffoPancake))
			optionRoute.POST("/waffo-pancake/subscription-product", ginadapter.Handler(billing.CreateWaffoPancakeSubscriptionProduct))
			optionRoute.GET("/waffo-pancake/subscription-product-options", ginadapter.Handler(billing.ListWaffoPancakeSubscriptionProductOptions))
		}
		proxyRoute := apiRouter.Group("/proxy")
		proxyRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(policy.SystemSettings)))
		{
			proxyRoute.GET("/config", ginadapter.Handler(ops.GetProxyConfig))
			proxyRoute.PUT("/config", ginadapter.Handler(ops.UpdateProxyConfig))
			proxyRoute.GET("/config/generate", ginadapter.Handler(ops.GenerateProxyConfig))
			proxyRoute.GET("/status", ginadapter.Handler(ops.GetProxyStatus))
			proxyRoute.POST("/reload", ginadapter.Handler(ops.ReloadProxy))
			proxyRoute.GET("/nodes", ginadapter.Handler(ops.ListProxyNodes))
			proxyRoute.GET("/nodes/report", ginadapter.Handler(ops.GetProxyNodeReport))
			proxyRoute.POST("/nodes", ginadapter.Handler(ops.CreateProxyNode))
			proxyRoute.POST("/nodes/batch", ginadapter.Handler(ops.BatchCreateProxyNodes))
			proxyRoute.POST("/nodes/batch-enabled", ginadapter.Handler(ops.BatchSetProxyNodesEnabled))
			proxyRoute.POST("/nodes/batch-clear-errors", ginadapter.Handler(ops.BatchClearProxyNodeErrors))
			proxyRoute.GET("/nodes/:id", ginadapter.Handler(ops.GetProxyNode))
			proxyRoute.PUT("/nodes/:id", ginadapter.Handler(ops.UpdateProxyNode))
			proxyRoute.DELETE("/nodes/:id", ginadapter.Handler(ops.DeleteProxyNode))
			proxyRoute.POST("/nodes/:id/test", ginadapter.Handler(ops.TestProxyNode))
			proxyRoute.POST("/nodes/test", ginadapter.Handler(ops.TestAllProxyNodes))
		}

		// Custom OAuth provider management (admin with system.settings permission)
		customOAuthRoute := apiRouter.Group("/custom-oauth-provider")
		customOAuthRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(policy.SystemSettings)))
		{
			customOAuthRoute.POST("/discovery", ginadapter.Handler(identity.FetchCustomOAuthDiscovery))
			customOAuthRoute.GET("/", ginadapter.Handler(identity.GetCustomOAuthProviders))
			customOAuthRoute.GET("/:id", ginadapter.Handler(identity.GetCustomOAuthProvider))
			customOAuthRoute.POST("/", ginadapter.Handler(identity.CreateCustomOAuthProvider))
			customOAuthRoute.PUT("/:id", ginadapter.Handler(identity.UpdateCustomOAuthProvider))
			customOAuthRoute.DELETE("/:id", ginadapter.Handler(identity.DeleteCustomOAuthProvider))
		}
		ratioSyncRoute := apiRouter.Group("/ratio_sync")
		ratioSyncRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(policy.SystemSettings)))
		{
			ratioSyncRoute.GET("/channels", ginadapter.Handler(handler.GetSyncableChannels))
			ratioSyncRoute.POST("/fetch", ginadapter.Handler(handler.FetchUpstreamRatios))
		}
		registerChannelRoutes(apiRouter)
		registerAuthzRoutes(apiRouter)
		tokenRoute := apiRouter.Group("/token")
		tokenRoute.Use(ginadapter.Middleware(security.UserAuth()))
		{
			tokenRoute.GET("/", ginadapter.Handler(identity.GetAllTokens))
			tokenRoute.GET("/search", ginadapter.Middleware(middleware.SearchRateLimit()), ginadapter.Handler(identity.SearchTokens))
			tokenRoute.GET("/auto-groups", ginadapter.Handler(identity.GetTokenAutoGroups))
			tokenRoute.GET("/:id", ginadapter.Handler(identity.GetToken))
			tokenRoute.POST("/:id/key", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.GetTokenKey))
			tokenRoute.POST("/", ginadapter.Handler(identity.AddToken))
			tokenRoute.PUT("/", ginadapter.Handler(identity.UpdateToken))
			tokenRoute.DELETE("/:id", ginadapter.Handler(identity.DeleteToken))
			tokenRoute.POST("/batch", ginadapter.Handler(identity.DeleteTokenBatch))
			tokenRoute.POST("/batch/keys", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(identity.GetTokenKeysBatch))
		}

		usageRoute := apiRouter.Group("/usage")
		usageRoute.Use(middleware.CORS(), ginadapter.Middleware(middleware.CriticalRateLimit()))
		{
			tokenUsageRoute := usageRoute.Group("/token")
			tokenUsageRoute.Use(ginadapter.Middleware(security.TokenAuthReadOnly()))
			{
				tokenUsageRoute.GET("/", ginadapter.Handler(identity.GetTokenUsage))
			}
		}

		redemptionRoute := apiRouter.Group("/redemption")
		redemptionRoute.Use(ginadapter.Middleware(security.AdminAuth()))
		{
			redemptionRoute.GET("/", ginadapter.Handler(billing.GetAllRedemptions))
			redemptionRoute.GET("/search", ginadapter.Handler(billing.SearchRedemptions))
			redemptionRoute.GET("/:id", ginadapter.Handler(billing.GetRedemption))
			redemptionRoute.POST("/", ginadapter.Handler(billing.AddRedemption))
			redemptionRoute.PUT("/", ginadapter.Handler(billing.UpdateRedemption))
			redemptionRoute.DELETE("/invalid", ginadapter.Handler(billing.DeleteInvalidRedemption))
			redemptionRoute.DELETE("/:id", ginadapter.Handler(billing.DeleteRedemption))
		}

		systemTaskRoute := apiRouter.Group("/system-task")
		systemTaskRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(policy.SystemSettings)))
		{
			systemTaskRoute.POST("/log-cleanup", ginadapter.Handler(handler.CreateLogCleanupSystemTask))
			systemTaskRoute.GET("/list", ginadapter.Handler(handler.ListSystemTasks))
			systemTaskRoute.GET("/current", ginadapter.Handler(handler.GetCurrentSystemTask))
			systemTaskRoute.GET("/:task_id", ginadapter.Handler(handler.GetSystemTask))
		}
		systemInfoRoute := apiRouter.Group("/system-info")
		systemInfoRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(policy.SystemSettings)))
		{
			systemInfoRoute.GET("/instances", ginadapter.Handler(handler.ListSystemInstances))
			systemInfoRoute.DELETE("/stale-instances", ginadapter.Handler(handler.DeleteStaleSystemInstances))
			systemInfoRoute.DELETE("/instances/:node_name", ginadapter.Handler(handler.DeleteStaleSystemInstance))
		}

		groupRoute := apiRouter.Group("/group")
		groupRoute.Use(ginadapter.Middleware(security.AdminAuth()))
		{
			groupRoute.GET("/", ginadapter.Handler(handler.GetGroups))
		}

		prefillGroupRoute := apiRouter.Group("/prefill_group")
		prefillGroupRoute.Use(ginadapter.Middleware(security.AdminAuth()))
		{
			prefillGroupRoute.GET("/", ginadapter.Handler(handler.GetPrefillGroups))
			prefillGroupRoute.POST("/", ginadapter.Handler(handler.CreatePrefillGroup))
			prefillGroupRoute.PUT("/", ginadapter.Handler(handler.UpdatePrefillGroup))
			prefillGroupRoute.DELETE("/:id", ginadapter.Handler(handler.DeletePrefillGroup))
		}

		mjRoute := apiRouter.Group("/mj")
		mjRoute.GET("/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(handler.GetUserMidjourney))
		mjRoute.GET("/", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(handler.GetAllMidjourney))

		taskRoute := apiRouter.Group("/task")
		{
			taskRoute.GET("/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(handler.GetUserTask))
			taskRoute.GET("/", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(handler.GetAllTask))
		}

		vendorRoute := apiRouter.Group("/vendors")
		vendorRoute.Use(ginadapter.Middleware(security.AdminAuth()))
		{
			vendorRoute.GET("/", ginadapter.Handler(handler.GetAllVendors))
			vendorRoute.GET("/search", ginadapter.Handler(handler.SearchVendors))
			vendorRoute.GET("/:id", ginadapter.Handler(handler.GetVendorMeta))
			vendorRoute.POST("/", ginadapter.Handler(handler.CreateVendorMeta))
			vendorRoute.PUT("/", ginadapter.Handler(handler.UpdateVendorMeta))
			vendorRoute.DELETE("/:id", ginadapter.Handler(handler.DeleteVendorMeta))
		}

		modelsRoute := apiRouter.Group("/models")
		modelsRoute.Use(ginadapter.Middleware(security.AdminAuth()))
		{
			modelsRoute.GET("/sync_upstream/preview", ginadapter.Handler(handler.SyncUpstreamPreview))
			modelsRoute.POST("/sync_upstream", ginadapter.Handler(handler.SyncUpstreamModels))
			modelsRoute.GET("/missing", ginadapter.Handler(handler.GetMissingModels))
			modelsRoute.GET("/", ginadapter.Handler(handler.GetAllModelsMeta))
			modelsRoute.GET("/search", ginadapter.Handler(handler.SearchModelsMeta))
			modelsRoute.GET("/:id", ginadapter.Handler(handler.GetModelMeta))
			modelsRoute.POST("/", ginadapter.Handler(handler.CreateModelMeta))
			modelsRoute.PUT("/", ginadapter.Handler(handler.UpdateModelMeta))
			modelsRoute.DELETE("/:id", ginadapter.Handler(handler.DeleteModelMeta))
		}

	}
}
