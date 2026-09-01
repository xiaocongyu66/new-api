package contract

import (
	"context"
	"net/http"
	"time"
)

// Chainable is a handler or middleware mounted on a route or a group. It is the
// bare func type rather than Handler because Handler and Middleware are both
// declared as func(Context): naming the bare type here lets a value of either
// pass without a conversion at every one of the several hundred registration
// sites, while the parameter still documents what is accepted.
type Chainable = func(Context)

// Routes registers request handlers under a path prefix.
//
// Registration order is observable: Use mounts middleware for the routes
// registered after it, and a group inherits whatever was mounted on its parent
// before the group was created. Implementations must preserve that ordering,
// because the middleware chain per route is part of the transport's behaviour.
type Routes interface {
	// Group returns a child scope prefixed with path that inherits the
	// middleware mounted on this scope so far.
	Group(path string) Routes
	// Use mounts middleware for the routes registered after this call.
	Use(chain ...Chainable)
	// Handle registers a route whose method is carried as data, for the
	// permission tables that pair a method with a handler.
	Handle(method, path string, chain ...Chainable)

	GET(path string, chain ...Chainable)
	POST(path string, chain ...Chainable)
	PUT(path string, chain ...Chainable)
	PATCH(path string, chain ...Chainable)
	DELETE(path string, chain ...Chainable)
	// Any registers the route for every method the transport routes.
	Any(path string, chain ...Chainable)

	// UseCORS mounts the cross-origin policy for this scope.
	//
	// It is a named capability rather than a Middleware value because the
	// policy comes from a transport-specific library that produces the
	// transport's own handler type; business code cannot construct one without
	// importing the framework, which is what this contract exists to prevent.
	UseCORS()
	// UseCompression mounts response compression at the compression library's
	// default level, for the same reason as UseCORS.
	UseCompression()
}

// Engine is the whole server: the root route scope, the capabilities that
// configure the transport itself rather than a single route, and the process
// lifecycle. It deliberately does NOT embed http.Handler: fasthttp-backed
// engines have no ServeHTTP, and satisfying it through an adaptor would make
// Flush a no-op and drop Hijack, silently breaking every SSE stream and the
// realtime upgrade.
type Engine interface {
	Routes
	// NoRoute installs the fallback invoked when no registered route matches.
	NoRoute(chain ...Chainable)
	// TrustProxies declares which peer addresses may set forwarded headers, so
	// ClientIP believes a forwarded chain only when it arrives from one of
	// them. A nil or empty list trusts no proxy, making the peer address
	// authoritative. This is engine configuration rather than middleware: it
	// changes how an existing accessor resolves, so it cannot be expressed as
	// a handler in the chain.
	TrustProxies(cidrs []string) error
	// ServeAssets serves fs under prefix. A path fs does not hold falls
	// through to the NoRoute fallback rather than being answered here, so a
	// single-page application can serve its index for client-side routes.
	ServeAssets(prefix string, fs AssetFS)
	// UseRequestLog mounts access logging, rendering each completed request
	// with format. The caller owns the line format; the transport owns when
	// the line is emitted and how the fields are measured.
	UseRequestLog(format func(RequestLog) string)

	// Serve listens on addr and blocks until the engine stops. A graceful
	// Shutdown makes it return without error.
	Serve(addr string) error
	// Shutdown stops accepting connections and waits for in-flight requests,
	// giving up when ctx expires. SSE streams can run for minutes, so the
	// caller's timeout is what bounds the wait.
	Shutdown(ctx context.Context) error
}

// AssetFS is the file system asset serving reads from: an http.FileSystem plus
// an existence probe, so a miss falls through to the NoRoute fallback instead
// of being answered from here.
type AssetFS interface {
	http.FileSystem
	Exists(prefix string, path string) bool
}

// RequestLog is one completed request as the access log observes it.
type RequestLog struct {
	Timestamp  time.Time
	StatusCode int
	Latency    time.Duration
	ClientIP   string
	Method     string
	Path       string
	// Values is the per-request state handlers and middleware set, so a log
	// line can carry the request id and the route tag.
	Values map[string]any
}
