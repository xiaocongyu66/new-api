package ops

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
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
	// TODO(#287) B: httputil.ReverseProxy's ErrorHandler is typed on
	// http.ResponseWriter; fasthttp has an equivalent reverse-proxy path, so this
	// handler is reimplemented against it at the cutover.
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, proxyErr error) {
		common.SysError("Karmada dashboard proxy error: " + proxyErr.Error())
		writer.WriteHeader(http.StatusBadGateway)
	}
	// TODO(#287) B: httputil.ReverseProxy.ServeHTTP consumes a concrete writer and
	// request; fasthttp's proxy client (fasthttp/fasthttpproxy) is the equivalent this
	// is rewritten onto at the cutover.
	proxy.ServeHTTP(c.ResponseWriter(), c.HTTPRequest())
}
