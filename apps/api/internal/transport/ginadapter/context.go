// Package ginadapter implements the framework-neutral transport contract on top
// of Gin. It is the only package that may reference gin types while the
// migration is in progress; replacing the HTTP framework means adding a sibling
// adapter, not changing business code.
package ginadapter

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/gin-gonic/gin"
)

// requestContext adapts *gin.Context to contract.Context.
type requestContext struct {
	gin *gin.Context
}

// Wrap adapts a gin context to the transport contract.
func Wrap(c *gin.Context) contract.Context {
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

func (r *requestContext) GetTime(key string) time.Time { return r.gin.GetTime(key) }

// ---- Request ----

func (r *requestContext) Method() string { return r.gin.Request.Method }

func (r *requestContext) Path() string { return r.gin.Request.URL.Path }

func (r *requestContext) FullPath() string { return r.gin.FullPath() }

func (r *requestContext) ClientIP() string { return r.gin.ClientIP() }

func (r *requestContext) UserAgent() string { return r.gin.Request.UserAgent() }

func (r *requestContext) ContentType() string { return r.gin.ContentType() }

func (r *requestContext) Query(key string) string { return r.gin.Query(key) }

func (r *requestContext) DefaultQuery(key, fallback string) string {
	return r.gin.DefaultQuery(key, fallback)
}

func (r *requestContext) QueryValues() map[string][]string {
	return r.gin.Request.URL.Query()
}

func (r *requestContext) Param(key string) string { return r.gin.Param(key) }

func (r *requestContext) Header(key string) string { return r.gin.GetHeader(key) }

func (r *requestContext) Headers() http.Header { return r.gin.Request.Header }

func (r *requestContext) Cookie(name string) (string, error) { return r.gin.Cookie(name) }

// BindJSON decodes through the shared replayable-body helper so the body stays
// readable for the outbound relay request.
func (r *requestContext) BindJSON(target any) error {
	return common.UnmarshalBodyReusable(r.gin, target)
}

func (r *requestContext) RawBody() ([]byte, error) {
	storage, err := common.GetBodyStorage(r.gin)
	if err != nil {
		return nil, err
	}
	return storage.Bytes()
}

func (r *requestContext) BodyReader() (io.ReadCloser, error) {
	storage, err := common.GetBodyStorage(r.gin)
	if err != nil {
		return nil, err
	}
	return storage.NewReader()
}

func (r *requestContext) MultipartForm() (*multipart.Form, error) {
	return common.ParseMultipartFormReusable(r.gin)
}

func (r *requestContext) PostForm(key string) string { return r.gin.PostForm(key) }

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

func (r *requestContext) SetHeader(key, value string) { r.gin.Header(key, value) }

func (r *requestContext) SetCookie(cookie *http.Cookie) {
	http.SetCookie(r.gin.Writer, cookie)
}

// ---- Chain ----

func (r *requestContext) Next() { r.gin.Next() }

func (r *requestContext) Abort() { r.gin.Abort() }

func (r *requestContext) IsAborted() bool { return r.gin.IsAborted() }
