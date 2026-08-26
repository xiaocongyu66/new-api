package ginadapter

import (
	"net/http"
	"net/http/httptest"

	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/gin-gonic/gin"
)

// SetReleaseMode switches the framework to production mode. It exists so
// process startup does not import gin to make that choice.
func SetReleaseMode() {
	gin.SetMode(gin.ReleaseMode)
}

// NewEngine builds the HTTP engine with panic recovery installed. onPanic
// receives the request context and the recovered value; returning normally
// completes the response.
func NewEngine(onPanic func(c contract.Context, recovered any)) *gin.Engine {
	engine := gin.New()
	engine.Use(gin.CustomRecovery(func(c *gin.Context, recovered any) {
		onPanic(Wrap(c), recovered)
	}))
	return engine
}

// NewSyntheticContext builds a contract context backed by an in-process
// recorder rather than a client connection. Internal callers that exercise the
// relay pipeline without an inbound request (channel testing, scheduled probes)
// use it instead of constructing a framework context directly.
//
// The returned recorder captures whatever the pipeline writes.
func NewSyntheticContext(req *http.Request) (contract.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = req
	if c.Request == nil {
		c.Request = httptest.NewRequest("GET", "/", nil)
	}
	return Wrap(c), recorder
}

// ReplaceRequest swaps the inbound request on a synthetic context. It is only
// valid for contexts created by NewSyntheticContext.
func ReplaceRequest(c contract.Context, req *http.Request) bool {
	adapted, ok := c.(*requestContext)
	if !ok {
		return false
	}
	adapted.gin.Request = req
	return true
}

// Middleware adapts a contract middleware to a gin handler so route
// registration does not require business code to import gin.
func Middleware(m contract.Middleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		m(Wrap(c))
	}
}

// MustUnwrap recovers the gin context behind c. It exists for the migration
// window where a migrated caller must hand control to code that has not been
// migrated yet (relay providers, protocol adaptors). It panics when c did not
// originate from this adapter, which would mean a framework swap left a caller
// behind.
func MustUnwrap(c contract.Context) *gin.Context {
	ginCtx, ok := Unwrap(c)
	if !ok {
		panic("ginadapter: context did not originate from this adapter")
	}
	return ginCtx
}
