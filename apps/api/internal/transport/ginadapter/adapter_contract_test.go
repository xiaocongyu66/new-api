package ginadapter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/contract/conformance"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGinAdapterSatisfiesTransportContract runs the adapter-agnostic conformance
// suite against this adapter. The behavioural assertions live in the conformance
// package so a second adapter proves equivalence by supplying a factory rather
// than by copying test bodies, which is how two implementations would silently
// diverge.
func TestGinAdapterSatisfiesTransportContract(t *testing.T) {
	gin.SetMode(gin.TestMode)

	conformance.Run(t, conformance.Adapter{
		Name:              "gin",
		NewContext:        NewSyntheticContext,
		ServeRoute:        serveRoute,
		NewEventStream:    EventStream,
		NewResponseStream: ResponseStream,
	})
}

// serveRoute registers route on a real gin engine and serves req through it, so
// the cases that need router-matched parameters and real middleware
// continuation exercise the framework rather than a synthetic context.
func serveRoute(t *testing.T, route conformance.Route, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)

	engine := gin.New()
	handlers := make([]gin.HandlerFunc, 0, len(route.Middleware)+1)
	for _, m := range route.Middleware {
		handlers = append(handlers, Middleware(m))
	}
	handlers = append(handlers, Handler(route.Handler))
	engine.Handle(route.Method, route.Pattern, handlers...)

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder
}

// TestUnwrapRecoversGinContext asserts the migration escape hatch returns the
// original context, and reports failure for a foreign implementation. It stays
// gin-specific by nature: recovering the framework context is exactly the part
// that cannot be adapter-agnostic.
func TestUnwrapRecoversGinContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	adapted := Wrap(ginCtx)

	recovered, ok := Unwrap(adapted)
	require.True(t, ok)
	assert.Same(t, ginCtx, recovered)
}

// TestUnwrapRejectsForeignContext asserts the escape hatch reports failure
// instead of returning a zero context, so a caller left behind by a framework
// swap fails loudly rather than nil-panicking later.
func TestUnwrapRejectsForeignContext(t *testing.T) {
	recovered, ok := Unwrap(foreignContext{})

	assert.False(t, ok)
	assert.Nil(t, recovered)
}

// foreignContext stands in for a non-gin contract implementation, the way a
// future fiberadapter context would arrive here. It is only ever passed to
// Unwrap, so the embedded methods are never called.
//
// The embedding goes through an alias because embedding contract.Context
// directly would name the field "Context", shadowing the promoted Context()
// method the interface itself requires.
type foreignContext struct{ transportContext }

type transportContext = contract.Context
