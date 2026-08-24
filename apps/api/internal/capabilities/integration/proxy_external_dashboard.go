package integration

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const karmadaDashboardSessionCookie = "newapi_karmada_session"

func CreateKarmadaDashboardSession(c contract.Context, ident AuthIdentity) {
	sessionToken, expiresAt, err := IssueKarmadaDashboardSession(ident)
	if err != nil {
		AbortWithMessage(c, http.StatusInternalServerError, "failed to issue karmada dashboard session")
		return
	}

	c.SetCookie(&http.Cookie{
		Name:     karmadaDashboardSessionCookie,
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(KarmadaDashboardSessionTTL.Seconds()),
	})
	c.SetHeader("Cache-Control", "no-store")
	_ = c.JSON(http.StatusOK, common.H{"success": true, "data": common.H{"expires_at": expiresAt}})
}

func ProxyKarmadaDashboard(c contract.Context) {
	upstreamRaw := strings.TrimSpace(os.Getenv("KARMADA_DASHBOARD_URL"))
	credential := strings.TrimSpace(os.Getenv("KARMADA_DASHBOARD_TOKEN"))
	if upstreamRaw == "" || credential == "" {
		common.SysError("Karmada dashboard proxy is not configured")
		AbortWithMessage(c, http.StatusInternalServerError, "karmada dashboard proxy is not configured")
		return
	}

	upstream, err := url.Parse(upstreamRaw)
	if err != nil || upstream.Scheme == "" || upstream.Host == "" {
		common.SysError("Karmada dashboard proxy target is invalid: " + upstreamRaw)
		AbortWithMessage(c, http.StatusInternalServerError, "karmada dashboard proxy target is invalid")
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(upstream)
	originalDirector := proxy.Director
	proxy.Director = func(request *http.Request) {
		originalDirector(request)
		request.Header.Set("Authorization", "Bearer "+credential)
		request.Header.Set("X-Forwarded-Host", request.Host)
		request.Header.Set("X-Forwarded-Proto", "https")
		request.Host = upstream.Host
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		response.Header.Del("Content-Security-Policy")
		response.Header.Del("Content-Security-Policy-Report-Only")
		return nil
	}
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, proxyErr error) {
		common.SysError("Karmada dashboard proxy error: " + proxyErr.Error())
		writer.WriteHeader(http.StatusBadGateway)
	}
	proxy.ServeHTTP(c.ResponseWriter(), c.HTTPRequest())
}

// PaymentReturnURL computes the full return URL for payment callbacks.
// It is used by top-up and subscription flows to redirect back to the dashboard.
func PaymentReturnURL(suffix string) string {
	base := strings.TrimRight(system_setting.ServerAddress, "/")
	return base + suffix
}