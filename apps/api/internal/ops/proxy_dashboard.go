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
	// http.ResponseWriter. Under fiber/middleware/proxy there is no error hook:
	// proxy.Do returns the transport error to the handler, so this becomes an
	// `if err := proxy.Do(...); err != nil { log; return c.SendStatus(502) }`
	// at the call site below.
	proxy.ErrorHandler = func(writer http.ResponseWriter, _ *http.Request, proxyErr error) {
		common.SysError("Karmada dashboard proxy error: " + proxyErr.Error())
		writer.WriteHeader(http.StatusBadGateway)
	}
	// TODO(#287) B: fasthttp has NO ReverseProxy type. fasthttpproxy is a
	// client-side dialer package (FasthttpHTTPDialer returns a fasthttp.DialFunc
	// for CONNECT/SOCKS5 egress), not a reverse proxy — do not migrate onto it.
	// The replacement is fiber/middleware/proxy: proxy.Do(c, upstream+path) or
	// proxy.DoTimeout, which rewrites c.Request()'s URI, runs the client, and
	// fills c.Response() in place. Mapping for the three hooks above:
	//   Director       → set the headers on c.Request().Header before proxy.Do;
	//                    request.Host = upstream.Host is implicit in the addr.
	//   ModifyResponse → Del the CSP headers on c.Response().Header after it
	//                    returns (proxy.Do has no response callback).
	//   ErrorHandler   → the non-nil error return, see above.
	// Two behaviour regressions that must be handled, not discovered later:
	//   1. No streaming by default. proxy.Do buffers the entire upstream body;
	//      incremental delivery needs a &fasthttp.Client{StreamResponseBody:true}
	//      passed in via proxy.WithClient (global) or as the variadic client arg.
	//   2. No protocol upgrades at all. doAction does
	//      req.Header.Del(fiber.HeaderConnection) on the way out and again on the
	//      response, so a 101 can never be negotiated, whereas ReverseProxy
	//      handles StatusSwitchingProtocols by hijacking and splicing
	//      (reverseproxy.go:561-565, handleUpgradeResponse). If the Karmada
	//      dashboard ever uses websockets, proxy.Do is a functional regression
	//      and the upgrade path needs a hand-written hijack.
	proxy.ServeHTTP(c.ResponseWriter(), c.HTTPRequest())
}
