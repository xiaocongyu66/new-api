package ginadapter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/gin-gonic/gin"
	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpgradeCompletesTheHandshake asserts the gin side of the upgrade
// capability negotiates the subprotocol the realtime route depends on.
//
// It runs against a real server rather than a recorder because
// httptest.ResponseRecorder is not an http.Hijacker, so a recorder-backed
// assertion could only ever observe the failure path.
func TestUpgradeCompletesTheHandshake(t *testing.T) {
	echoed := make(chan string, 1)

	engine := gin.New()
	engine.GET("/v1/realtime", Handler(func(ctx contract.Context) {
		conn, err := ctx.UpgradeWebSocket("realtime")
		require.NoError(t, err)
		require.NotNil(t, conn)
		defer func() { _ = conn.Close() }()

		messageType, payload, err := conn.ReadMessage()
		require.NoError(t, err)
		assert.Equal(t, gorilla.TextMessage, messageType)
		require.NoError(t, conn.WriteMessage(gorilla.TextMessage, payload))
		echoed <- string(payload)
	}))

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	dialer := gorilla.Dialer{
		Subprotocols:     []string{"realtime"},
		HandshakeTimeout: 10 * time.Second,
	}
	conn, response, err := dialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1)+"/v1/realtime", nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	assert.Equal(t, http.StatusSwitchingProtocols, response.StatusCode)
	assert.Equal(t, "realtime", response.Header.Get("Sec-WebSocket-Protocol"))
	assert.Equal(t, "realtime", conn.Subprotocol())

	require.NoError(t, conn.WriteMessage(gorilla.TextMessage, []byte(`{"type":"session.update"}`)))

	select {
	case message := <-echoed:
		assert.Equal(t, `{"type":"session.update"}`, message)
	case <-time.After(5 * time.Second):
		t.Fatal("the upgraded handler never observed its frame")
	}
}

// TestFailedUpgradeReturnsATrueNilInterface is the W1-C regression pin on the gin
// side.
//
// Returning gorilla's *websocket.Conn directly would produce a non-nil interface
// holding a nil pointer on failure, which passes the `ws == nil` guard in
// gateway.WssError and then dereferences nil, turning every failed handshake into
// a panic instead of an error response.
func TestFailedUpgradeReturnsATrueNilInterface(t *testing.T) {
	type outcome struct {
		conn  contract.WSConn
		isNil bool
		err   error
	}
	observed := make(chan outcome, 1)

	engine := gin.New()
	engine.GET("/v1/realtime", Handler(func(ctx contract.Context) {
		conn, err := ctx.UpgradeWebSocket("realtime")
		observed <- outcome{conn: conn, isNil: conn == nil, err: err}
	}))

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	response, err := http.Get(server.URL + "/v1/realtime")
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })

	select {
	case result := <-observed:
		require.Error(t, result.err)
		assert.True(t, result.isNil,
			"a failed upgrade must return a true nil interface, not one holding a nil pointer")
		assert.Nil(t, result.conn)
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never observed the upgrade result")
	}

	assert.NotEqual(t, http.StatusSwitchingProtocols, response.StatusCode)
}
