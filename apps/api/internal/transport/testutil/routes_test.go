package testutil_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/testutil"

	"github.com/stretchr/testify/require"
)

func TestServeBufferedRouteDispatchesParamsMiddlewareAndResponse(t *testing.T) {
	trace := make([]string, 0, 3)
	response := testutil.ServeBufferedRoute(
		t,
		http.MethodGet,
		"/widgets/:id",
		[]contract.Middleware{func(c contract.Context) {
			trace = append(trace, "before")
			c.Next()
			trace = append(trace, "after")
		}},
		func(c contract.Context) {
			trace = append(trace, "handler:"+c.Param("id"))
			c.SetHeader("X-Route-Fixture", "dispatch")
			require.NoError(t, c.String(http.StatusCreated, "created"))
		},
		httptest.NewRequest(http.MethodGet, "/widgets/42", nil),
	)

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, []string{"before", "handler:42", "after"}, trace)
	require.Equal(t, http.StatusCreated, response.StatusCode)
	require.Equal(t, "dispatch", response.Header.Get("X-Route-Fixture"))
	require.Equal(t, "created", string(body))
}
