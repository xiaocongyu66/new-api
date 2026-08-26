// Package contract defines the framework-neutral HTTP boundary used by business
// code. Nothing in this package may import an HTTP framework: the Gin
// implementation lives in internal/transport/ginadapter, and a future Fiber
// implementation replaces only that adapter.
package contract

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// Values carries per-request state that middleware produces and handlers read
// (authenticated user id, selected channel, request id, and similar).
//
// Keys are plain strings so the adapter can map them onto whichever per-request
// storage the underlying framework provides.
type Values interface {
	Set(key string, value any)
	Get(key string) (any, bool)
	GetString(key string) string
	GetInt(key string) int
	GetInt64(key string) int64
	GetBool(key string) bool
	GetStringMap(key string) map[string]any
	GetTime(key string) time.Time
}

// Request exposes the inbound request. Body access goes through BodyStorage so
// the replayable-body semantics the relay pipeline depends on are preserved
// rather than reimplemented per framework.
type Request interface {
	Method() string
	Path() string
	FullPath() string
	ClientIP() string
	UserAgent() string
	ContentType() string

	Query(key string) string
	DefaultQuery(key, fallback string) string
	QueryValues() map[string][]string
	Param(key string) string

	Header(key string) string
	Headers() http.Header
	Cookie(name string) (string, error)

	// BindJSON decodes the request body into target. The body remains readable
	// afterwards so downstream relay code can forward it upstream.
	BindJSON(target any) error
	// RawBody returns the buffered request body.
	RawBody() ([]byte, error)
	// BodyReader returns an independent reader positioned at the body start.
	BodyReader() (io.ReadCloser, error)
	MultipartForm() (*multipart.Form, error)
	PostForm(key string) string

	// Context is the request lifetime. It is cancelled when the client
	// disconnects, which streaming code must observe.
	Context() context.Context
}

// Response writes a complete (non-streaming) response.
type Response interface {
	JSON(status int, payload any) error
	Data(status int, contentType string, payload []byte) error
	String(status int, value string) error
	Redirect(status int, location string)
	Status(status int)
	SetHeader(key, value string)
	SetCookie(cookie *http.Cookie)
}

// Chain controls middleware continuation.
type Chain interface {
	Next()
	Abort()
	IsAborted() bool
}

// Context is the single value handlers, middleware, and relay code accept in
// place of a framework context.
type Context interface {
	Values
	Request
	Response
	Chain
}

// Handler is a framework-neutral request handler.
type Handler func(Context)

// Middleware wraps request handling. Implementations call Context.Next to
// continue, or Context.Abort plus a Response write to reject.
type Middleware func(Context)
