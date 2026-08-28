package compose

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/QuantumNous/new-api/internal/transport/middleware"
	"github.com/QuantumNous/new-api/internal/security"

	"github.com/gin-gonic/gin"
)

func SetVideoRouter(router *gin.Engine) {
	// Video proxy: accepts either session auth (dashboard) or token auth (API clients)
	videoProxyRouter := router.Group("/v1")
	videoProxyRouter.Use(middleware.RouteTag("relay"))
	videoProxyRouter.Use(ginadapter.Middleware(security.TokenOrUserAuth()))
	{
		videoProxyRouter.GET("/videos/:task_id/content", ginadapter.Handler(controller.VideoProxy))
	}

	videoV1Router := router.Group("/v1")
	videoV1Router.Use(middleware.RouteTag("relay"))
	videoV1Router.Use(ginadapter.Middleware(security.TokenAuth()), ginadapter.Middleware(middleware.Distribute()))
	{
		videoV1Router.POST("/video/generations", ginadapter.Handler(controller.RelayTask))
		videoV1Router.GET("/video/generations/:task_id", ginadapter.Handler(controller.RelayTaskFetch))
		videoV1Router.POST("/videos/:video_id/remix", ginadapter.Handler(controller.RelayTask))
	}
	// openai compatible API video routes
	// docs: https://platform.openai.com/docs/api-reference/videos/create
	{
		videoV1Router.POST("/videos", ginadapter.Handler(controller.RelayTask))
		videoV1Router.GET("/videos/:task_id", ginadapter.Handler(controller.RelayTaskFetch))
	}

	klingV1Router := router.Group("/kling/v1")
	klingV1Router.Use(middleware.RouteTag("relay"))
	klingV1Router.Use(ginadapter.Middleware(middleware.KlingRequestConvert()), ginadapter.Middleware(security.TokenAuth()), ginadapter.Middleware(middleware.Distribute()))
	{
		klingV1Router.POST("/videos/text2video", ginadapter.Handler(controller.RelayTask))
		klingV1Router.POST("/videos/image2video", ginadapter.Handler(controller.RelayTask))
		klingV1Router.GET("/videos/text2video/:task_id", ginadapter.Handler(controller.RelayTaskFetch))
		klingV1Router.GET("/videos/image2video/:task_id", ginadapter.Handler(controller.RelayTaskFetch))
	}

	// Jimeng official API routes - direct mapping to official API format
	jimengOfficialGroup := router.Group("jimeng")
	jimengOfficialGroup.Use(middleware.RouteTag("relay"))
	jimengOfficialGroup.Use(ginadapter.Middleware(middleware.JimengRequestConvert()), ginadapter.Middleware(security.TokenAuth()), ginadapter.Middleware(middleware.Distribute()))
	{
		// Maps to: /?Action=CVSync2AsyncSubmitTask&Version=2022-08-31 and /?Action=CVSync2AsyncGetResult&Version=2022-08-31
		jimengOfficialGroup.POST("/", ginadapter.Handler(controller.RelayTask))
	}
}
