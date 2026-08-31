package conformance

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runChainCases(t *testing.T, adapter Adapter) {
	t.Helper()

	// AbortStopsHandler asserts middleware written against the contract can
	// reject a request, matching the current abort behaviour.
	t.Run("AbortStopsHandler", func(t *testing.T) {
		handlerRan := false
		route := Route{
			Method:  http.MethodGet,
			Pattern: "/api/user/self",
			Middleware: []contract.Middleware{
				func(c contract.Context) {
					require.NoError(t, c.JSON(http.StatusUnauthorized, map[string]any{"success": false}))
					c.Abort()
				},
			},
			Handler: func(contract.Context) { handlerRan = true },
		}

		recorder := adapter.ServeRoute(t, route, httptest.NewRequest(http.MethodGet, "/api/user/self", nil))

		assert.False(t, handlerRan, "aborted request must not reach the handler")
		assert.Equal(t, http.StatusUnauthorized, recorder.Code)
		assert.JSONEq(t, `{"success":false}`, recorder.Body.String())
	})

	// RunsContractHandler asserts a contract handler registered on a route
	// receives a working context.
	t.Run("RunsContractHandler", func(t *testing.T) {
		route := Route{
			Method:  http.MethodGet,
			Pattern: "/api/token/:id",
			Handler: func(c contract.Context) {
				require.NoError(t, c.JSON(http.StatusOK, map[string]any{"id": c.Param("id")}))
			},
		}

		recorder := adapter.ServeRoute(t, route, httptest.NewRequest(http.MethodGet, "/api/token/31", nil))

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.JSONEq(t, `{"id":"31"}`, recorder.Body.String())
	})

	// NextRunsDownstreamThenReturns asserts Next is synchronous and returns
	// after the rest of the chain. Billing and logging middleware do their work
	// after Next, so a Next that returned early would bill before the response
	// existed.
	t.Run("NextRunsDownstreamThenReturns", func(t *testing.T) {
		var order []string
		route := Route{
			Method:  http.MethodGet,
			Pattern: "/api/log",
			Middleware: []contract.Middleware{
				func(c contract.Context) {
					order = append(order, "before")
					c.Next()
					order = append(order, "after")
				},
			},
			Handler: func(c contract.Context) {
				order = append(order, "handler")
				require.NoError(t, c.JSON(http.StatusOK, map[string]any{"success": true}))
			},
		}

		recorder := adapter.ServeRoute(t, route, httptest.NewRequest(http.MethodGet, "/api/log", nil))

		assert.Equal(t, []string{"before", "handler", "after"}, order)
		assert.Equal(t, http.StatusOK, recorder.Code)
	})

	// ResponseStatusIsReadableAfterNext asserts post-Next middleware observes
	// the status the handler wrote, which is how request logging classifies the
	// outcome.
	t.Run("ResponseStatusIsReadableAfterNext", func(t *testing.T) {
		observed := 0
		route := Route{
			Method:  http.MethodGet,
			Pattern: "/api/log",
			Middleware: []contract.Middleware{
				func(c contract.Context) {
					c.Next()
					observed = c.ResponseStatus()
				},
			},
			Handler: func(c contract.Context) {
				require.NoError(t, c.JSON(http.StatusCreated, map[string]any{"success": true}))
			},
		}

		adapter.ServeRoute(t, route, httptest.NewRequest(http.MethodGet, "/api/log", nil))

		assert.Equal(t, http.StatusCreated, observed)
	})

	// IsAbortedReportsChainState asserts the flag flips only after Abort, so
	// middleware that guards on it does not skip work on a live request.
	t.Run("IsAbortedReportsChainState", func(t *testing.T) {
		var beforeAbort, afterAbort bool
		route := Route{
			Method:  http.MethodGet,
			Pattern: "/api/user/self",
			Middleware: []contract.Middleware{
				func(c contract.Context) {
					beforeAbort = c.IsAborted()
					c.Abort()
					afterAbort = c.IsAborted()
				},
			},
			Handler: func(contract.Context) {},
		}

		adapter.ServeRoute(t, route, httptest.NewRequest(http.MethodGet, "/api/user/self", nil))

		assert.False(t, beforeAbort)
		assert.True(t, afterAbort)
	})

	// AbortWithStatusStopsChain covers the status-only rejection used by the
	// rate limiter and the body-size guard.
	t.Run("AbortWithStatusStopsChain", func(t *testing.T) {
		handlerRan := false
		route := Route{
			Method:  http.MethodPost,
			Pattern: "/v1/chat/completions",
			Middleware: []contract.Middleware{
				func(c contract.Context) { c.AbortWithStatus(http.StatusTooManyRequests) },
			},
			Handler: func(contract.Context) { handlerRan = true },
		}

		recorder := adapter.ServeRoute(t, route, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

		assert.False(t, handlerRan)
		assert.Equal(t, http.StatusTooManyRequests, recorder.Code)
	})

	// AbortWithStatusJSONStopsChainAndWritesBody covers the rejection path that
	// returns a structured error, which is what API clients parse.
	t.Run("AbortWithStatusJSONStopsChainAndWritesBody", func(t *testing.T) {
		handlerRan := false
		route := Route{
			Method:  http.MethodPost,
			Pattern: "/v1/chat/completions",
			Middleware: []contract.Middleware{
				func(c contract.Context) {
					c.AbortWithStatusJSON(http.StatusForbidden, map[string]any{
						"success": false,
						"message": "no permission",
					})
				},
			},
			Handler: func(contract.Context) { handlerRan = true },
		}

		recorder := adapter.ServeRoute(t, route, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

		assert.False(t, handlerRan)
		assert.Equal(t, http.StatusForbidden, recorder.Code)
		assert.JSONEq(t, `{"success":false,"message":"no permission"}`, recorder.Body.String())
	})
}
