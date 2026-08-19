package router

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	catalogcontroller "github.com/QuantumNous/new-api/internal/catalog/controller"
	topcontroller "github.com/QuantumNous/new-api/controller"
)

func SetRelayRouter(router *gin.Engine) {
	router.Use(middleware.CORS())
	router.Use(middleware.DecompressRequestMiddleware())
	router.Use(middleware.BodyStorageCleanup()) // 清理请求体存储
	router.Use(middleware.StatsMiddleware())
	// https://platform.openai.com/docs/api-reference/introduction
	modelsRouter := router.Group("/v1/models")
	modelsRouter.Use(middleware.RouteTag("relay"))
	modelsRouter.Use(middleware.TokenAuth())
	{
		modelsRouter.GET("", func(c *gin.Context) {
			switch {
			case c.GetHeader("x-api-key") != "" && c.GetHeader("anthropic-version") != "":
				catalogcontroller.ListModels(c, constant.ChannelTypeAnthropic)
			case c.GetHeader("x-goog-api-key") != "" || c.Query("key") != "": // 单独的适配
				catalogcontroller.RetrieveModel(c, constant.ChannelTypeGemini)
			default:
				catalogcontroller.ListModels(c, constant.ChannelTypeOpenAI)
			}
		})

		modelsRouter.GET("/:model", func(c *gin.Context) {
			switch {
			case c.GetHeader("x-api-key") != "" && c.GetHeader("anthropic-version") != "":
				catalogcontroller.RetrieveModel(c, constant.ChannelTypeAnthropic)
			default:
				catalogcontroller.RetrieveModel(c, constant.ChannelTypeOpenAI)
			}
		})
	}

	geminiRouter := router.Group("/v1beta/models")
	geminiRouter.Use(middleware.RouteTag("relay"))
	geminiRouter.Use(middleware.TokenAuth())
	{
		geminiRouter.GET("", func(c *gin.Context) {
			catalogcontroller.ListModels(c, constant.ChannelTypeGemini)
		})
	}

	geminiCompatibleRouter := router.Group("/v1beta/openai/models")
	geminiCompatibleRouter.Use(middleware.RouteTag("relay"))
	geminiCompatibleRouter.Use(middleware.TokenAuth())
	{
		geminiCompatibleRouter.GET("", func(c *gin.Context) {
			catalogcontroller.ListModels(c, constant.ChannelTypeOpenAI)
		})
	}

	playgroundRouter := router.Group("/pg")
	playgroundRouter.Use(middleware.RouteTag("relay"))
	playgroundRouter.Use(middleware.SystemPerformanceCheck())
	playgroundRouter.Use(middleware.UserAuth(), middleware.Distribute())
	{
		playgroundRouter.POST("/chat/completions", catalogcontroller.Playground)
	}
	relayV1Router := router.Group("/v1")
	relayV1Router.Use(middleware.RouteTag("relay"))
	relayV1Router.Use(middleware.SystemPerformanceCheck())
	relayV1Router.Use(middleware.TokenAuth())
	relayV1Router.Use(middleware.ModelRequestRateLimit())
	{
		// WebSocket 路由（统一到 Relay）
		wsRouter := relayV1Router.Group("")
		wsRouter.Use(middleware.Distribute())
		wsRouter.GET("/realtime", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAIRealtime)
		})
	}
	{
		//http router
		httpRouter := relayV1Router.Group("")
		httpRouter.Use(middleware.Distribute())

		// claude related routes
		httpRouter.POST("/messages", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatClaude)
		})

		// chat related routes
		httpRouter.POST("/completions", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAI)
		})
		httpRouter.POST("/chat/completions", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAI)
		})

		// response related routes
		httpRouter.POST("/responses", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAIResponses)
		})
		httpRouter.POST("/responses/compact", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAIResponsesCompaction)
		})

		// alpha search related routes (Codex standalone web search)
		httpRouter.POST("/alpha/search", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAIAlphaSearch)
		})

		// image related routes
		httpRouter.POST("/edits", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAIImage)
		})
		httpRouter.POST("/images/generations", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAIImage)
		})
		httpRouter.POST("/images/edits", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAIImage)
		})

		// embedding related routes
		httpRouter.POST("/embeddings", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatEmbedding)
		})

		// audio related routes
		httpRouter.POST("/audio/transcriptions", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAIAudio)
		})
		httpRouter.POST("/audio/translations", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAIAudio)
		})
		httpRouter.POST("/audio/speech", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAIAudio)
		})

		// rerank related routes
		httpRouter.POST("/rerank", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatRerank)
		})

		// gemini relay routes
		httpRouter.POST("/engines/:model/embeddings", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatGemini)
		})
		httpRouter.POST("/models/*path", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatGemini)
		})

		// other relay routes
		httpRouter.POST("/moderations", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatOpenAI)
		})

		// not implemented
		httpRouter.POST("/images/variations", topcontroller.RelayNotImplemented)
		httpRouter.GET("/files", topcontroller.RelayNotImplemented)
		httpRouter.POST("/files", topcontroller.RelayNotImplemented)
		httpRouter.DELETE("/files/:id", topcontroller.RelayNotImplemented)
		httpRouter.GET("/files/:id", topcontroller.RelayNotImplemented)
		httpRouter.GET("/files/:id/content", topcontroller.RelayNotImplemented)
		httpRouter.POST("/fine-tunes", topcontroller.RelayNotImplemented)
		httpRouter.GET("/fine-tunes", topcontroller.RelayNotImplemented)
		httpRouter.GET("/fine-tunes/:id", topcontroller.RelayNotImplemented)
		httpRouter.POST("/fine-tunes/:id/cancel", topcontroller.RelayNotImplemented)
		httpRouter.GET("/fine-tunes/:id/events", topcontroller.RelayNotImplemented)
		httpRouter.DELETE("/models/:model", topcontroller.RelayNotImplemented)
	}

	relayMjRouter := router.Group("/mj")
	relayMjRouter.Use(middleware.RouteTag("relay"))
	relayMjRouter.Use(middleware.SystemPerformanceCheck())
	registerMjRouterGroup(relayMjRouter)

	relayMjModeRouter := router.Group("/:mode/mj")
	relayMjModeRouter.Use(middleware.RouteTag("relay"))
	relayMjModeRouter.Use(middleware.SystemPerformanceCheck())
	registerMjRouterGroup(relayMjModeRouter)
	//relayMjRouter.Use()

	relaySunoRouter := router.Group("/suno")
	relaySunoRouter.Use(middleware.RouteTag("relay"))
	relaySunoRouter.Use(middleware.SystemPerformanceCheck())
	relaySunoRouter.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		relaySunoRouter.POST("/submit/:action", topcontroller.RelayTask)
		relaySunoRouter.POST("/fetch", topcontroller.RelayTaskFetch)
		relaySunoRouter.GET("/fetch/:id", topcontroller.RelayTaskFetch)
	}

	relayGeminiRouter := router.Group("/v1beta")
	relayGeminiRouter.Use(middleware.RouteTag("relay"))
	relayGeminiRouter.Use(middleware.SystemPerformanceCheck())
	relayGeminiRouter.Use(middleware.TokenAuth())
	relayGeminiRouter.Use(middleware.ModelRequestRateLimit())
	relayGeminiRouter.Use(middleware.Distribute())
	{
		// Gemini API 路径格式: /v1beta/models/{model_name}:{action}
		relayGeminiRouter.POST("/models/*path", func(c *gin.Context) {
			topcontroller.Relay(c, types.RelayFormatGemini)
		})
	}
}

func registerMjRouterGroup(relayMjRouter *gin.RouterGroup) {
	relayMjRouter.GET("/image/:id", relay.RelayMidjourneyImage)
	relayMjRouter.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		relayMjRouter.POST("/submit/action", topcontroller.RelayMidjourney)
		relayMjRouter.POST("/submit/shorten", topcontroller.RelayMidjourney)
		relayMjRouter.POST("/submit/modal", topcontroller.RelayMidjourney)
		relayMjRouter.POST("/submit/imagine", topcontroller.RelayMidjourney)
		relayMjRouter.POST("/submit/change", topcontroller.RelayMidjourney)
		relayMjRouter.POST("/submit/simple-change", topcontroller.RelayMidjourney)
		relayMjRouter.POST("/submit/describe", topcontroller.RelayMidjourney)
		relayMjRouter.POST("/submit/blend", topcontroller.RelayMidjourney)
		relayMjRouter.POST("/submit/edits", topcontroller.RelayMidjourney)
		relayMjRouter.POST("/submit/video", topcontroller.RelayMidjourney)
		//relayMjRouter.POST("/notify", topcontroller.RelayMidjourney)
		relayMjRouter.GET("/task/:id/fetch", topcontroller.RelayMidjourney)
		relayMjRouter.GET("/task/:id/image-seed", topcontroller.RelayMidjourney)
		relayMjRouter.POST("/task/list-by-condition", topcontroller.RelayMidjourney)
		relayMjRouter.POST("/insight-face/swap", topcontroller.RelayMidjourney)
		relayMjRouter.POST("/submit/upload-discord-images", topcontroller.RelayMidjourney)
	}
}
