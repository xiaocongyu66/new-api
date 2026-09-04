package compose

import (
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/QuantumNous/new-api/internal/task"
	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/QuantumNous/new-api/internal/transport/handler"
	"github.com/QuantumNous/new-api/internal/transport/middleware"
)

func SetVideoRouter(router contract.Engine) {
	// Video proxy: accepts either session auth (dashboard) or token auth (API clients)
	videoProxyRouter := router.Group("/v1")
	videoProxyRouter.Use(middleware.RouteTag("relay"))
	videoProxyRouter.Use(security.TokenOrUserAuth())
	{
		videoProxyRouter.GET("/videos/:task_id/content", task.VideoProxy)
	}

	videoV1Router := router.Group("/v1")
	videoV1Router.Use(middleware.RouteTag("relay"))
	videoV1Router.Use(security.TokenAuth(), middleware.Distribute())
	{
		videoV1Router.POST("/video/generations", handler.RelayTask)
		videoV1Router.GET("/video/generations/:task_id", handler.RelayTaskFetch)
		videoV1Router.POST("/videos/:video_id/remix", handler.RelayTask)
	}
	// openai compatible API video routes
	// docs: https://platform.openai.com/docs/api-reference/videos/create
	{
		videoV1Router.POST("/videos", handler.RelayTask)
		videoV1Router.GET("/videos/:task_id", handler.RelayTaskFetch)
	}

	klingV1Router := router.Group("/kling/v1")
	klingV1Router.Use(middleware.RouteTag("relay"))
	klingV1Router.Use(middleware.KlingRequestConvert(), security.TokenAuth(), middleware.Distribute())
	{
		klingV1Router.POST("/videos/text2video", handler.RelayTask)
		klingV1Router.POST("/videos/image2video", handler.RelayTask)
		klingV1Router.GET("/videos/text2video/:task_id", handler.RelayTaskFetch)
		klingV1Router.GET("/videos/image2video/:task_id", handler.RelayTaskFetch)
	}

	// Jimeng official API routes - direct mapping to official API format
	jimengOfficialGroup := router.Group("jimeng")
	jimengOfficialGroup.Use(middleware.RouteTag("relay"))
	jimengOfficialGroup.Use(middleware.JimengRequestConvert(), security.TokenAuth(), middleware.Distribute())
	{
		// Maps to: /?Action=CVSync2AsyncSubmitTask&Version=2022-08-31 and /?Action=CVSync2AsyncGetResult&Version=2022-08-31
		jimengOfficialGroup.POST("/", handler.RelayTask)
	}
}
