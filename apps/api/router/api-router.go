package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"

	// Import oauth package to register providers via init()
	_ "github.com/QuantumNous/new-api/internal/security/oauth"

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
		apiRouter.GET("/setup", ginadapter.Handler(controller.GetSetup))
		apiRouter.POST("/setup", anonymousRequestBodyLimit, ginadapter.Handler(controller.PostSetup))
		apiRouter.GET("/status", ginadapter.Handler(controller.GetStatus))
		apiRouter.GET("/uptime/status", ginadapter.Handler(controller.GetUptimeKumaStatus))
		apiRouter.GET("/models", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(controller.DashboardListModels))
		apiRouter.GET("/status/test", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(controller.TestStatus))
		apiRouter.POST("/karmada/session", ginadapter.Middleware(security.RootAuth()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.CreateKarmadaDashboardSession))
		apiRouter.GET("/notice", ginadapter.Handler(controller.GetNotice))
		apiRouter.GET("/user-agreement", ginadapter.Handler(controller.GetUserAgreement))
		apiRouter.GET("/privacy-policy", ginadapter.Handler(controller.GetPrivacyPolicy))
		apiRouter.GET("/about", ginadapter.Handler(controller.GetAbout))
		//apiRouter.GET("/midjourney", ginadapter.Handler(controller.GetMidjourney))
		apiRouter.GET("/home_page_content", ginadapter.Handler(controller.GetHomePageContent))
		apiRouter.GET("/pricing", ginadapter.Middleware(middleware.HeaderNavModuleAuth("pricing")), ginadapter.Handler(controller.GetPricing))
		perfMetricsRoute := apiRouter.Group("/perf-metrics")
		perfMetricsRoute.Use(ginadapter.Middleware(middleware.HeaderNavModulePublicOrUserAuth("pricing")))
		{
			perfMetricsRoute.GET("/summary", ginadapter.Handler(controller.GetPerfMetricsSummary))
			perfMetricsRoute.GET("", ginadapter.Handler(controller.GetPerfMetrics))
		}
		apiRouter.GET("/rankings", ginadapter.Middleware(middleware.HeaderNavModuleAuth("rankings")), ginadapter.Handler(controller.GetRankings))
		apiRouter.GET("/verification", ginadapter.Middleware(middleware.EmailVerificationRateLimit()), ginadapter.Middleware(middleware.TurnstileCheck()), ginadapter.Handler(controller.SendEmailVerification))
		apiRouter.GET("/reset_password", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.TurnstileCheck()), ginadapter.Handler(controller.SendPasswordResetEmail))
		apiRouter.POST("/user/reset", ginadapter.Middleware(middleware.CriticalRateLimit()), anonymousRequestBodyLimit, ginadapter.Handler(controller.ResetPassword))
		// OAuth routes - specific routes must come before :provider wildcard
		apiRouter.POST("/oauth/state", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Middleware(security.TryUserAuth()), anonymousRequestBodyLimit, ginadapter.Handler(controller.GenerateOAuthCode))
		apiRouter.POST("/oauth/email/bind", ginadapter.Middleware(security.UserAuth()), ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.EmailBind))
		// Non-standard OAuth (WeChat, Telegram) - keep original routes
		apiRouter.GET("/oauth/wechat", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.WeChatAuth))
		apiRouter.POST("/oauth/wechat/bind", ginadapter.Middleware(security.UserAuth()), ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.WeChatBind))
		apiRouter.GET("/oauth/telegram/login", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.TelegramLogin))
		apiRouter.POST("/oauth/telegram/bind/start", ginadapter.Middleware(security.UserAuth()), ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.TelegramBindStart))
		apiRouter.GET("/oauth/telegram/bind/:flow_token", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.TelegramBind))
		// Standard OAuth providers (GitHub, Discord, OIDC, LinuxDO) - unified route
		apiRouter.GET("/oauth/:provider", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Middleware(security.TryUserAuth()), ginadapter.Handler(controller.HandleOAuth))
		apiRouter.GET("/ratio_config", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.GetRatioConfig))

		apiRouter.POST("/stripe/webhook", anonymousRequestBodyLimit, ginadapter.Handler(controller.StripeWebhook))
		apiRouter.POST("/creem/webhook", anonymousRequestBodyLimit, ginadapter.Handler(controller.CreemWebhook))
		apiRouter.POST("/waffo/webhook", anonymousRequestBodyLimit, ginadapter.Handler(controller.WaffoWebhook))
		// :env separates test vs prod URLs so the operator can register each
		// in Pancake's matching webhook slot; handler enforces env match.
		apiRouter.POST("/waffo-pancake/webhook/:env", anonymousRequestBodyLimit, ginadapter.Handler(controller.WaffoPancakeWebhook))

		// Universal secure verification routes
		apiRouter.POST("/verify", ginadapter.Middleware(security.UserAuth()), ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.UniversalVerify))

		userRoute := apiRouter.Group("/user")
		{
			userRoute.POST("/auth/refresh", ginadapter.Middleware(security.SessionCookieOriginGuard()), ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.RefreshAuth))
			userRoute.POST("/auth/logout", ginadapter.Middleware(security.SessionCookieOriginGuard()), ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.AuthLogout))
			userRoute.POST("/register", ginadapter.Middleware(middleware.CriticalRateLimit()), anonymousRequestBodyLimit, ginadapter.Middleware(middleware.TurnstileCheck()), ginadapter.Handler(controller.Register))
			userRoute.POST("/login", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), anonymousRequestBodyLimit, ginadapter.Middleware(middleware.TurnstileCheck()), ginadapter.Handler(controller.Login))
			userRoute.POST("/login/2fa", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), anonymousRequestBodyLimit, ginadapter.Handler(controller.Verify2FALogin))
			userRoute.POST("/passkey/login/begin", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), anonymousRequestBodyLimit, ginadapter.Handler(controller.PasskeyLoginBegin))
			userRoute.POST("/passkey/login/finish", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), anonymousRequestBodyLimit, ginadapter.Handler(controller.PasskeyLoginFinish))
			//userRoute.POST("/tokenlog", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.TokenLog))
			userRoute.POST("/epay/notify", anonymousRequestBodyLimit, ginadapter.Handler(controller.EpayNotify))
			userRoute.GET("/epay/notify", ginadapter.Handler(controller.EpayNotify))
			userRoute.GET("/groups", ginadapter.Handler(controller.GetUserGroups))

			selfRoute := userRoute.Group("/")
			selfRoute.Use(ginadapter.Middleware(security.UserAuth()))
			{
				selfRoute.GET("/sessions", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.GetLoginSessions))
				selfRoute.DELETE("/sessions/:sid", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.DeleteLoginSession))
				selfRoute.POST("/sessions/revoke-others", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.RevokeOtherLoginSessions))
				selfRoute.GET("/self/groups", ginadapter.Handler(controller.GetUserGroups))
				selfRoute.GET("/self", ginadapter.Handler(controller.GetSelf))
				selfRoute.GET("/models", ginadapter.Handler(controller.GetUserModels))
				selfRoute.PUT("/self", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.UpdateSelf))
				selfRoute.DELETE("/self", ginadapter.Handler(controller.DeleteSelf))
				selfRoute.GET("/token", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.UserCriticalRateLimit("access-token")), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.GenerateAccessToken))
				selfRoute.GET("/passkey", ginadapter.Handler(controller.PasskeyStatus))
				selfRoute.POST("/passkey/register/begin", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.PasskeyRegisterBegin))
				selfRoute.POST("/passkey/register/finish", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.PasskeyRegisterFinish))
				selfRoute.POST("/passkey/verify/begin", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.PasskeyVerifyBegin))
				selfRoute.POST("/passkey/verify/finish", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.PasskeyVerifyFinish))
				selfRoute.DELETE("/passkey", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.PasskeyDelete))
				selfRoute.GET("/aff", ginadapter.Handler(controller.GetAffCode))
				selfRoute.GET("/topup/info", ginadapter.Handler(controller.GetTopUpInfo))
				selfRoute.GET("/topup/self", ginadapter.Handler(controller.GetUserTopUps))
				selfRoute.POST("/topup", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.TopUp))
				selfRoute.POST("/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.RequestEpay))
				selfRoute.POST("/amount", ginadapter.Handler(controller.RequestAmount))
				selfRoute.POST("/stripe/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.RequestStripePay))
				selfRoute.POST("/stripe/amount", ginadapter.Handler(controller.RequestStripeAmount))
				selfRoute.POST("/creem/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.RequestCreemPay))
				selfRoute.POST("/waffo/amount", ginadapter.Handler(controller.RequestWaffoAmount))
				selfRoute.POST("/waffo/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.RequestWaffoPay))
				selfRoute.POST("/waffo-pancake/amount", ginadapter.Handler(controller.RequestWaffoPancakeAmount))
				selfRoute.POST("/waffo-pancake/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.RequestWaffoPancakePay))
				selfRoute.POST("/aff_transfer", ginadapter.Middleware(middleware.UserCriticalRateLimit("aff-transfer")), ginadapter.Handler(controller.TransferAffQuota))
				selfRoute.PUT("/setting", ginadapter.Handler(controller.UpdateUserSetting))

				// 2FA routes
				selfRoute.GET("/2fa/status", ginadapter.Handler(controller.Get2FAStatus))
				selfRoute.POST("/2fa/setup", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.Setup2FA))
				selfRoute.POST("/2fa/enable", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.Enable2FA))
				selfRoute.POST("/2fa/disable", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.Disable2FA))
				selfRoute.POST("/2fa/backup_codes", ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.RegenerateBackupCodes))

				// Check-in routes
				selfRoute.GET("/checkin", ginadapter.Handler(controller.GetCheckinStatus))
				selfRoute.POST("/checkin", ginadapter.Middleware(middleware.TurnstileCheck()), ginadapter.Handler(controller.DoCheckin))

				// Custom OAuth bindings
				selfRoute.GET("/oauth/bindings", ginadapter.Handler(controller.GetUserOAuthBindings))
				selfRoute.DELETE("/oauth/bindings/:provider_id", ginadapter.Handler(controller.UnbindCustomOAuth))
			}

			adminRoute := userRoute.Group("/")
			adminRoute.Use(ginadapter.Middleware(security.AdminAuth()))
			{
				adminRoute.GET("/", ginadapter.Handler(controller.GetAllUsers))
				adminRoute.GET("/topup", ginadapter.Handler(controller.GetAllTopUps))
				adminRoute.POST("/topup/complete", ginadapter.Handler(controller.AdminCompleteTopUp))
				adminRoute.GET("/search", ginadapter.Handler(controller.SearchUsers))
				adminRoute.GET("/:id/oauth/bindings", ginadapter.Handler(controller.GetUserOAuthBindingsByAdmin))
				adminRoute.DELETE("/:id/oauth/bindings/:provider_id", ginadapter.Handler(controller.UnbindCustomOAuthByAdmin))
				adminRoute.DELETE("/:id/bindings/:binding_type", ginadapter.Handler(controller.AdminClearUserBinding))
				adminRoute.GET("/:id", ginadapter.Handler(controller.GetUser))
				adminRoute.POST("/", ginadapter.Handler(controller.CreateUser))
				adminRoute.POST("/manage", ginadapter.Handler(controller.ManageUser))
				adminRoute.PUT("/", ginadapter.Handler(controller.UpdateUser))
				adminRoute.DELETE("/:id", ginadapter.Handler(controller.DeleteUser))
				adminRoute.DELETE("/:id/reset_passkey", ginadapter.Handler(controller.AdminResetPasskey))

				// Admin 2FA routes
				adminRoute.GET("/2fa/stats", ginadapter.Handler(controller.Admin2FAStats))
				adminRoute.DELETE("/:id/2fa", ginadapter.Handler(controller.AdminDisable2FA))
			}
		}

		// Subscription billing (plans, purchase, admin management)
		subscriptionRoute := apiRouter.Group("/subscription")
		subscriptionRoute.Use(ginadapter.Middleware(security.UserAuth()))
		{
			subscriptionRoute.GET("/plans", ginadapter.Handler(controller.GetSubscriptionPlans))
			subscriptionRoute.GET("/self", ginadapter.Handler(controller.GetSubscriptionSelf))
			subscriptionRoute.PUT("/self/preference", ginadapter.Handler(controller.UpdateSubscriptionPreference))
			subscriptionRoute.POST("/balance/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.SubscriptionRequestBalancePay))
			subscriptionRoute.POST("/epay/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.SubscriptionRequestEpay))
			subscriptionRoute.POST("/stripe/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.SubscriptionRequestStripePay))
			subscriptionRoute.POST("/creem/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.SubscriptionRequestCreemPay))
			subscriptionRoute.POST("/waffo-pancake/pay", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Handler(controller.SubscriptionRequestWaffoPancakePay))
		}
		subscriptionAdminRoute := apiRouter.Group("/subscription/admin")
		subscriptionAdminRoute.Use(ginadapter.Middleware(security.AdminAuth()))
		{
			subscriptionAdminRoute.GET("/plans", ginadapter.Handler(controller.AdminListSubscriptionPlans))
			subscriptionAdminRoute.POST("/plans", ginadapter.Handler(controller.AdminCreateSubscriptionPlan))
			subscriptionAdminRoute.PUT("/plans/:id", ginadapter.Handler(controller.AdminUpdateSubscriptionPlan))
			subscriptionAdminRoute.PATCH("/plans/:id", ginadapter.Handler(controller.AdminUpdateSubscriptionPlanStatus))
			subscriptionAdminRoute.POST("/bind", ginadapter.Handler(controller.AdminBindSubscription))
			subscriptionAdminRoute.POST("/plans/:id/subscriptions/reset", ginadapter.Handler(controller.AdminResetPlanSubscriptions))

			// User subscription management (admin)
			subscriptionAdminRoute.GET("/users/:id/subscriptions", ginadapter.Handler(controller.AdminListUserSubscriptions))
			subscriptionAdminRoute.POST("/users/:id/subscriptions", ginadapter.Handler(controller.AdminCreateUserSubscription))
			subscriptionAdminRoute.POST("/users/:id/subscriptions/reset", ginadapter.Handler(controller.AdminResetUserSubscriptionsByPlan))
			subscriptionAdminRoute.POST("/user_subscriptions/:id/invalidate", ginadapter.Handler(controller.AdminInvalidateUserSubscription))
			subscriptionAdminRoute.DELETE("/user_subscriptions/:id", ginadapter.Handler(controller.AdminDeleteUserSubscription))
		}

		// Subscription payment callbacks (no auth)
		apiRouter.POST("/subscription/epay/notify", anonymousRequestBodyLimit, ginadapter.Handler(controller.SubscriptionEpayNotify))
		apiRouter.GET("/subscription/epay/notify", ginadapter.Handler(controller.SubscriptionEpayNotify))
		apiRouter.GET("/subscription/epay/return", ginadapter.Handler(controller.SubscriptionEpayReturn))
		optionRoute := apiRouter.Group("/option")
		optionRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(authz.SystemSettings)))
		{
			optionRoute.GET("/", ginadapter.Handler(controller.GetOptions))
			optionRoute.PUT("/", ginadapter.Handler(controller.UpdateOption))
			optionRoute.POST("/payment_compliance", ginadapter.Handler(controller.ConfirmPaymentCompliance))
			optionRoute.GET("/channel_affinity_cache", ginadapter.Handler(controller.GetChannelAffinityCacheStats))
			optionRoute.DELETE("/channel_affinity_cache", ginadapter.Handler(controller.ClearChannelAffinityCache))
			optionRoute.POST("/rest_model_ratio", ginadapter.Handler(controller.ResetModelRatio))
			optionRoute.GET("/waffo-pancake/catalog", ginadapter.Handler(controller.ListWaffoPancakeCatalog))
			optionRoute.POST("/waffo-pancake/pair", ginadapter.Handler(controller.CreateWaffoPancakePair))
			optionRoute.POST("/waffo-pancake/save", ginadapter.Handler(controller.SaveWaffoPancake))
			optionRoute.POST("/waffo-pancake/subscription-product", ginadapter.Handler(controller.CreateWaffoPancakeSubscriptionProduct))
			optionRoute.GET("/waffo-pancake/subscription-product-options", ginadapter.Handler(controller.ListWaffoPancakeSubscriptionProductOptions))
		}
		proxyRoute := apiRouter.Group("/proxy")
		proxyRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(authz.SystemSettings)))
		{
			proxyRoute.GET("/config", ginadapter.Handler(controller.GetProxyConfig))
			proxyRoute.PUT("/config", ginadapter.Handler(controller.UpdateProxyConfig))
			proxyRoute.GET("/config/generate", ginadapter.Handler(controller.GenerateProxyConfig))
			proxyRoute.GET("/status", ginadapter.Handler(controller.GetProxyStatus))
			proxyRoute.POST("/reload", ginadapter.Handler(controller.ReloadProxy))
			proxyRoute.GET("/nodes", ginadapter.Handler(controller.ListProxyNodes))
			proxyRoute.GET("/nodes/report", ginadapter.Handler(controller.GetProxyNodeReport))
			proxyRoute.POST("/nodes", ginadapter.Handler(controller.CreateProxyNode))
			proxyRoute.POST("/nodes/batch", ginadapter.Handler(controller.BatchCreateProxyNodes))
			proxyRoute.POST("/nodes/batch-enabled", ginadapter.Handler(controller.BatchSetProxyNodesEnabled))
			proxyRoute.POST("/nodes/batch-clear-errors", ginadapter.Handler(controller.BatchClearProxyNodeErrors))
			proxyRoute.GET("/nodes/:id", ginadapter.Handler(controller.GetProxyNode))
			proxyRoute.PUT("/nodes/:id", ginadapter.Handler(controller.UpdateProxyNode))
			proxyRoute.DELETE("/nodes/:id", ginadapter.Handler(controller.DeleteProxyNode))
			proxyRoute.POST("/nodes/:id/test", ginadapter.Handler(controller.TestProxyNode))
			proxyRoute.POST("/nodes/test", ginadapter.Handler(controller.TestAllProxyNodes))
		}

		// Custom OAuth provider management (admin with system.settings permission)
		customOAuthRoute := apiRouter.Group("/custom-oauth-provider")
		customOAuthRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(authz.SystemSettings)))
		{
			customOAuthRoute.POST("/discovery", ginadapter.Handler(controller.FetchCustomOAuthDiscovery))
			customOAuthRoute.GET("/", ginadapter.Handler(controller.GetCustomOAuthProviders))
			customOAuthRoute.GET("/:id", ginadapter.Handler(controller.GetCustomOAuthProvider))
			customOAuthRoute.POST("/", ginadapter.Handler(controller.CreateCustomOAuthProvider))
			customOAuthRoute.PUT("/:id", ginadapter.Handler(controller.UpdateCustomOAuthProvider))
			customOAuthRoute.DELETE("/:id", ginadapter.Handler(controller.DeleteCustomOAuthProvider))
			performanceRoute := apiRouter.Group("/performance")
			performanceRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(authz.SystemSettings)))
			{
				performanceRoute.GET("/stats", ginadapter.Handler(controller.GetPerformanceStats))
				performanceRoute.DELETE("/disk_cache", ginadapter.Handler(controller.ClearDiskCache))
				performanceRoute.POST("/reset_stats", ginadapter.Handler(controller.ResetPerformanceStats))
				performanceRoute.POST("/gc", ginadapter.Handler(controller.ForceGC))
				performanceRoute.GET("/logs", ginadapter.Handler(controller.GetLogFiles))
				performanceRoute.DELETE("/logs", ginadapter.Handler(controller.CleanupLogFiles))
			}
			ratioSyncRoute := apiRouter.Group("/ratio_sync")
			ratioSyncRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(authz.SystemSettings)))
			ratioSyncRoute.GET("/channels", ginadapter.Handler(controller.GetSyncableChannels))
			ratioSyncRoute.POST("/fetch", ginadapter.Handler(controller.FetchUpstreamRatios))
		}
		registerChannelRoutes(apiRouter)
		registerAuthzRoutes(apiRouter)
		tokenRoute := apiRouter.Group("/token")
		tokenRoute.Use(ginadapter.Middleware(security.UserAuth()))
		{
			tokenRoute.GET("/", ginadapter.Handler(controller.GetAllTokens))
			tokenRoute.GET("/search", ginadapter.Middleware(middleware.SearchRateLimit()), ginadapter.Handler(controller.SearchTokens))
			tokenRoute.GET("/auto-groups", ginadapter.Handler(controller.GetTokenAutoGroups))
			tokenRoute.GET("/:id", ginadapter.Handler(controller.GetToken))
			tokenRoute.POST("/:id/key", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.GetTokenKey))
			tokenRoute.POST("/", ginadapter.Handler(controller.AddToken))
			tokenRoute.PUT("/", ginadapter.Handler(controller.UpdateToken))
			tokenRoute.DELETE("/:id", ginadapter.Handler(controller.DeleteToken))
			tokenRoute.POST("/batch", ginadapter.Handler(controller.DeleteTokenBatch))
			tokenRoute.POST("/batch/keys", ginadapter.Middleware(middleware.CriticalRateLimit()), ginadapter.Middleware(middleware.DisableCache()), ginadapter.Handler(controller.GetTokenKeysBatch))
		}

		usageRoute := apiRouter.Group("/usage")
		usageRoute.Use(middleware.CORS(), ginadapter.Middleware(middleware.CriticalRateLimit()))
		{
			tokenUsageRoute := usageRoute.Group("/token")
			tokenUsageRoute.Use(ginadapter.Middleware(security.TokenAuthReadOnly()))
			{
				tokenUsageRoute.GET("/", ginadapter.Handler(controller.GetTokenUsage))
			}
		}

		redemptionRoute := apiRouter.Group("/redemption")
		redemptionRoute.Use(ginadapter.Middleware(security.AdminAuth()))
		{
			redemptionRoute.GET("/", ginadapter.Handler(controller.GetAllRedemptions))
			redemptionRoute.GET("/search", ginadapter.Handler(controller.SearchRedemptions))
			redemptionRoute.GET("/:id", ginadapter.Handler(controller.GetRedemption))
			redemptionRoute.POST("/", ginadapter.Handler(controller.AddRedemption))
			redemptionRoute.PUT("/", ginadapter.Handler(controller.UpdateRedemption))
			redemptionRoute.DELETE("/invalid", ginadapter.Handler(controller.DeleteInvalidRedemption))
			redemptionRoute.DELETE("/:id", ginadapter.Handler(controller.DeleteRedemption))
		}
		logRoute := apiRouter.Group("/log")
		logRoute.GET("/", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(controller.GetAllLogs))
		logRoute.GET("/stat", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(controller.GetLogsStat))
		logRoute.GET("/self/stat", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(controller.GetLogsSelfStat))
		logRoute.GET("/channel_affinity_usage_cache", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(controller.GetChannelAffinityUsageCacheStats))
		logRoute.GET("/search", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(controller.SearchAllLogs))
		logRoute.GET("/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(controller.GetUserLogs))
		logRoute.GET("/self/search", ginadapter.Middleware(security.UserAuth()), ginadapter.Middleware(middleware.SearchRateLimit()), ginadapter.Handler(controller.SearchUserLogs))

		systemTaskRoute := apiRouter.Group("/system-task")
		systemTaskRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(authz.SystemSettings)))
		{
			systemTaskRoute.POST("/log-cleanup", ginadapter.Handler(controller.CreateLogCleanupSystemTask))
			systemTaskRoute.GET("/list", ginadapter.Handler(controller.ListSystemTasks))
			systemTaskRoute.GET("/current", ginadapter.Handler(controller.GetCurrentSystemTask))
			systemTaskRoute.GET("/:task_id", ginadapter.Handler(controller.GetSystemTask))
		}
		systemInfoRoute := apiRouter.Group("/system-info")
		systemInfoRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(authz.SystemSettings)))
		{
			systemInfoRoute.GET("/instances", ginadapter.Handler(controller.ListSystemInstances))
			systemInfoRoute.DELETE("/stale-instances", ginadapter.Handler(controller.DeleteStaleSystemInstances))
			systemInfoRoute.DELETE("/instances/:node_name", ginadapter.Handler(controller.DeleteStaleSystemInstance))
		}

		dataRoute := apiRouter.Group("/data")
		dataRoute.GET("/", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(controller.GetAllQuotaDates))
		dataRoute.GET("/users", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(controller.GetQuotaDatesByUser))
		dataRoute.GET("/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(controller.GetUserQuotaDates))
		dataRoute.GET("/flow", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(controller.GetAllFlowQuotaDates))
		dataRoute.GET("/flow/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(controller.GetUserFlowQuotaDates))

		logRoute.Use(middleware.CORS(), ginadapter.Middleware(middleware.CriticalRateLimit()))
		{
			logRoute.GET("/token", ginadapter.Middleware(security.TokenAuthReadOnly()), ginadapter.Handler(controller.GetLogByKey))
		}
		groupRoute := apiRouter.Group("/group")
		groupRoute.Use(ginadapter.Middleware(security.AdminAuth()))
		{
			groupRoute.GET("/", ginadapter.Handler(controller.GetGroups))
		}

		prefillGroupRoute := apiRouter.Group("/prefill_group")
		prefillGroupRoute.Use(ginadapter.Middleware(security.AdminAuth()))
		{
			prefillGroupRoute.GET("/", ginadapter.Handler(controller.GetPrefillGroups))
			prefillGroupRoute.POST("/", ginadapter.Handler(controller.CreatePrefillGroup))
			prefillGroupRoute.PUT("/", ginadapter.Handler(controller.UpdatePrefillGroup))
			prefillGroupRoute.DELETE("/:id", ginadapter.Handler(controller.DeletePrefillGroup))
		}

		mjRoute := apiRouter.Group("/mj")
		mjRoute.GET("/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(controller.GetUserMidjourney))
		mjRoute.GET("/", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(controller.GetAllMidjourney))

		taskRoute := apiRouter.Group("/task")
		{
			taskRoute.GET("/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(controller.GetUserTask))
			taskRoute.GET("/", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(controller.GetAllTask))
		}

		vendorRoute := apiRouter.Group("/vendors")
		vendorRoute.Use(ginadapter.Middleware(security.AdminAuth()))
		{
			vendorRoute.GET("/", ginadapter.Handler(controller.GetAllVendors))
			vendorRoute.GET("/search", ginadapter.Handler(controller.SearchVendors))
			vendorRoute.GET("/:id", ginadapter.Handler(controller.GetVendorMeta))
			vendorRoute.POST("/", ginadapter.Handler(controller.CreateVendorMeta))
			vendorRoute.PUT("/", ginadapter.Handler(controller.UpdateVendorMeta))
			vendorRoute.DELETE("/:id", ginadapter.Handler(controller.DeleteVendorMeta))
		}

		modelsRoute := apiRouter.Group("/models")
		modelsRoute.Use(ginadapter.Middleware(security.AdminAuth()))
		{
			modelsRoute.GET("/sync_upstream/preview", ginadapter.Handler(controller.SyncUpstreamPreview))
			modelsRoute.POST("/sync_upstream", ginadapter.Handler(controller.SyncUpstreamModels))
			modelsRoute.GET("/missing", ginadapter.Handler(controller.GetMissingModels))
			modelsRoute.GET("/", ginadapter.Handler(controller.GetAllModelsMeta))
			modelsRoute.GET("/search", ginadapter.Handler(controller.SearchModelsMeta))
			modelsRoute.GET("/:id", ginadapter.Handler(controller.GetModelMeta))
			modelsRoute.POST("/", ginadapter.Handler(controller.CreateModelMeta))
			modelsRoute.PUT("/", ginadapter.Handler(controller.UpdateModelMeta))
			modelsRoute.DELETE("/:id", ginadapter.Handler(controller.DeleteModelMeta))
		}

	}
}
