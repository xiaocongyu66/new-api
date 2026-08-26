package compose

import (
	"fmt"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	tpmw "github.com/QuantumNous/new-api/internal/transport/middleware"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
)

func SetRouter(router *gin.Engine, assets WebAssets) {
	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	SetVideoRouter(router)
	router.Any("/karmada-dashboard/*path", ginadapter.Handler(controller.ProxyKarmadaDashboard))
	frontendBaseUrl := os.Getenv("FRONTEND_BASE_URL")
	if common.IsMasterNode && frontendBaseUrl != "" {
		frontendBaseUrl = ""
		common.SysLog("FRONTEND_BASE_URL is ignored on master node")
	}
	if frontendBaseUrl == "" {
		SetWebRouter(router, assets)
	} else {
		frontendBaseUrl = strings.TrimSuffix(frontendBaseUrl, "/")
		router.NoRoute(func(cc *gin.Context) {
			cc.Set(tpmw.RouteTagKey, "web")
			cc.Redirect(http.StatusMovedPermanently, fmt.Sprintf("%s%s", frontendBaseUrl, cc.Request.RequestURI))
		})
	}
}
