package router

import (
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"

	// Import oauth package to register providers via init(
	catalogcontroller "github.com/QuantumNous/new-api/internal/catalog/controller"
	billingcontroller "github.com/QuantumNous/new-api/internal/billing/controller"
	identitycontroller "github.com/QuantumNous/new-api/internal/identity/controller"
	opscontroller "github.com/QuantumNous/new-api/internal/ops/controller"
	taskcontroller "github.com/QuantumNous/new-api/internal/task/controller"
	usagecontroller "github.com/QuantumNous/new-api/internal/usage/controller"
	misccontroller "github.com/QuantumNous/new-api/internal/misc/controller"

	_ "github.com/QuantumNous/new-api/oauth"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

func SetApiRouter(router *gin.Engine) {
	apiRouter := router.Group("/api")
	apiRouter.Use(middleware.RouteTag("api"))
	apiRouter.Use(gzip.Gzip(gzip.DefaultCompression))
	apiRouter.Use(middleware.BodyStorageCleanup()) // 清理请求体存储
	apiRouter.Use(middleware.GlobalAPIRateLimit())
	anonymousRequestBodyLimit := middleware.AnonymousRequestBodyLimit()
	{
		apiRouter.GET("/setup", identitycontroller.GetSetup)
		apiRouter.POST("/setup", anonymousRequestBodyLimit, identitycontroller.PostSetup)
		apiRouter.GET("/status", misccontroller.GetStatus)
		apiRouter.GET("/uptime/status", opscontroller.GetUptimeKumaStatus)
		apiRouter.GET("/models", middleware.UserAuth(), catalogcontroller.DashboardListModels)
		apiRouter.GET("/status/test", middleware.AdminAuth(), misccontroller.TestStatus)
		apiRouter.POST("/karmada/session", middleware.RootAuth(), middleware.DisableCache(), opscontroller.CreateKarmadaDashboardSession)
		apiRouter.GET("/notice", misccontroller.GetNotice)
		apiRouter.GET("/user-agreement", misccontroller.GetUserAgreement)
		apiRouter.GET("/privacy-policy", misccontroller.GetPrivacyPolicy)
		apiRouter.GET("/about", misccontroller.GetAbout)
		//apiRouter.GET("/midjourney", misccontroller.GetMidjourney)
		apiRouter.GET("/home_page_content", misccontroller.GetHomePageContent)
		apiRouter.GET("/pricing", middleware.HeaderNavModuleAuth("pricing"), catalogcontroller.GetPricing)
		perfMetricsRoute := apiRouter.Group("/perf-metrics")
		perfMetricsRoute.Use(middleware.HeaderNavModulePublicOrUserAuth("pricing"))
		{
			perfMetricsRoute.GET("/summary", usagecontroller.GetPerfMetricsSummary)
			perfMetricsRoute.GET("", usagecontroller.GetPerfMetrics)
		}
		apiRouter.GET("/rankings", middleware.HeaderNavModuleAuth("rankings"), usagecontroller.GetRankings)
		apiRouter.GET("/verification", middleware.EmailVerificationRateLimit(), middleware.TurnstileCheck(), misccontroller.SendEmailVerification)
		apiRouter.GET("/reset_password", middleware.CriticalRateLimit(), middleware.TurnstileCheck(), misccontroller.SendPasswordResetEmail)
		apiRouter.POST("/user/reset", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, misccontroller.ResetPassword)
		// OAuth routes - specific routes must come before :provider wildcard
		apiRouter.POST("/oauth/state", middleware.CriticalRateLimit(), middleware.DisableCache(), middleware.TryUserAuth(), anonymousRequestBodyLimit, identitycontroller.GenerateOAuthCode)
		apiRouter.POST("/oauth/email/bind", middleware.UserAuth(), middleware.CriticalRateLimit(), identitycontroller.EmailBind)
		// Non-standard OAuth (WeChat, Telegram) - keep original routes
		apiRouter.GET("/oauth/wechat", middleware.CriticalRateLimit(), middleware.DisableCache(), identitycontroller.WeChatAuth)
		apiRouter.POST("/oauth/wechat/bind", middleware.UserAuth(), middleware.CriticalRateLimit(), identitycontroller.WeChatBind)
		apiRouter.GET("/oauth/telegram/login", middleware.CriticalRateLimit(), middleware.DisableCache(), identitycontroller.TelegramLogin)
		apiRouter.POST("/oauth/telegram/bind/start", middleware.UserAuth(), middleware.CriticalRateLimit(), middleware.DisableCache(), identitycontroller.TelegramBindStart)
		apiRouter.GET("/oauth/telegram/bind/:flow_token", middleware.CriticalRateLimit(), middleware.DisableCache(), identitycontroller.TelegramBind)
		// Standard OAuth providers (GitHub, Discord, OIDC, LinuxDO) - unified route
		apiRouter.GET("/oauth/:provider", middleware.CriticalRateLimit(), middleware.DisableCache(), middleware.TryUserAuth(), identitycontroller.HandleOAuth)
		apiRouter.GET("/ratio_config", middleware.CriticalRateLimit(), catalogcontroller.GetRatioConfig)

		apiRouter.POST("/stripe/webhook", anonymousRequestBodyLimit, billingcontroller.StripeWebhook)
		apiRouter.POST("/creem/webhook", anonymousRequestBodyLimit, billingcontroller.CreemWebhook)
		apiRouter.POST("/waffo/webhook", anonymousRequestBodyLimit, billingcontroller.WaffoWebhook)
		// :env separates test vs prod URLs so the operator can register each
		// in Pancake's matching webhook slot; handler enforces env match.
		apiRouter.POST("/waffo-pancake/webhook/:env", anonymousRequestBodyLimit, billingcontroller.WaffoPancakeWebhook)

		// Universal secure verification routes
		apiRouter.POST("/verify", middleware.UserAuth(), middleware.CriticalRateLimit(), middleware.DisableCache(), identitycontroller.UniversalVerify)

		userRoute := apiRouter.Group("/user")
		{
			userRoute.POST("/auth/refresh", middleware.SessionCookieOriginGuard(), middleware.CriticalRateLimit(), middleware.DisableCache(), identitycontroller.RefreshAuth)
			userRoute.POST("/auth/logout", middleware.SessionCookieOriginGuard(), middleware.CriticalRateLimit(), middleware.DisableCache(), identitycontroller.AuthLogout)
			userRoute.POST("/register", middleware.CriticalRateLimit(), anonymousRequestBodyLimit, middleware.TurnstileCheck(), identitycontroller.Register)
			userRoute.POST("/login", middleware.CriticalRateLimit(), middleware.DisableCache(), anonymousRequestBodyLimit, middleware.TurnstileCheck(), identitycontroller.Login)
			userRoute.POST("/login/2fa", middleware.CriticalRateLimit(), middleware.DisableCache(), anonymousRequestBodyLimit, identitycontroller.Verify2FALogin)
			userRoute.POST("/passkey/login/begin", middleware.CriticalRateLimit(), middleware.DisableCache(), anonymousRequestBodyLimit, identitycontroller.PasskeyLoginBegin)
			userRoute.POST("/passkey/login/finish", middleware.CriticalRateLimit(), middleware.DisableCache(), anonymousRequestBodyLimit, identitycontroller.PasskeyLoginFinish)
			//userRoute.POST("/tokenlog", middleware.CriticalRateLimit(), topcontroller.TokenLog)
			userRoute.POST("/epay/notify", anonymousRequestBodyLimit, billingcontroller.EpayNotify)
			userRoute.GET("/epay/notify", billingcontroller.EpayNotify)
			userRoute.GET("/groups", catalogcontroller.GetUserGroups)

			selfRoute := userRoute.Group("/")
			selfRoute.Use(middleware.UserAuth())
			{
				selfRoute.GET("/sessions", middleware.DisableCache(), identitycontroller.GetLoginSessions)
				selfRoute.DELETE("/sessions/:sid", middleware.DisableCache(), identitycontroller.DeleteLoginSession)
				selfRoute.POST("/sessions/revoke-others", middleware.DisableCache(), identitycontroller.RevokeOtherLoginSessions)
				selfRoute.GET("/self/groups", catalogcontroller.GetUserGroups)
				selfRoute.GET("/self", identitycontroller.GetSelf)
				selfRoute.GET("/models", identitycontroller.GetUserModels)
				selfRoute.PUT("/self", middleware.CriticalRateLimit(), middleware.DisableCache(), identitycontroller.UpdateSelf)
				selfRoute.DELETE("/self", identitycontroller.DeleteSelf)
				selfRoute.GET("/token", middleware.CriticalRateLimit(), middleware.UserCriticalRateLimit("access-token"), middleware.DisableCache(), identitycontroller.GenerateAccessToken)
				selfRoute.GET("/passkey", identitycontroller.PasskeyStatus)
				selfRoute.POST("/passkey/register/begin", middleware.DisableCache(), identitycontroller.PasskeyRegisterBegin)
				selfRoute.POST("/passkey/register/finish", middleware.DisableCache(), identitycontroller.PasskeyRegisterFinish)
				selfRoute.POST("/passkey/verify/begin", middleware.DisableCache(), identitycontroller.PasskeyVerifyBegin)
				selfRoute.POST("/passkey/verify/finish", middleware.DisableCache(), identitycontroller.PasskeyVerifyFinish)
				selfRoute.DELETE("/passkey", middleware.DisableCache(), identitycontroller.PasskeyDelete)
				selfRoute.GET("/aff", identitycontroller.GetAffCode)
				selfRoute.GET("/topup/info", billingcontroller.GetTopUpInfo)
				selfRoute.GET("/topup/self", billingcontroller.GetUserTopUps)
				selfRoute.POST("/topup", middleware.CriticalRateLimit(), identitycontroller.TopUp)
				selfRoute.POST("/pay", middleware.CriticalRateLimit(), billingcontroller.RequestEpay)
				selfRoute.POST("/amount", billingcontroller.RequestAmount)
				selfRoute.POST("/stripe/pay", middleware.CriticalRateLimit(), billingcontroller.RequestStripePay)
				selfRoute.POST("/stripe/amount", billingcontroller.RequestStripeAmount)
				selfRoute.POST("/creem/pay", middleware.CriticalRateLimit(), billingcontroller.RequestCreemPay)
				selfRoute.POST("/waffo/amount", billingcontroller.RequestWaffoAmount)
				selfRoute.POST("/waffo/pay", middleware.CriticalRateLimit(), billingcontroller.RequestWaffoPay)
				selfRoute.POST("/waffo-pancake/amount", billingcontroller.RequestWaffoPancakeAmount)
				selfRoute.POST("/waffo-pancake/pay", middleware.CriticalRateLimit(), billingcontroller.RequestWaffoPancakePay)
				selfRoute.POST("/aff_transfer", middleware.UserCriticalRateLimit("aff-transfer"), identitycontroller.TransferAffQuota)
				selfRoute.PUT("/setting", identitycontroller.UpdateUserSetting)

				// 2FA routes
				selfRoute.GET("/2fa/status", identitycontroller.Get2FAStatus)
				selfRoute.POST("/2fa/setup", middleware.DisableCache(), identitycontroller.Setup2FA)
				selfRoute.POST("/2fa/enable", middleware.DisableCache(), identitycontroller.Enable2FA)
				selfRoute.POST("/2fa/disable", middleware.DisableCache(), identitycontroller.Disable2FA)
				selfRoute.POST("/2fa/backup_codes", middleware.DisableCache(), identitycontroller.RegenerateBackupCodes)

				// Check-in routes
				selfRoute.GET("/checkin", billingcontroller.GetCheckinStatus)
				selfRoute.POST("/checkin", middleware.TurnstileCheck(), billingcontroller.DoCheckin)

				// Custom OAuth bindings
				selfRoute.GET("/oauth/bindings", identitycontroller.GetUserOAuthBindings)
				selfRoute.DELETE("/oauth/bindings/:provider_id", identitycontroller.UnbindCustomOAuth)
			}

			adminRoute := userRoute.Group("/")
			adminRoute.Use(middleware.AdminAuth())
			{
				adminRoute.GET("/", identitycontroller.GetAllUsers)
				adminRoute.GET("/topup", billingcontroller.GetAllTopUps)
				adminRoute.POST("/topup/complete", billingcontroller.AdminCompleteTopUp)
				adminRoute.GET("/search", identitycontroller.SearchUsers)
				adminRoute.GET("/:id/oauth/bindings", identitycontroller.GetUserOAuthBindingsByAdmin)
				adminRoute.DELETE("/:id/oauth/bindings/:provider_id", identitycontroller.UnbindCustomOAuthByAdmin)
				adminRoute.DELETE("/:id/bindings/:binding_type", identitycontroller.AdminClearUserBinding)
				adminRoute.GET("/:id", identitycontroller.GetUser)
				adminRoute.POST("/", identitycontroller.CreateUser)
				adminRoute.POST("/manage", identitycontroller.ManageUser)
				adminRoute.PUT("/", identitycontroller.UpdateUser)
				adminRoute.DELETE("/:id", identitycontroller.DeleteUser)
				adminRoute.DELETE("/:id/reset_passkey", identitycontroller.AdminResetPasskey)

				// Admin 2FA routes
				adminRoute.GET("/2fa/stats", identitycontroller.Admin2FAStats)
				adminRoute.DELETE("/:id/2fa", identitycontroller.AdminDisable2FA)
			}
		}

		// Subscription billing (plans, purchase, admin management)
		subscriptionRoute := apiRouter.Group("/subscription")
		subscriptionRoute.Use(middleware.UserAuth())
		{
			subscriptionRoute.GET("/plans", billingcontroller.GetSubscriptionPlans)
			subscriptionRoute.GET("/self", billingcontroller.GetSubscriptionSelf)
			subscriptionRoute.PUT("/self/preference", billingcontroller.UpdateSubscriptionPreference)
			subscriptionRoute.POST("/balance/pay", middleware.CriticalRateLimit(), billingcontroller.SubscriptionRequestBalancePay)
			subscriptionRoute.POST("/epay/pay", middleware.CriticalRateLimit(), billingcontroller.SubscriptionRequestEpay)
			subscriptionRoute.POST("/stripe/pay", middleware.CriticalRateLimit(), billingcontroller.SubscriptionRequestStripePay)
			subscriptionRoute.POST("/creem/pay", middleware.CriticalRateLimit(), billingcontroller.SubscriptionRequestCreemPay)
			subscriptionRoute.POST("/waffo-pancake/pay", middleware.CriticalRateLimit(), billingcontroller.SubscriptionRequestWaffoPancakePay)
		}
		subscriptionAdminRoute := apiRouter.Group("/subscription/admin")
		subscriptionAdminRoute.Use(middleware.AdminAuth())
		{
			subscriptionAdminRoute.GET("/plans", billingcontroller.AdminListSubscriptionPlans)
			subscriptionAdminRoute.POST("/plans", billingcontroller.AdminCreateSubscriptionPlan)
			subscriptionAdminRoute.PUT("/plans/:id", billingcontroller.AdminUpdateSubscriptionPlan)
			subscriptionAdminRoute.PATCH("/plans/:id", billingcontroller.AdminUpdateSubscriptionPlanStatus)
			subscriptionAdminRoute.POST("/bind", billingcontroller.AdminBindSubscription)
			subscriptionAdminRoute.POST("/plans/:id/subscriptions/reset", billingcontroller.AdminResetPlanSubscriptions)

			// User subscription management (admin)
			subscriptionAdminRoute.GET("/users/:id/subscriptions", billingcontroller.AdminListUserSubscriptions)
			subscriptionAdminRoute.POST("/users/:id/subscriptions", billingcontroller.AdminCreateUserSubscription)
			subscriptionAdminRoute.POST("/users/:id/subscriptions/reset", billingcontroller.AdminResetUserSubscriptionsByPlan)
			subscriptionAdminRoute.POST("/user_subscriptions/:id/invalidate", billingcontroller.AdminInvalidateUserSubscription)
			subscriptionAdminRoute.DELETE("/user_subscriptions/:id", billingcontroller.AdminDeleteUserSubscription)
		}

		// Subscription payment callbacks (no auth)
		apiRouter.POST("/subscription/epay/notify", anonymousRequestBodyLimit, billingcontroller.SubscriptionEpayNotify)
		apiRouter.GET("/subscription/epay/notify", billingcontroller.SubscriptionEpayNotify)
		apiRouter.GET("/subscription/epay/return", billingcontroller.SubscriptionEpayReturn)
		optionRoute := apiRouter.Group("/option")
		optionRoute.Use(middleware.AdminAuth(), middleware.RequirePermission(authz.SystemSettings))
		{
			optionRoute.GET("/", catalogcontroller.GetOptions)
			optionRoute.PUT("/", catalogcontroller.UpdateOption)
			optionRoute.POST("/payment_compliance", billingcontroller.ConfirmPaymentCompliance)
			optionRoute.GET("/channel_affinity_cache", catalogcontroller.GetChannelAffinityCacheStats)
			optionRoute.DELETE("/channel_affinity_cache", catalogcontroller.ClearChannelAffinityCache)
			optionRoute.POST("/rest_model_ratio", catalogcontroller.ResetModelRatio)
			optionRoute.GET("/waffo-pancake/catalog", billingcontroller.ListWaffoPancakeCatalog)
			optionRoute.POST("/waffo-pancake/pair", billingcontroller.CreateWaffoPancakePair)
			optionRoute.POST("/waffo-pancake/save", billingcontroller.SaveWaffoPancake)
			optionRoute.POST("/waffo-pancake/subscription-product", billingcontroller.CreateWaffoPancakeSubscriptionProduct)
			optionRoute.GET("/waffo-pancake/subscription-product-options", billingcontroller.ListWaffoPancakeSubscriptionProductOptions)
		}
		proxyRoute := apiRouter.Group("/proxy")
		proxyRoute.Use(middleware.AdminAuth(), middleware.RequirePermission(authz.SystemSettings))
		{
			proxyRoute.GET("/config", catalogcontroller.GetProxyConfig)
			proxyRoute.PUT("/config", catalogcontroller.UpdateProxyConfig)
			proxyRoute.GET("/config/generate", catalogcontroller.GenerateProxyConfig)
			proxyRoute.GET("/status", catalogcontroller.GetProxyStatus)
			proxyRoute.POST("/reload", catalogcontroller.ReloadProxy)
			proxyRoute.GET("/nodes", catalogcontroller.ListProxyNodes)
			proxyRoute.GET("/nodes/report", catalogcontroller.GetProxyNodeReport)
			proxyRoute.POST("/nodes", catalogcontroller.CreateProxyNode)
			proxyRoute.POST("/nodes/batch", catalogcontroller.BatchCreateProxyNodes)
			proxyRoute.POST("/nodes/batch-enabled", catalogcontroller.BatchSetProxyNodesEnabled)
			proxyRoute.POST("/nodes/batch-clear-errors", catalogcontroller.BatchClearProxyNodeErrors)
			proxyRoute.GET("/nodes/:id", catalogcontroller.GetProxyNode)
			proxyRoute.PUT("/nodes/:id", catalogcontroller.UpdateProxyNode)
			proxyRoute.DELETE("/nodes/:id", catalogcontroller.DeleteProxyNode)
			proxyRoute.POST("/nodes/:id/test", catalogcontroller.TestProxyNode)
			proxyRoute.POST("/nodes/test", catalogcontroller.TestAllProxyNodes)
		}

		// Custom OAuth provider management (admin with system.settings permission)
		customOAuthRoute := apiRouter.Group("/custom-oauth-provider")
		customOAuthRoute.Use(middleware.AdminAuth(), middleware.RequirePermission(authz.SystemSettings))
		{
			customOAuthRoute.POST("/discovery", identitycontroller.FetchCustomOAuthDiscovery)
			customOAuthRoute.GET("/", identitycontroller.GetCustomOAuthProviders)
			customOAuthRoute.GET("/:id", identitycontroller.GetCustomOAuthProvider)
			customOAuthRoute.POST("/", identitycontroller.CreateCustomOAuthProvider)
			customOAuthRoute.PUT("/:id", identitycontroller.UpdateCustomOAuthProvider)
			customOAuthRoute.DELETE("/:id", identitycontroller.DeleteCustomOAuthProvider)
			performanceRoute := apiRouter.Group("/performance")
			performanceRoute.Use(middleware.AdminAuth(), middleware.RequirePermission(authz.SystemSettings))
			{
				performanceRoute.GET("/stats", usagecontroller.GetPerformanceStats)
				performanceRoute.DELETE("/disk_cache", usagecontroller.ClearDiskCache)
				performanceRoute.POST("/reset_stats", usagecontroller.ResetPerformanceStats)
				performanceRoute.POST("/gc", usagecontroller.ForceGC)
				performanceRoute.GET("/logs", usagecontroller.GetLogFiles)
				performanceRoute.DELETE("/logs", usagecontroller.CleanupLogFiles)
			}
			ratioSyncRoute := apiRouter.Group("/ratio_sync")
			ratioSyncRoute.Use(middleware.AdminAuth(), middleware.RequirePermission(authz.SystemSettings))
			ratioSyncRoute.GET("/channels", catalogcontroller.GetSyncableChannels)
			ratioSyncRoute.POST("/fetch", catalogcontroller.FetchUpstreamRatios)
		}
		registerChannelRoutes(apiRouter)
		registerAuthzRoutes(apiRouter)
		tokenRoute := apiRouter.Group("/token")
		tokenRoute.Use(middleware.UserAuth())
		{
			tokenRoute.GET("/", identitycontroller.GetAllTokens)
			tokenRoute.GET("/search", middleware.SearchRateLimit(), identitycontroller.SearchTokens)
			tokenRoute.GET("/auto-groups", identitycontroller.GetTokenAutoGroups)
			tokenRoute.GET("/:id", identitycontroller.GetToken)
			tokenRoute.POST("/:id/key", middleware.CriticalRateLimit(), middleware.DisableCache(), identitycontroller.GetTokenKey)
			tokenRoute.POST("/", identitycontroller.AddToken)
			tokenRoute.PUT("/", identitycontroller.UpdateToken)
			tokenRoute.DELETE("/:id", identitycontroller.DeleteToken)
			tokenRoute.POST("/batch", identitycontroller.DeleteTokenBatch)
			tokenRoute.POST("/batch/keys", middleware.CriticalRateLimit(), middleware.DisableCache(), identitycontroller.GetTokenKeysBatch)
		}

		usageRoute := apiRouter.Group("/usage")
		usageRoute.Use(middleware.CORS(), middleware.CriticalRateLimit())
		{
			tokenUsageRoute := usageRoute.Group("/token")
			tokenUsageRoute.Use(middleware.TokenAuthReadOnly())
			{
				tokenUsageRoute.GET("/", identitycontroller.GetTokenUsage)
			}
		}

		redemptionRoute := apiRouter.Group("/redemption")
		redemptionRoute.Use(middleware.AdminAuth())
		{
			redemptionRoute.GET("/", billingcontroller.GetAllRedemptions)
			redemptionRoute.GET("/search", billingcontroller.SearchRedemptions)
			redemptionRoute.GET("/:id", billingcontroller.GetRedemption)
			redemptionRoute.POST("/", billingcontroller.AddRedemption)
			redemptionRoute.PUT("/", billingcontroller.UpdateRedemption)
			redemptionRoute.DELETE("/invalid", billingcontroller.DeleteInvalidRedemption)
			redemptionRoute.DELETE("/:id", billingcontroller.DeleteRedemption)
		}
		logRoute := apiRouter.Group("/log")
		logRoute.GET("/", middleware.AdminAuth(), usagecontroller.GetAllLogs)
		logRoute.GET("/stat", middleware.AdminAuth(), usagecontroller.GetLogsStat)
		logRoute.GET("/self/stat", middleware.UserAuth(), usagecontroller.GetLogsSelfStat)
		logRoute.GET("/channel_affinity_usage_cache", middleware.AdminAuth(), catalogcontroller.GetChannelAffinityUsageCacheStats)
		logRoute.GET("/search", middleware.AdminAuth(), usagecontroller.SearchAllLogs)
		logRoute.GET("/self", middleware.UserAuth(), usagecontroller.GetUserLogs)
		logRoute.GET("/self/search", middleware.UserAuth(), middleware.SearchRateLimit(), usagecontroller.SearchUserLogs)

		systemTaskRoute := apiRouter.Group("/system-task")
		systemTaskRoute.Use(middleware.AdminAuth(), middleware.RequirePermission(authz.SystemSettings))
		{
			systemTaskRoute.POST("/log-cleanup", opscontroller.CreateLogCleanupSystemTask)
			systemTaskRoute.GET("/list", opscontroller.ListSystemTasks)
			systemTaskRoute.GET("/current", opscontroller.GetCurrentSystemTask)
			systemTaskRoute.GET("/:task_id", opscontroller.GetSystemTask)
		}
		systemInfoRoute := apiRouter.Group("/system-info")
		systemInfoRoute.Use(middleware.AdminAuth(), middleware.RequirePermission(authz.SystemSettings))
		{
			systemInfoRoute.GET("/instances", opscontroller.ListSystemInstances)
			systemInfoRoute.DELETE("/stale-instances", opscontroller.DeleteStaleSystemInstances)
			systemInfoRoute.DELETE("/instances/:node_name", opscontroller.DeleteStaleSystemInstance)
		}

		dataRoute := apiRouter.Group("/data")
		dataRoute.GET("/", middleware.AdminAuth(), usagecontroller.GetAllQuotaDates)
		dataRoute.GET("/users", middleware.AdminAuth(), usagecontroller.GetQuotaDatesByUser)
		dataRoute.GET("/self", middleware.UserAuth(), usagecontroller.GetUserQuotaDates)
		dataRoute.GET("/flow", middleware.AdminAuth(), usagecontroller.GetAllFlowQuotaDates)
		dataRoute.GET("/flow/self", middleware.UserAuth(), usagecontroller.GetUserFlowQuotaDates)

		logRoute.Use(middleware.CORS(), middleware.CriticalRateLimit())
		{
			logRoute.GET("/token", middleware.TokenAuthReadOnly(), usagecontroller.GetLogByKey)
		}
		groupRoute := apiRouter.Group("/group")
		groupRoute.Use(middleware.AdminAuth())
		{
			groupRoute.GET("/", catalogcontroller.GetGroups)
		}

		prefillGroupRoute := apiRouter.Group("/prefill_group")
		prefillGroupRoute.Use(middleware.AdminAuth())
		{
			prefillGroupRoute.GET("/", catalogcontroller.GetPrefillGroups)
			prefillGroupRoute.POST("/", catalogcontroller.CreatePrefillGroup)
			prefillGroupRoute.PUT("/", catalogcontroller.UpdatePrefillGroup)
			prefillGroupRoute.DELETE("/:id", catalogcontroller.DeletePrefillGroup)
		}

		mjRoute := apiRouter.Group("/mj")
		mjRoute.GET("/self", middleware.UserAuth(), taskcontroller.GetUserMidjourney)
		mjRoute.GET("/", middleware.AdminAuth(), taskcontroller.GetAllMidjourney)

		taskRoute := apiRouter.Group("/task")
		{
			taskRoute.GET("/self", middleware.UserAuth(), taskcontroller.GetUserTask)
			taskRoute.GET("/", middleware.AdminAuth(), taskcontroller.GetAllTask)
		}

		vendorRoute := apiRouter.Group("/vendors")
		vendorRoute.Use(middleware.AdminAuth())
		{
			vendorRoute.GET("/", catalogcontroller.GetAllVendors)
			vendorRoute.GET("/search", catalogcontroller.SearchVendors)
			vendorRoute.GET("/:id", catalogcontroller.GetVendorMeta)
			vendorRoute.POST("/", catalogcontroller.CreateVendorMeta)
			vendorRoute.PUT("/", catalogcontroller.UpdateVendorMeta)
			vendorRoute.DELETE("/:id", catalogcontroller.DeleteVendorMeta)
		}

		modelsRoute := apiRouter.Group("/models")
		modelsRoute.Use(middleware.AdminAuth())
		{
			modelsRoute.GET("/sync_upstream/preview", catalogcontroller.SyncUpstreamPreview)
			modelsRoute.POST("/sync_upstream", catalogcontroller.SyncUpstreamModels)
			modelsRoute.GET("/missing", catalogcontroller.GetMissingModels)
			modelsRoute.GET("/", catalogcontroller.GetAllModelsMeta)
			modelsRoute.GET("/search", catalogcontroller.SearchModelsMeta)
			modelsRoute.GET("/:id", catalogcontroller.GetModelMeta)
			modelsRoute.POST("/", catalogcontroller.CreateModelMeta)
			modelsRoute.PUT("/", catalogcontroller.UpdateModelMeta)
			modelsRoute.DELETE("/:id", catalogcontroller.DeleteModelMeta)
		}

	}
}
