package compose

import (
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/relay"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/handler"
	"github.com/QuantumNous/new-api/internal/transport/middleware"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/QuantumNous/new-api/internal/security"
)

func SetRelayRouter(router contract.Engine) {
	router.UseCORS()
	router.Use(middleware.DecompressRequestMiddleware())
	router.Use(middleware.BodyStorageCleanup()) // 清理请求体存储
	router.Use(middleware.StatsMiddleware())
	// https://platform.openai.com/docs/api-reference/introduction
	modelsRouter := router.Group("/v1/models")
	modelsRouter.Use(middleware.RouteTag("relay"))
	modelsRouter.Use(security.TokenAuth())
	{
		modelsRouter.GET("", func(cc contract.Context) {
			switch {
			case cc.Header("x-api-key") != "" && cc.Header("anthropic-version") != "":
				handler.ListModels(cc, constant.ChannelTypeAnthropic)
			case cc.Header("x-goog-api-key") != "" || cc.Query("key") != "": // 单独的适配
				handler.RetrieveModel(cc, constant.ChannelTypeGemini)
			default:
				handler.ListModels(cc, constant.ChannelTypeOpenAI)
			}
		})

		modelsRouter.GET("/:model", func(cc contract.Context) {
			switch {
			case cc.Header("x-api-key") != "" && cc.Header("anthropic-version") != "":
				handler.RetrieveModel(cc, constant.ChannelTypeAnthropic)
			default:
				handler.RetrieveModel(cc, constant.ChannelTypeOpenAI)
			}
		})
	}

	geminiRouter := router.Group("/v1beta/models")
	geminiRouter.Use(middleware.RouteTag("relay"))
	geminiRouter.Use(security.TokenAuth())
	{
		geminiRouter.GET("", func(cc contract.Context) {
			handler.ListModels(cc, constant.ChannelTypeGemini)
		})
	}

	geminiCompatibleRouter := router.Group("/v1beta/openai/models")
	geminiCompatibleRouter.Use(middleware.RouteTag("relay"))
	geminiCompatibleRouter.Use(security.TokenAuth())
	{
		geminiCompatibleRouter.GET("", func(cc contract.Context) {
			handler.ListModels(cc, constant.ChannelTypeOpenAI)
		})
	}

	playgroundRouter := router.Group("/pg")
	playgroundRouter.Use(middleware.RouteTag("relay"))
	playgroundRouter.Use(middleware.SystemPerformanceCheck())
	playgroundRouter.Use(security.UserAuth(), middleware.Distribute())
	{
		playgroundRouter.POST("/chat/completions", handler.Playground)
	}
	relayV1Router := router.Group("/v1")
	relayV1Router.Use(middleware.RouteTag("relay"))
	relayV1Router.Use(middleware.SystemPerformanceCheck())
	relayV1Router.Use(security.TokenAuth())
	relayV1Router.Use(middleware.ModelRequestRateLimit())
	relayV1Router.Use(middleware.UserInsight())
	{
		// WebSocket 路由（统一到 Relay）
		wsRouter := relayV1Router.Group("")
		wsRouter.Use(middleware.Distribute())
		wsRouter.GET("/realtime", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAIRealtime)
		})
	}
	{
		//http router
		httpRouter := relayV1Router.Group("")
		httpRouter.Use(middleware.Distribute())

		// claude related routes
		httpRouter.POST("/messages", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatClaude)
		})

		// chat related routes
		httpRouter.POST("/completions", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAI)
		})
		httpRouter.POST("/chat/completions", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAI)
		})

		// response related routes
		httpRouter.POST("/responses", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAIResponses)
		})
		httpRouter.POST("/responses/compact", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAIResponsesCompaction)
		})

		// alpha search related routes (Codex standalone web search)
		httpRouter.POST("/alpha/search", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAIAlphaSearch)
		})

		// image related routes
		httpRouter.POST("/edits", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAIImage)
		})
		httpRouter.POST("/images/generations", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAIImage)
		})
		httpRouter.POST("/images/edits", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAIImage)
		})

		// embedding related routes
		httpRouter.POST("/embeddings", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatEmbedding)
		})

		// audio related routes
		httpRouter.POST("/audio/transcriptions", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAIAudio)
		})
		httpRouter.POST("/audio/translations", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAIAudio)
		})
		httpRouter.POST("/audio/speech", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAIAudio)
		})

		// rerank related routes
		httpRouter.POST("/rerank", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatRerank)
		})

		// gemini relay routes
		httpRouter.POST("/engines/:model/embeddings", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatGemini)
		})
		httpRouter.POST("/models/*path", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatGemini)
		})

		// other relay routes
		httpRouter.POST("/moderations", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatOpenAI)
		})

		// not implemented
		httpRouter.POST("/images/variations", handler.RelayNotImplemented)
		httpRouter.GET("/files", handler.RelayNotImplemented)
		httpRouter.POST("/files", handler.RelayNotImplemented)
		httpRouter.DELETE("/files/:id", handler.RelayNotImplemented)
		httpRouter.GET("/files/:id", handler.RelayNotImplemented)
		httpRouter.GET("/files/:id/content", handler.RelayNotImplemented)
		httpRouter.POST("/fine-tunes", handler.RelayNotImplemented)
		httpRouter.GET("/fine-tunes", handler.RelayNotImplemented)
		httpRouter.GET("/fine-tunes/:id", handler.RelayNotImplemented)
		httpRouter.POST("/fine-tunes/:id/cancel", handler.RelayNotImplemented)
		httpRouter.GET("/fine-tunes/:id/events", handler.RelayNotImplemented)
		httpRouter.DELETE("/models/:model", handler.RelayNotImplemented)
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
	relaySunoRouter.Use(security.TokenAuth(), middleware.Distribute())
	{
		relaySunoRouter.POST("/submit/:action", handler.RelayTask)
		relaySunoRouter.POST("/fetch", handler.RelayTaskFetch)
		relaySunoRouter.GET("/fetch/:id", handler.RelayTaskFetch)
	}
	relayGeminiRouter := router.Group("/v1beta")
	relayGeminiRouter.Use(middleware.RouteTag("relay"))
	relayGeminiRouter.Use(middleware.SystemPerformanceCheck())
	relayGeminiRouter.Use(security.TokenAuth())
	relayGeminiRouter.Use(middleware.ModelRequestRateLimit())
	relayGeminiRouter.Use(middleware.UserInsight())
	relayGeminiRouter.Use(middleware.Distribute())
	{
		// Gemini API 路径格式: /v1beta/models/{model_name}:{action}
		relayGeminiRouter.POST("/models/*path", func(cc contract.Context) {
			handler.Relay(cc, types.RelayFormatGemini)
		})
	}
}

func registerMjRouterGroup(relayMjRouter contract.Routes) {
	relayMjRouter.GET("/image/:id", relay.RelayMidjourneyImage)
	relayMjRouter.Use(security.TokenAuth(), middleware.Distribute())
	{
		relayMjRouter.POST("/submit/action", handler.RelayMidjourney)
		relayMjRouter.POST("/submit/shorten", handler.RelayMidjourney)
		relayMjRouter.POST("/submit/modal", handler.RelayMidjourney)
		relayMjRouter.POST("/submit/imagine", handler.RelayMidjourney)
		relayMjRouter.POST("/submit/change", handler.RelayMidjourney)
		relayMjRouter.POST("/submit/simple-change", handler.RelayMidjourney)
		relayMjRouter.POST("/submit/describe", handler.RelayMidjourney)
		relayMjRouter.POST("/submit/blend", handler.RelayMidjourney)
		relayMjRouter.POST("/submit/edits", handler.RelayMidjourney)
		relayMjRouter.POST("/submit/video", handler.RelayMidjourney)
		//relayMjRouter.POST("/notify", handler.RelayMidjourney)
		relayMjRouter.GET("/task/:id/fetch", handler.RelayMidjourney)
		relayMjRouter.GET("/task/:id/image-seed", handler.RelayMidjourney)
		relayMjRouter.POST("/task/list-by-condition", handler.RelayMidjourney)
		relayMjRouter.POST("/insight-face/swap", handler.RelayMidjourney)
		relayMjRouter.POST("/submit/upload-discord-images", handler.RelayMidjourney)
	}
}
