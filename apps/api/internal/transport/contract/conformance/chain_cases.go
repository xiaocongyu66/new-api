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

	// The multi-middleware abort cases run here rather than as their own family,
	// so adding them needs no change to the Adapter contract every adapter
	// implements.
	runAbortSemanticsCases(t, adapter)

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
		assert.Equal(t, http.StatusUnauthorized, recorder.Status())
		assert.JSONEq(t, `{"success":false}`, string(recorder.Body()))
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

		assert.Equal(t, http.StatusOK, recorder.Status())
		assert.JSONEq(t, `{"id":"31"}`, string(recorder.Body()))
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
		assert.Equal(t, http.StatusOK, recorder.Status())
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
		assert.Equal(t, http.StatusTooManyRequests, recorder.Status())
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
		assert.Equal(t, http.StatusForbidden, recorder.Status())
		assert.JSONEq(t, `{"success":false,"message":"no permission"}`, string(recorder.Body()))
	})
}

// runAbortSemanticsCases pins what Abort means across more than one middleware.
//
// The seven cases above all use a single middleware, which cannot distinguish
// "the chain stopped" from "this middleware returned". That distinction is the
// whole of the abort contract, and it is exactly where a transport whose own
// continuation is inverted (returning without continuing ends the chain, so
// returning normally would read as an abort) would silently invert every
// authorising middleware in the process.
func runAbortSemanticsCases(t *testing.T, adapter Adapter) {
	t.Helper()

	// AbortInsideMiddlewareSkipsRemainingMiddlewareToo asserts an abort stops the
	// whole rest of the chain, not just the handler.
	//
	// This is the security-relevant shape: authentication runs before rate
	// limiting, quota checks, and billing. A transport that skipped only the
	// handler would run every later middleware on an unauthenticated request,
	// with the middleware believing auth had passed because it was reached.
	t.Run("AbortInsideMiddlewareSkipsRemainingMiddlewareToo", func(t *testing.T) {
		var reached []string
		route := Route{
			Method:  http.MethodPost,
			Pattern: "/v1/chat/completions",
			Middleware: []contract.Middleware{
				func(c contract.Context) {
					reached = append(reached, "first")
					c.Next()
					reached = append(reached, "first-after")
				},
				func(c contract.Context) {
					reached = append(reached, "authorising")
					c.AbortWithStatusJSON(http.StatusUnauthorized, map[string]any{"success": false})
				},
				func(c contract.Context) {
					reached = append(reached, "must-not-run")
					c.Next()
				},
			},
			Handler: func(contract.Context) { reached = append(reached, "handler") },
		}

		recorder := adapter.ServeRoute(t, route, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

		assert.Equal(t, []string{"first", "authorising", "first-after"}, reached,
			"an abort must skip every later middleware and the handler, while still unwinding the ones already entered")
		assert.Equal(t, http.StatusUnauthorized, recorder.Status())
		assert.JSONEq(t, `{"success":false}`, string(recorder.Body()))
	})

	// AbortAfterNextDoesNotResurrectTheChain asserts a middleware that aborts on
	// the way back out does not cause anything to re-run.
	//
	// Post-Next aborts are real: response-inspecting middleware decides the
	// outcome was unacceptable after seeing it. The chain is already unwinding at
	// that point, so the only correct effect is on the flag.
	t.Run("AbortAfterNextDoesNotResurrectTheChain", func(t *testing.T) {
		var order []string
		handlerRuns := 0
		route := Route{
			Method:  http.MethodGet,
			Pattern: "/api/log",
			Middleware: []contract.Middleware{
				func(c contract.Context) {
					order = append(order, "outer-before")
					c.Next()
					order = append(order, "outer-after")
					c.Abort()
					order = append(order, "outer-aborted")
				},
				func(c contract.Context) {
					order = append(order, "inner-before")
					c.Next()
					order = append(order, "inner-after")
				},
			},
			Handler: func(c contract.Context) {
				handlerRuns++
				require.NoError(t, c.JSON(http.StatusOK, map[string]any{"success": true}))
			},
		}

		recorder := adapter.ServeRoute(t, route, httptest.NewRequest(http.MethodGet, "/api/log", nil))

		assert.Equal(t, 1, handlerRuns, "aborting after Next must not re-run the handler")
		assert.Equal(t,
			[]string{"outer-before", "inner-before", "inner-after", "outer-after", "outer-aborted"},
			order,
			"an abort while unwinding must not restart or re-enter the chain")
		assert.Equal(t, http.StatusOK, recorder.Status(),
			"a response already written before the abort still reaches the client")
	})

	// NextAfterAbortIsANoOp asserts calling Next on an aborted chain does not
	// continue it.
	//
	// Middleware calls Next unconditionally all over the codebase, including after
	// invoking a helper that may have aborted. If Next resumed an aborted chain,
	// every one of those sites would defeat the abort it just performed.
	t.Run("NextAfterAbortIsANoOp", func(t *testing.T) {
		handlerRan := false
		laterRan := false
		var abortedBefore, abortedAfter bool
		route := Route{
			Method:  http.MethodPost,
			Pattern: "/v1/chat/completions",
			Middleware: []contract.Middleware{
				func(c contract.Context) {
					c.AbortWithStatus(http.StatusTooManyRequests)
					abortedBefore = c.IsAborted()
					// The unconditional continuation an aborting middleware's
					// caller would perform.
					c.Next()
					abortedAfter = c.IsAborted()
				},
				func(c contract.Context) { laterRan = true },
			},
			Handler: func(contract.Context) { handlerRan = true },
		}

		recorder := adapter.ServeRoute(t, route, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil))

		assert.False(t, laterRan, "Next after Abort must not run later middleware")
		assert.False(t, handlerRan, "Next after Abort must not reach the handler")
		assert.True(t, abortedBefore, "the abort must be observable immediately")
		assert.True(t, abortedAfter, "a no-op Next must not clear the aborted flag")
		assert.Equal(t, http.StatusTooManyRequests, recorder.Status())
	})
}
