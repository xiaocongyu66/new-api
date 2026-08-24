package usage

import (
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/security"
)

func RegisterUsageRoutes(apiRouter *gin.RouterGroup) {
	// /api/log routes
	logRoute := apiRouter.Group("/log")
	logRoute.GET("/", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(GetAllLogs))
	logRoute.GET("/stat", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(GetLogsStat))
	logRoute.GET("/self/stat", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(GetLogsSelfStat))
	logRoute.GET("/channel_affinity_usage_cache", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(GetChannelAffinityUsageCacheStats))
	logRoute.GET("/search", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(SearchAllLogs))
	logRoute.GET("/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(GetUserLogs))
	logRoute.GET("/self/search", ginadapter.Middleware(security.UserAuth()), ginadapter.Middleware(middleware.SearchRateLimit()), ginadapter.Handler(SearchUserLogs))

	logRoute.Use(middleware.CORS(), ginadapter.Middleware(middleware.CriticalRateLimit()))
	{
		logRoute.GET("/token", ginadapter.Middleware(security.TokenAuthReadOnly()), ginadapter.Handler(GetLogByKey))
	}

	// /api/data routes
	dataRoute := apiRouter.Group("/data")
	dataRoute.GET("/", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(GetAllQuotaDates))
	dataRoute.GET("/users", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(GetQuotaDatesByUser))
	dataRoute.GET("/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(GetUserQuotaDates))
	dataRoute.GET("/flow", ginadapter.Middleware(security.AdminAuth()), ginadapter.Handler(GetAllFlowQuotaDates))
	dataRoute.GET("/flow/self", ginadapter.Middleware(security.UserAuth()), ginadapter.Handler(GetUserFlowQuotaDates))

	// /api/rankings route
	apiRouter.GET("/rankings", ginadapter.Middleware(middleware.HeaderNavModuleAuth("rankings")), ginadapter.Handler(GetRankings))

	// /api/perf-metrics routes
	perfMetricsRoute := apiRouter.Group("/perf-metrics")
	perfMetricsRoute.Use(ginadapter.Middleware(middleware.HeaderNavModulePublicOrUserAuth("pricing")))
	{
		perfMetricsRoute.GET("/summary", ginadapter.Handler(GetPerfMetricsSummary))
		perfMetricsRoute.GET("", ginadapter.Handler(GetPerfMetrics))
	}

	// /api/performance routes
	performanceRoute := apiRouter.Group("/performance")
	performanceRoute.Use(ginadapter.Middleware(security.AdminAuth()), ginadapter.Middleware(security.RequirePermission(authz.SystemSettings)))
	{
		performanceRoute.GET("/stats", ginadapter.Handler(GetPerformanceStats))
		performanceRoute.DELETE("/disk_cache", ginadapter.Handler(ClearDiskCache))
		performanceRoute.POST("/reset_stats", ginadapter.Handler(ResetPerformanceStats))
		performanceRoute.POST("/gc", ginadapter.Handler(ForceGC))
		performanceRoute.GET("/logs", ginadapter.Handler(GetLogFiles))
		performanceRoute.DELETE("/logs", ginadapter.Handler(CleanupLogFiles))
	}
}