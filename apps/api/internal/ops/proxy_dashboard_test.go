package ops

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/transport/ginadapter"

	"github.com/gin-gonic/gin"
	"github.com/gofiber/fiber/v2"
	fiberproxy "github.com/gofiber/fiber/v2/middleware/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

const dashboardProxyCredential = "dashboard-test-token"

type dashboardGateway struct {
	name  string
	serve func(*testing.T, string) string
}

func dashboardGateways() []dashboardGateway {
	return []dashboardGateway{
		{name: "gin", serve: serveGinDashboardProxy},
		{name: "fiber", serve: serveFiberDashboardProxy},
	}
}

func TestKarmadaDashboardProxyObservableBehavior(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "/ui/api", r.URL.Path)
		assert.Equal(t, "view=table", r.URL.RawQuery)
		assert.Equal(t, "dashboard.example", r.Header.Get("X-Forwarded-Host"))
		assert.Equal(t, "https", r.Header.Get("X-Forwarded-Proto"))
		assert.Equal(t, "Bearer "+dashboardProxyCredential, r.Header.Get("Authorization"))
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("Content-Security-Policy-Report-Only", "report-to csp")
		w.Header().Set("X-Upstream", "present")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	for _, gateway := range dashboardGateways() {
		t.Run(gateway.name, func(t *testing.T) {
			response := dashboardRequest(t, gateway.serve(t, upstream.URL), http.MethodPatch, "/ui/api?view=table")
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.Equal(t, http.StatusCreated, response.StatusCode)
			assert.Equal(t, `{"ok":true}`, string(body))
			assert.Equal(t, "present", response.Header.Get("X-Upstream"))
			assert.Empty(t, response.Header.Get("Content-Security-Policy"))
			assert.Empty(t, response.Header.Get("Content-Security-Policy-Report-Only"))
		})
	}
}

func TestKarmadaDashboardProxyUpstreamFailure(t *testing.T) {
	for _, gateway := range dashboardGateways() {
		t.Run(gateway.name, func(t *testing.T) {
			response := dashboardRequest(t, gateway.serve(t, "http://127.0.0.1:1"), http.MethodGet, "/ui")
			defer response.Body.Close()
			assert.Equal(t, http.StatusBadGateway, response.StatusCode)
		})
	}
}

func TestKarmadaDashboardProxyStreamsBeforeUpstreamCompletes(t *testing.T) {
	finished := make(chan struct{})
	var finishOnce sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		require.True(t, ok)
		_, _ = w.Write([]byte("first"))
		flusher.Flush()
		time.Sleep(250 * time.Millisecond)
		_, _ = w.Write([]byte("second"))
		finishOnce.Do(func() { close(finished) })
	}))
	defer upstream.Close()

	for _, gateway := range dashboardGateways() {
		t.Run(gateway.name, func(t *testing.T) {
			response := dashboardRequest(t, gateway.serve(t, upstream.URL), http.MethodGet, "/stream")
			defer response.Body.Close()
			first := make([]byte, len("first"))
			firstRead := make(chan error, 1)
			go func() { _, err := io.ReadFull(response.Body, first); firstRead <- err }()
			select {
			case err := <-firstRead:
				require.NoError(t, err)
				assert.Equal(t, "first", string(first))
			case <-finished:
				t.Fatal("first bytes arrived only after the upstream completed")
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for the proxied first bytes")
			}
		})
	}
}

func TestKarmadaDashboardProxyUpgradeBehavior(t *testing.T) {
	upstreamURL, closeUpstream := dashboardUpgradeUpstream(t)
	defer closeUpstream()

	for _, gateway := range dashboardGateways() {
		t.Run(gateway.name, func(t *testing.T) {
			status, headers := rawUpgrade(t, gateway.serve(t, upstreamURL))
			if gateway.name == "gin" {
				assert.Equal(t, http.StatusSwitchingProtocols, status)
				assert.Equal(t, "websocket", strings.ToLower(headers.Get("Upgrade")))
				return
			}
			assert.Equal(t, http.StatusBadRequest, status,
				"fiber proxy.Do removes Connection before sending the upstream request")
		})
	}
}

func serveGinDashboardProxy(t *testing.T, upstream string) string {
	t.Helper()
	t.Setenv("KARMADA_DASHBOARD_URL", upstream)
	t.Setenv("KARMADA_DASHBOARD_TOKEN", dashboardProxyCredential)
	app := gin.New()
	app.Any("/*path", ginadapter.Handler(ProxyKarmadaDashboard))
	server := httptest.NewServer(app)
	t.Cleanup(server.Close)
	return server.URL
}

func serveFiberDashboardProxy(t *testing.T, upstream string) string {
	t.Helper()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	app.All("/*", func(c *fiber.Ctx) error { return fiberDashboardProxy(c, upstream) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() { _ = app.Listener(listener) }()
	return "http://" + listener.Addr().String()
}

func dashboardRequest(t *testing.T, gatewayURL, method, path string) *http.Response {
	t.Helper()
	target, err := url.Parse(gatewayURL)
	require.NoError(t, err)
	req, err := http.NewRequest(method, target.String()+path, nil)
	require.NoError(t, err)
	req.Host = "dashboard.example"
	response, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return response
}

// fiberDashboardProxy is the exact candidate mapping for the fiber transport.
// It deliberately lives in this test until the upgrade contract can be preserved.
func fiberDashboardProxy(c *fiber.Ctx, upstream string) error {
	c.Request().Header.Set("Authorization", "Bearer "+dashboardProxyCredential)
	c.Request().Header.Set("X-Forwarded-Host", c.Hostname())
	c.Request().Header.Set("X-Forwarded-Proto", "https")
	if err := fiberproxy.Do(c, strings.TrimSuffix(upstream, "/")+c.OriginalURL(), &fasthttp.Client{StreamResponseBody: true}); err != nil {
		return c.SendStatus(http.StatusBadGateway)
	}
	c.Response().Header.Del("Content-Security-Policy")
	c.Response().Header.Del("Content-Security-Policy-Report-Only")
	return nil
}

func dashboardUpgradeUpstream(t *testing.T) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go dashboardUpgradeConnection(conn)
		}
	}()
	return "http://" + listener.Addr().String(), func() {
		_ = listener.Close()
		<-finished
	}
}

func dashboardUpgradeConnection(conn net.Conn) {
	defer conn.Close()
	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}
	if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") && strings.Contains(strings.ToLower(req.Header.Get("Connection")), "upgrade") {
		_, _ = io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		return
	}
	_, _ = io.WriteString(conn, "HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n")
}

func rawUpgrade(t *testing.T, gatewayURL string) (int, http.Header) {
	t.Helper()
	target, err := url.Parse(gatewayURL)
	require.NoError(t, err)
	conn, err := net.Dial("tcp", target.Host)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = fmt.Fprintf(conn, "GET /socket HTTP/1.1\r\nHost: dashboard.example\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	require.NoError(t, err)
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet})
	require.NoError(t, err)
	return response.StatusCode, response.Header
}
