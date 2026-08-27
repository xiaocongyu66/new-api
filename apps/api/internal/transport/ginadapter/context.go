// Package ginadapter implements the framework-neutral transport contract on top
// of Gin. It is the only package that may reference gin types while the
// migration is in progress; replacing the HTTP framework means adding a sibling
// adapter, not changing business code.
package ginadapter

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/gin-gonic/gin"
)

// requestContext adapts *gin.Context to contract.Context.
type requestContext struct {
	gin *gin.Context
}

// Wrap adapts a gin context to the transport contract. A context without an
// inbound request (tests built via gin.CreateTestContext) gets a default one
// so request accessors never nil-panic.
func Wrap(c *gin.Context) contract.Context {
	if c.Request == nil {
		c.Request = httptest.NewRequest("GET", "/", nil)
	}
	return &requestContext{gin: c}
}

// Unwrap returns the underlying gin context. It exists for incremental
// migration: code still requiring gin-specific behaviour can recover the
// concrete context instead of widening the contract. New code must not use it.
func Unwrap(c contract.Context) (*gin.Context, bool) {
	adapted, ok := c.(*requestContext)
	if !ok {
		return nil, false
	}
	return adapted.gin, true
}

// Handler adapts a contract handler to a gin handler so routes can be
// registered without business code importing gin.
func Handler(handler contract.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		handler(Wrap(c))
	}
}

// ---- Values ----

func (r *requestContext) Set(key string, value any) { r.gin.Set(key, value) }

func (r *requestContext) Get(key string) (any, bool) { return r.gin.Get(key) }

func (r *requestContext) GetString(key string) string { return r.gin.GetString(key) }

func (r *requestContext) GetInt(key string) int { return r.gin.GetInt(key) }

func (r *requestContext) GetInt64(key string) int64 { return r.gin.GetInt64(key) }

func (r *requestContext) GetBool(key string) bool { return r.gin.GetBool(key) }

func (r *requestContext) GetStringMap(key string) map[string]any {
	return r.gin.GetStringMap(key)
}

func (r *requestContext) GetStringSlice(key string) []string {
	return r.gin.GetStringSlice(key)
}

func (r *requestContext) GetTime(key string) time.Time { return r.gin.GetTime(key) }

// ---- Request ----

func (r *requestContext) Method() string { return r.gin.Request.Method }

func (r *requestContext) Path() string { return r.gin.Request.URL.Path }

func (r *requestContext) FullPath() string { return r.gin.FullPath() }

func (r *requestContext) ClientIP() string { return r.gin.ClientIP() }

func (r *requestContext) UserAgent() string { return r.gin.Request.UserAgent() }

func (r *requestContext) ContentType() string { return r.gin.ContentType() }

func (r *requestContext) ContentLength() int64 { return r.gin.Request.ContentLength }

func (r *requestContext) RequestURI() string { return r.gin.Request.RequestURI }

func (r *requestContext) RawQuery() string { return r.gin.Request.URL.RawQuery }

func (r *requestContext) ParseForm() error { return r.gin.Request.ParseForm() }

func (r *requestContext) PostFormValues() map[string][]string {
	return r.gin.Request.PostForm
}

func (r *requestContext) Query(key string) string { return r.gin.Query(key) }

func (r *requestContext) DefaultQuery(key, fallback string) string {
	return r.gin.DefaultQuery(key, fallback)
}

func (r *requestContext) QueryValues() map[string][]string {
	return r.gin.Request.URL.Query()
}

func (r *requestContext) Param(key string) string { return r.gin.Param(key) }

func (r *requestContext) Params() map[string]string {
	params := make(map[string]string, len(r.gin.Params))
	for _, p := range r.gin.Params {
		params[p.Key] = p.Value
	}
	return params
}

func (r *requestContext) Header(key string) string { return r.gin.GetHeader(key) }

func (r *requestContext) Headers() http.Header { return r.gin.Request.Header }

func (r *requestContext) Cookie(name string) (string, error) { return r.gin.Cookie(name) }

// BindJSON decodes through the shared replayable-body helper so the body stays
// readable for the outbound relay request.
func (r *requestContext) BindJSON(target any) error {
	return common.UnmarshalBodyReusable(r, target)
}

func (r *requestContext) RawBody() ([]byte, error) {
	storage, err := common.GetBodyStorage(r)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

func (r *requestContext) BodyReader() (io.ReadCloser, error) {
	storage, err := common.GetBodyStorage(r)
	if err != nil {
		return nil, err
	}
	return storage.NewReader()
}

func (r *requestContext) MultipartForm() (*multipart.Form, error) {
	return common.ParseMultipartFormReusable(r)
}

func (r *requestContext) PostForm(key string) string { return r.gin.PostForm(key) }

func (r *requestContext) HTTPRequest() *http.Request { return r.gin.Request }

// ReplaceBody rewrites the inbound body and clears the cached body storage so
// subsequent reads observe the new payload.
func (r *requestContext) ReplaceBody(payload []byte) {
	r.gin.Request.Body = io.NopCloser(bytes.NewReader(payload))
	r.gin.Request.ContentLength = int64(len(payload))
	common.CleanupBodyStorage(r)
}

func (r *requestContext) SetPath(path string) {
	r.gin.Request.URL.Path = path
}

func (r *requestContext) ResponseWriter() http.ResponseWriter { return r.gin.Writer }

func (r *requestContext) ResetBody(body io.ReadCloser) {
	r.gin.Request.Body = body
}

func (r *requestContext) SetMethod(method string) {
	r.gin.Request.Method = method
}

func (r *requestContext) SetContextValue(key, value any) {
	r.gin.Request = r.gin.Request.WithContext(context.WithValue(r.gin.Request.Context(), key, value))
}

func (r *requestContext) Context() context.Context { return r.gin.Request.Context() }

// ---- Response ----

func (r *requestContext) JSON(status int, payload any) error {
	r.gin.JSON(status, payload)
	return nil
}

func (r *requestContext) Data(status int, contentType string, payload []byte) error {
	r.gin.Data(status, contentType, payload)
	return nil
}

func (r *requestContext) String(status int, value string) error {
	r.gin.String(status, value)
	return nil
}

func (r *requestContext) Redirect(status int, location string) {
	r.gin.Redirect(status, location)
}

func (r *requestContext) Status(status int) { r.gin.Status(status) }

func (r *requestContext) ResponseStatus() int { return r.gin.Writer.Status() }

// CaptureResponse swaps in a writer that mirrors the response into a bounded
// buffer. The framework writer stays in place for the actual response.
func (r *requestContext) CaptureResponse(maxBytes int) contract.ResponseCapture {
	capture := &responseCapture{
		ResponseWriter: r.gin.Writer,
		body:           bytes.NewBuffer(nil),
		maxSize:        maxBytes,
	}
	r.gin.Writer = capture
	return capture
}

// responseCapture mirrors written bytes into a bounded buffer so audit code can
// inspect the payload without re-reading the response.
type responseCapture struct {
	gin.ResponseWriter
	body    *bytes.Buffer
	maxSize int
}

func (w *responseCapture) Write(b []byte) (int, error) {
	if w.body.Len() < w.maxSize {
		if remain := w.maxSize - w.body.Len(); remain >= len(b) {
			w.body.Write(b)
		} else {
			w.body.Write(b[:remain])
		}
	}
	return w.ResponseWriter.Write(b)
}

func (w *responseCapture) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *responseCapture) Body() []byte { return w.body.Bytes() }

func (r *requestContext) SetHeader(key, value string) { r.gin.Header(key, value) }

func (r *requestContext) SetCookie(cookie *http.Cookie) {
	http.SetCookie(r.gin.Writer, cookie)
}

// ---- Chain ----

func (r *requestContext) Next() { r.gin.Next() }

func (r *requestContext) Abort() { r.gin.Abort() }

func (r *requestContext) IsAborted() bool { return r.gin.IsAborted() }

func (r *requestContext) AbortWithStatus(status int) { r.gin.AbortWithStatus(status) }

func (r *requestContext) AbortWithStatusJSON(status int, payload any) {
	r.gin.AbortWithStatusJSON(status, payload)
}
