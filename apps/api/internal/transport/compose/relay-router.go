package compose

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	tpmw "github.com/QuantumNous/new-api/internal/transport/middleware"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

func SetRelayRouter(router *gin.Engine) {
	router.Use(tpmw.CORS())
	router.Use(tpmw.DecompressRequestMiddleware())
	router.Use(ginadapter.Middleware(middleware.BodyStorageCleanup())) // 清理请求体存储
	router.Use(ginadapter.Middleware(middleware.StatsMiddleware()))
	// https://platform.openai.com/docs/api-reference/introduction
	modelsRouter := router.Group("/v1/models")
	modelsRouter.Use(tpmw.RouteTag("relay"))
	modelsRouter.Use(ginadapter.Middleware(security.TokenAuth()))
	{
		modelsRouter.GET("", func(cc *gin.Context) {
			switch {
			case cc.GetHeader("x-api-key") != "" && cc.GetHeader("anthropic-version") != "":
				controller.ListModels(ginadapter.Wrap(cc), constant.ChannelTypeAnthropic)
			case cc.GetHeader("x-goog-api-key") != "" || cc.Query("key") != "": // 单独的适配
				controller.RetrieveModel(ginadapter.Wrap(cc), constant.ChannelTypeGemini)
			default:
				controller.ListModels(ginadapter.Wrap(cc), constant.ChannelTypeOpenAI)
			}
		})

		modelsRouter.GET("/:model", func(cc *gin.Context) {
			switch {
			case cc.GetHeader("x-api-key") != "" && cc.GetHeader("anthropic-version") != "":
				controller.RetrieveModel(ginadapter.Wrap(cc), constant.ChannelTypeAnthropic)
			default:
				controller.RetrieveModel(ginadapter.Wrap(cc), constant.ChannelTypeOpenAI)
			}
		})
	}

	geminiRouter := router.Group("/v1beta/models")
	geminiRouter.Use(tpmw.RouteTag("relay"))
	geminiRouter.Use(ginadapter.Middleware(security.TokenAuth()))
	{
		geminiRouter.GET("", func(cc *gin.Context) {
			controller.ListModels(ginadapter.Wrap(cc), constant.ChannelTypeGemini)
		})
	}

	geminiCompatibleRouter := router.Group("/v1beta/openai/models")
	geminiCompatibleRouter.Use(tpmw.RouteTag("relay"))
	geminiCompatibleRouter.Use(ginadapter.Middleware(security.TokenAuth()))
	{
		geminiCompatibleRouter.GET("", func(cc *gin.Context) {
			controller.ListModels(ginadapter.Wrap(cc), constant.ChannelTypeOpenAI)
		})
	}

	playgroundRouter := router.Group("/pg")
	playgroundRouter.Use(tpmw.RouteTag("relay"))
	playgroundRouter.Use(ginadapter.Middleware(middleware.SystemPerformanceCheck()))
	playgroundRouter.Use(ginadapter.Middleware(security.UserAuth()), ginadapter.Middleware(middleware.Distribute()))
	{
		playgroundRouter.POST("/chat/completions", ginadapter.Handler(controller.Playground))
	}
	relayV1Router := router.Group("/v1")
	relayV1Router.Use(tpmw.RouteTag("relay"))
	relayV1Router.Use(ginadapter.Middleware(middleware.SystemPerformanceCheck()))
	relayV1Router.Use(ginadapter.Middleware(security.TokenAuth()))
	relayV1Router.Use(ginadapter.Middleware(middleware.ModelRequestRateLimit()))
	{
		// WebSocket 路由（统一到 Relay）
		wsRouter := relayV1Router.Group("")
		wsRouter.Use(ginadapter.Middleware(middleware.Distribute()))
		wsRouter.GET("/realtime", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAIRealtime)
		})
	}
	{
		//http router
		httpRouter := relayV1Router.Group("")
		httpRouter.Use(ginadapter.Middleware(middleware.Distribute()))

		// claude related routes
		httpRouter.POST("/messages", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatClaude)
		})

		// chat related routes
		httpRouter.POST("/completions", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAI)
		})
		httpRouter.POST("/chat/completions", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAI)
		})

		// response related routes
		httpRouter.POST("/responses", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAIResponses)
		})
		httpRouter.POST("/responses/compact", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAIResponsesCompaction)
		})

		// alpha search related routes (Codex standalone web search)
		httpRouter.POST("/alpha/search", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAIAlphaSearch)
		})

		// image related routes
		httpRouter.POST("/edits", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAIImage)
		})
		httpRouter.POST("/images/generations", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAIImage)
		})
		httpRouter.POST("/images/edits", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAIImage)
		})

		// embedding related routes
		httpRouter.POST("/embeddings", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatEmbedding)
		})

		// audio related routes
		httpRouter.POST("/audio/transcriptions", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAIAudio)
		})
		httpRouter.POST("/audio/translations", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAIAudio)
		})
		httpRouter.POST("/audio/speech", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAIAudio)
		})

		// rerank related routes
		httpRouter.POST("/rerank", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatRerank)
		})

		// gemini relay routes
		httpRouter.POST("/engines/:model/embeddings", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatGemini)
		})
		httpRouter.POST("/models/*path", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatGemini)
		})

		// other relay routes
		httpRouter.POST("/moderations", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatOpenAI)
		})

		// not implemented
		httpRouter.POST("/images/variations", ginadapter.Handler(controller.RelayNotImplemented))
		httpRouter.GET("/files", ginadapter.Handler(controller.RelayNotImplemented))
		httpRouter.POST("/files", ginadapter.Handler(controller.RelayNotImplemented))
		httpRouter.DELETE("/files/:id", ginadapter.Handler(controller.RelayNotImplemented))
		httpRouter.GET("/files/:id", ginadapter.Handler(controller.RelayNotImplemented))
		httpRouter.GET("/files/:id/content", ginadapter.Handler(controller.RelayNotImplemented))
		httpRouter.POST("/fine-tunes", ginadapter.Handler(controller.RelayNotImplemented))
		httpRouter.GET("/fine-tunes", ginadapter.Handler(controller.RelayNotImplemented))
		httpRouter.GET("/fine-tunes/:id", ginadapter.Handler(controller.RelayNotImplemented))
		httpRouter.POST("/fine-tunes/:id/cancel", ginadapter.Handler(controller.RelayNotImplemented))
		httpRouter.GET("/fine-tunes/:id/events", ginadapter.Handler(controller.RelayNotImplemented))
		httpRouter.DELETE("/models/:model", ginadapter.Handler(controller.RelayNotImplemented))
	}
	relayMjRouter := router.Group("/mj")
	relayMjRouter.Use(tpmw.RouteTag("relay"))
	relayMjRouter.Use(ginadapter.Middleware(middleware.SystemPerformanceCheck()))
	registerMjRouterGroup(relayMjRouter)
	relayMjModeRouter := router.Group("/:mode/mj")
	relayMjModeRouter.Use(tpmw.RouteTag("relay"))
	relayMjModeRouter.Use(ginadapter.Middleware(middleware.SystemPerformanceCheck()))
	registerMjRouterGroup(relayMjModeRouter)
	//relayMjRouter.Use()
	relaySunoRouter := router.Group("/suno")
	relaySunoRouter.Use(tpmw.RouteTag("relay"))
	relaySunoRouter.Use(ginadapter.Middleware(middleware.SystemPerformanceCheck()))
	relaySunoRouter.Use(ginadapter.Middleware(security.TokenAuth()), ginadapter.Middleware(middleware.Distribute()))
	{
		relaySunoRouter.POST("/submit/:action", ginadapter.Handler(controller.RelayTask))
		relaySunoRouter.POST("/fetch", ginadapter.Handler(controller.RelayTaskFetch))
		relaySunoRouter.GET("/fetch/:id", ginadapter.Handler(controller.RelayTaskFetch))
	}
	relayGeminiRouter := router.Group("/v1beta")
	relayGeminiRouter.Use(tpmw.RouteTag("relay"))
	relayGeminiRouter.Use(ginadapter.Middleware(middleware.SystemPerformanceCheck()))
	relayGeminiRouter.Use(ginadapter.Middleware(security.TokenAuth()))
	relayGeminiRouter.Use(ginadapter.Middleware(middleware.ModelRequestRateLimit()))
	relayGeminiRouter.Use(ginadapter.Middleware(middleware.Distribute()))
	{
		// Gemini API 路径格式: /v1beta/models/{model_name}:{action}
		relayGeminiRouter.POST("/models/*path", func(cc *gin.Context) {
			controller.Relay(ginadapter.Wrap(cc), types.RelayFormatGemini)
		})
	}
}

func registerMjRouterGroup(relayMjRouter *gin.RouterGroup) {
	relayMjRouter.GET("/image/:id", ginadapter.Handler(relay.RelayMidjourneyImage))
	relayMjRouter.Use(ginadapter.Middleware(security.TokenAuth()), ginadapter.Middleware(middleware.Distribute()))
	{
		relayMjRouter.POST("/submit/action", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.POST("/submit/shorten", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.POST("/submit/modal", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.POST("/submit/imagine", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.POST("/submit/change", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.POST("/submit/simple-change", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.POST("/submit/describe", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.POST("/submit/blend", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.POST("/submit/edits", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.POST("/submit/video", ginadapter.Handler(controller.RelayMidjourney))
		//relayMjRouter.POST("/notify", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.GET("/task/:id/fetch", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.GET("/task/:id/image-seed", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.POST("/task/list-by-condition", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.POST("/insight-face/swap", ginadapter.Handler(controller.RelayMidjourney))
		relayMjRouter.POST("/submit/upload-discord-images", ginadapter.Handler(controller.RelayMidjourney))
	}
}
