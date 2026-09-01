package ginadapter

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/static"
	"github.com/gin-gonic/gin"
)

// routes implements contract.Routes on a gin router group.
//
// gin's *Engine embeds a RouterGroup, so the engine's root scope and every
// child group share this implementation. They differ in one observable way:
// mounting middleware on the engine also rebuilds the no-route and no-method
// handler chains, while mounting it on a group does not. mount therefore holds
// whichever of the two the scope was built from, so middleware keeps the
// semantics of the scope it is mounted on.
type routes struct {
	group *gin.RouterGroup
	mount gin.IRoutes
}

// engine implements contract.Engine on a gin engine.
//
// srv is built here rather than in Serve so Shutdown reaches the same server
// however the value was copied: contract.Engine is held by value, and the
// lifecycle methods have to agree on which listener they are talking about.
type engine struct {
	routes
	gin *gin.Engine
	srv *http.Server
}

// WrapEngine adapts a gin engine to the transport contract, so route
// registration and engine configuration do not require the caller to import
// gin.
func WrapEngine(e *gin.Engine) contract.Engine {
	return engine{
		routes: routes{group: &e.RouterGroup, mount: e},
		gin:    e,
		// No timeouts, matching what the process configured before the
		// lifecycle moved onto the contract: a WriteTimeout would cut off long
		// SSE streams.
		srv: &http.Server{Handler: e},
	}
}

// chain converts contract handlers to gin handlers, preserving order.
func chain(handlers []contract.Chainable) []gin.HandlerFunc {
	adapted := make([]gin.HandlerFunc, len(handlers))
	for i, h := range handlers {
		adapted[i] = Handler(h)
	}
	return adapted
}

func (r routes) Group(path string) contract.Routes {
	group := r.group.Group(path)
	return routes{group: group, mount: group}
}

func (r routes) Use(handlers ...contract.Chainable) {
	r.mount.Use(chain(handlers)...)
}

func (r routes) Handle(method, path string, handlers ...contract.Chainable) {
	r.group.Handle(method, path, chain(handlers)...)
}

func (r routes) GET(path string, handlers ...contract.Chainable) {
	r.group.GET(path, chain(handlers)...)
}

func (r routes) POST(path string, handlers ...contract.Chainable) {
	r.group.POST(path, chain(handlers)...)
}

func (r routes) PUT(path string, handlers ...contract.Chainable) {
	r.group.PUT(path, chain(handlers)...)
}

func (r routes) PATCH(path string, handlers ...contract.Chainable) {
	r.group.PATCH(path, chain(handlers)...)
}

func (r routes) DELETE(path string, handlers ...contract.Chainable) {
	r.group.DELETE(path, chain(handlers)...)
}

func (r routes) Any(path string, handlers ...contract.Chainable) {
	r.group.Any(path, chain(handlers)...)
}

// UseCORS mounts gin-contrib/cors. The policy allows every origin with
// credentials, the five methods the dashboard and the relay API issue, and any
// request header, because relay clients send vendor-specific headers
// (anthropic-version, x-goog-api-key) that cannot be enumerated here.
func (r routes) UseCORS() {
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowCredentials = true
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"*"}
	r.mount.Use(cors.New(config))
}

// UseCompression mounts gin-contrib/gzip at its default level.
func (r routes) UseCompression() {
	r.mount.Use(gzip.Gzip(gzip.DefaultCompression))
}

// ServeHTTP delegates to gin's router. It is no longer part of contract.Engine
// (a fasthttp-backed engine has no equivalent), but it stays a concrete method:
// it is how Serve's http.Server dispatches, and it lets a gin engine still sit
// in an http.Handler chain.
func (e engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.gin.ServeHTTP(w, r)
}

// Serve listens on addr and blocks. http.Server reports a graceful shutdown as
// ErrServerClosed; the contract says a graceful stop is not an error, so that
// one sentinel is swallowed here instead of at every caller.
func (e engine) Serve(addr string) error {
	e.srv.Addr = addr
	if err := e.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown stops accepting connections and waits for in-flight requests until
// ctx expires.
func (e engine) Shutdown(ctx context.Context) error {
	return e.srv.Shutdown(ctx)
}

func (e engine) NoRoute(handlers ...contract.Chainable) {
	e.gin.NoRoute(chain(handlers)...)
}

// TrustProxies maps onto gin's trusted-proxy configuration, which is what
// ClientIP consults before believing a forwarded header.
func (e engine) TrustProxies(cidrs []string) error {
	return e.gin.SetTrustedProxies(cidrs)
}

// ServeAssets mounts gin-contrib/static. contract.AssetFS is method-identical
// to static.ServeFileSystem, so the value passes straight through.
func (e engine) ServeAssets(prefix string, fs contract.AssetFS) {
	e.gin.Use(static.Serve(prefix, fs))
}

// UseRequestLog mounts gin's access logger, mapping gin's per-request log
// parameters onto contract.RequestLog so the caller owns the line format.
func (e engine) UseRequestLog(format func(contract.RequestLog) string) {
	e.gin.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return format(contract.RequestLog{
			Timestamp:  param.TimeStamp,
			StatusCode: param.StatusCode,
			Latency:    param.Latency,
			ClientIP:   param.ClientIP,
			Method:     param.Method,
			Path:       param.Path,
			Values:     param.Keys,
		})
	}))
}
