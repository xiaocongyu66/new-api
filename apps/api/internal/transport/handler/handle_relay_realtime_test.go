package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/gateway"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/QuantumNous/new-api/relaykit/types"

	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRelayRealtimeSurvivesAFailedUpgrade is the regression pin for the nil
// interface bug, asserted where it would actually have shipped.
//
// Relay's realtime branch upgrades first and writes an error frame if the
// handshake fails. The error frame goes through gateway.WssError, which guards on
// `ws == nil` — so the whole guard depends on the failed upgrade producing a true
// nil interface. Widening a failed upgrade's concrete conn into the interface
// instead yields a non-nil interface holding a nil pointer, the guard evaluates
// false, and the nil pointer is dereferenced: every failed handshake becomes a
// panic rather than an error response.
//
// It runs on both adapters because the trap has a different shape in each. The
// gin path returns gorilla's `(*Conn, error)` pair directly, so the mistake is
// assigning before checking the error; the fiber path answers a parked chain over
// a channel, so the mistake is populating the conn field on the failure path.
// Neither is observable without actually failing an upgrade.
func TestRelayRealtimeSurvivesAFailedUpgrade(t *testing.T) {
	for _, adapter := range []struct {
		name    string
		context func() contract.Context
	}{
		{
			name: "gin",
			context: func() contract.Context {
				// A recorder is not an http.Hijacker, so the upgrade fails the
				// way it does for a request that is not a handshake.
				adapted, _ := ginadapter.NewSyntheticContext(
					httptest.NewRequest(http.MethodGet, "/v1/realtime", nil))
				return adapted
			},
		},
		{
			name: "fiber",
			context: func() contract.Context {
				// An in-process context has no connection to hijack, which is
				// the fiber adapter's own failure path.
				adapted, _ := fiberadapter.NewSyntheticContext(
					httptest.NewRequest(http.MethodGet, "/v1/realtime", nil))
				return adapted
			},
		},
	} {
		t.Run(adapter.name, func(t *testing.T) {
			c := adapter.context()

			// The upgrade must fail here; if it ever starts succeeding this case
			// would stop covering the failure path silently.
			conn, err := c.UpgradeWebSocket("realtime")
			require.Error(t, err, "this context must not be upgradable")
			require.True(t, conn == nil,
				"a failed upgrade must produce a true nil interface")

			// The whole point: Relay's realtime branch must return an error
			// rather than panic.
			assert.NotPanics(t, func() {
				Relay(adapter.context(), types.RelayFormatOpenAIRealtime)
			}, "a failed realtime handshake must not panic")
		})
	}
}

// TestWssErrorGuardsOnATrueNilOnly demonstrates the failure mode the guard cannot
// defend against, which is why the upgrade contract promises a true nil rather
// than leaving it to callers.
//
// A true nil interface is refused safely. An interface holding a nil
// *gorilla.Conn passes `ws == nil` and panics on the write — so this pins that
// the guard is only sound because no adapter is allowed to produce that value.
func TestWssErrorGuardsOnATrueNilOnly(t *testing.T) {
	c, _ := ginadapter.NewSyntheticContext(httptest.NewRequest(http.MethodGet, "/v1/realtime", nil))
	openaiError := types.NewError(assert.AnError, types.ErrorCodeGetChannelFailed).ToOpenAIError()

	var trueNil relaycommon.WSConn
	assert.NotPanics(t, func() {
		gateway.WssError(c, trueNil, openaiError)
	}, "a true nil interface must be refused rather than dereferenced")

	// The value no adapter may return. It is asserted as a panic rather than
	// avoided, because that is the evidence for why UpgradeWebSocket documents
	// the nil guarantee instead of trusting each call site to get it right.
	nilPointerInInterface := relaycommon.WSConn((*gorilla.Conn)(nil))
	require.False(t, nilPointerInInterface == nil,
		"an interface holding a nil pointer is not nil: this is the trap")
	assert.Panics(t, func() {
		gateway.WssError(c, nilPointerInInterface, openaiError)
	}, "the ws == nil guard cannot catch a nil pointer inside an interface")
}
