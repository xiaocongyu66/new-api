package common

import (
	"errors"
	"fmt"
	"testing"

	fastws "github.com/fasthttp/websocket"
	"github.com/gorilla/websocket"

	"github.com/stretchr/testify/assert"
)

// TestIsNormalWSCloseRecognisesBothLibraries is the case that matters, because
// the bug it pins is silent.
//
// The realtime relay holds two connections from different modules: after the
// Fiber cutover the client side is a fasthttp/websocket conn while the target
// dial stays gorilla. fasthttp's package is a gorilla fork, so *CloseError is
// structurally identical but a distinct Go type, and each library's IsCloseError
// type-asserts only its own. Using either library's own check for both would
// classify every clean disconnect on the other side as a relay error, so an
// ordinary client closing a realtime session would be logged and reported as a
// failure on every request.
func TestIsNormalWSCloseRecognisesBothLibraries(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		err      error
		expected bool
	}{
		{"gorilla normal closure", &websocket.CloseError{Code: websocket.CloseNormalClosure}, true},
		{"gorilla going away", &websocket.CloseError{Code: websocket.CloseGoingAway}, true},
		{"fasthttp normal closure", &fastws.CloseError{Code: fastws.CloseNormalClosure}, true},
		{"fasthttp going away", &fastws.CloseError{Code: fastws.CloseGoingAway}, true},

		{"gorilla abnormal closure", &websocket.CloseError{Code: websocket.CloseAbnormalClosure}, false},
		{"fasthttp abnormal closure", &fastws.CloseError{Code: fastws.CloseAbnormalClosure}, false},
		{"gorilla policy violation", &websocket.CloseError{Code: websocket.ClosePolicyViolation}, false},
		{"a plain transport error", errors.New("read tcp: connection reset by peer"), false},
		{"no error at all", nil, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, IsNormalWSClose(testCase.err))
		})
	}
}

// TestIsNormalWSCloseUnwrapsWrappedErrors asserts the check survives a wrapped
// error, since the read loops report failures through fmt.Errorf.
func TestIsNormalWSCloseUnwrapsWrappedErrors(t *testing.T) {
	assert.True(t, IsNormalWSClose(
		fmt.Errorf("reading from client: %w", &fastws.CloseError{Code: fastws.CloseNormalClosure})))
	assert.True(t, IsNormalWSClose(
		fmt.Errorf("reading from target: %w", &websocket.CloseError{Code: websocket.CloseGoingAway})))
	assert.False(t, IsNormalWSClose(
		fmt.Errorf("reading from target: %w", &websocket.CloseError{Code: websocket.CloseAbnormalClosure})))
}

// TestEachLibrarysOwnCheckMissesTheOthers documents why IsNormalWSClose is not
// redundant with either library's IsCloseError, so nobody "simplifies" it back
// to one of them.
func TestEachLibrarysOwnCheckMissesTheOthers(t *testing.T) {
	gorillaClose := error(&websocket.CloseError{Code: websocket.CloseNormalClosure})
	fastClose := error(&fastws.CloseError{Code: fastws.CloseNormalClosure})

	assert.False(t, websocket.IsCloseError(fastClose, websocket.CloseNormalClosure),
		"gorilla's check does not recognise a fasthttp close error")
	assert.False(t, fastws.IsCloseError(gorillaClose, fastws.CloseNormalClosure),
		"fasthttp's check does not recognise a gorilla close error")

	assert.True(t, IsNormalWSClose(gorillaClose))
	assert.True(t, IsNormalWSClose(fastClose))
}
