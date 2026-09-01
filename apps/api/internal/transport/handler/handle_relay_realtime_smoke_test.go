package handler

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/gofiber/fiber/v2"
	gorilla "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRealtimeEndToEndOnBothAdapters is the end-to-end smoke for GET
// /v1/realtime: it drives the real Relay entry point over a real socket on both
// adapters and asserts the client-visible behaviour agrees.
//
// It deliberately stops short of a provider. Reaching a live upstream needs a
// configured channel and a database, so what is verified here is everything up to
// that point, which is the part the framework swap can break: the handshake
// completes, the "realtime" subprotocol is negotiated, and the relay's failure is
// delivered as an SSE-style error *frame over the websocket* rather than as an
// HTTP response — because by the time the relay fails, the response has already
// been hijacked and an HTTP error would reach nobody.
//
// The unreachable-upstream frame is exactly what a client sees in production when
// no channel can serve the model, so the assertion is on real behaviour rather
// than on a stand-in.
// TestRealtimeCompleteSessionOnBothAdapters is the successful counterpart to
// TestRealtimeEndToEndOnBothAdapters. It uses the relay's real duplex handler
// with a deterministic websocket upstream rather than a database-selected
// channel: routing and billing are orthogonal to the adapter boundary.
func TestRealtimeCompleteSessionOnBothAdapters(t *testing.T) {
	const clientEvent = `{"event_id":"client-1","type":"session.update","session":{"instructions":"be concise"}}`
	const upstreamEvent = `{"event_id":"upstream-1","type":"session.updated","session":{"input_audio_format":"pcm16","output_audio_format":"pcm16"}}`

	upstream, received := realtimeUpstream(t, upstreamEvent)
	results := make(map[string]realtimeSessionResult, 2)

	t.Run("gin", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		engine.GET("/v1/realtime", ginadapter.Handler(realtimeRelay(upstream)))
		results["gin"] = runRealtimeSession(t, listenGin(t, engine), clientEvent, upstreamEvent)
	})

	t.Run("fiber", func(t *testing.T) {
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Add(fiber.MethodGet, "/v1/realtime", func(c *fiber.Ctx) error {
			return fiberadapter.Dispatch(c, []contract.Handler{realtimeRelay(upstream)})
		})
		results["fiber"] = runRealtimeSession(t, listenFiber(t, app), clientEvent, upstreamEvent)
	})

	require.Equal(t, clientEvent, <-received)
	require.Equal(t, clientEvent, <-received)
	assert.Equal(t, realtimeSessionResult{Subprotocol: "realtime", MessageType: gorilla.TextMessage, Payload: upstreamEvent, CloseCode: gorilla.CloseGoingAway}, results["gin"])
	assert.Equal(t, results["gin"], results["fiber"], "adapter choice must not change a complete realtime session")
}

type realtimeSessionResult struct {
	Subprotocol string
	MessageType int
	Payload     string
	CloseCode   int
}

func realtimeUpstream(t *testing.T, reply string) (*httptest.Server, <-chan string) {
	t.Helper()
	received := make(chan string, 2)
	upgrader := gorilla.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		messageType, payload, err := conn.ReadMessage()
		require.NoError(t, err)
		require.Equal(t, gorilla.TextMessage, messageType)
		received <- string(payload)
		require.NoError(t, conn.WriteMessage(gorilla.TextMessage, []byte(reply)))
		_, _, _ = conn.ReadMessage() // consume the relay's normal close reply.
	}))
	t.Cleanup(server.Close)
	return server, received
}

func realtimeRelay(upstream *httptest.Server) contract.Handler {
	return func(c contract.Context) {
		client, err := c.UpgradeWebSocket("realtime")
		if err != nil {
			return
		}
		defer client.Close()

		target, _, err := gorilla.DefaultDialer.Dial(strings.Replace(upstream.URL, "http://", "ws://", 1), nil)
		if err != nil {
			return
		}
		defer target.Close()

		apiErr, _ := openai.OpenaiRealtimeHandler(c, &relaycommon.RelayInfo{
			ClientWs:          client,
			TargetWs:          target,
			ChannelMeta:       &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o-realtime-preview"},
			InputAudioFormat:  "pcm16",
			OutputAudioFormat: "pcm16",
			IsFirstRequest:    true,
		})
		if apiErr != nil {
			return
		}
	}
}

func runRealtimeSession(t *testing.T, baseURL, clientEvent, expectedReply string) realtimeSessionResult {
	t.Helper()
	dialer := gorilla.Dialer{Subprotocols: []string{"realtime"}, HandshakeTimeout: 10 * time.Second}
	conn, response, err := dialer.Dial(strings.Replace(baseURL, "http://", "ws://", 1)+"/v1/realtime", nil)
	require.NoError(t, err)
	defer conn.Close()
	require.Equal(t, http.StatusSwitchingProtocols, response.StatusCode)
	require.Equal(t, "realtime", response.Header.Get("Sec-WebSocket-Protocol"))

	require.NoError(t, conn.WriteMessage(gorilla.TextMessage, []byte(clientEvent)))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(15*time.Second)))
	messageType, payload, err := conn.ReadMessage()
	require.NoError(t, err)
	require.NoError(t, conn.WriteControl(gorilla.CloseMessage, gorilla.FormatCloseMessage(gorilla.CloseGoingAway, "done"), time.Now().Add(5*time.Second)))
	_, _, err = conn.ReadMessage()
	var closeErr *gorilla.CloseError
	require.True(t, errors.As(err, &closeErr), "expected the server to acknowledge a close frame: %v", err)
	assert.Equal(t, expectedReply, string(payload))
	assert.Equal(t, gorilla.CloseGoingAway, closeErr.Code)
	assert.True(t, relaycommon.IsNormalWSClose(closeErr))

	return realtimeSessionResult{conn.Subprotocol(), messageType, string(payload), closeErr.Code}
}

func TestRealtimeEndToEndOnBothAdapters(t *testing.T) {
	frames := make(map[string]string, 2)

	t.Run("gin", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		engine := gin.New()
		engine.GET("/v1/realtime", ginadapter.Handler(func(c contract.Context) {
			Relay(c, types.RelayFormatOpenAIRealtime)
		}))

		listener := listenGin(t, engine)
		frames["gin"] = realtimeErrorFrame(t, listener)
	})

	t.Run("fiber", func(t *testing.T) {
		app := fiber.New(fiber.Config{DisableStartupMessage: true})
		app.Add(fiber.MethodGet, "/v1/realtime", func(c *fiber.Ctx) error {
			return fiberadapter.Dispatch(c, []contract.Handler{func(cc contract.Context) {
				Relay(cc, types.RelayFormatOpenAIRealtime)
			}})
		})

		frames["fiber"] = realtimeErrorFrame(t, listenFiber(t, app))
	})

	require.Len(t, frames, 2, "both adapters must have produced a frame")

	// The frames must agree in shape. The event id embeds a per-request value, so
	// the comparison is on the decoded structure rather than on raw bytes.
	ginFrame := decodeRealtimeEvent(t, frames["gin"])
	fiberFrame := decodeRealtimeEvent(t, frames["fiber"])

	assert.Equal(t, "error", ginFrame["type"])
	assert.Equal(t, ginFrame["type"], fiberFrame["type"],
		"both adapters must report a realtime failure the same way")

	ginError, ok := ginFrame["error"].(map[string]any)
	require.True(t, ok, "the gin frame must carry an error object")
	fiberError, ok := fiberFrame["error"].(map[string]any)
	require.True(t, ok, "the fiber frame must carry an error object")

	assert.Equal(t, ginError["type"], fiberError["type"],
		"the error type a client observes must not depend on the transport")
	assert.NotEmpty(t, ginError["message"])
	assert.NotEmpty(t, fiberError["message"])
}

// realtimeErrorFrame dials the realtime endpoint, asserts the handshake, and
// returns the first frame the server sends.
func realtimeErrorFrame(t *testing.T, baseURL string) string {
	t.Helper()

	dialer := gorilla.Dialer{
		Subprotocols:     []string{"realtime"},
		HandshakeTimeout: 10 * time.Second,
	}
	conn, response, err := dialer.Dial(strings.Replace(baseURL, "http://", "ws://", 1)+"/v1/realtime", nil)
	require.NoError(t, err, "the realtime handshake must complete")
	t.Cleanup(func() { _ = conn.Close() })

	assert.Equal(t, http.StatusSwitchingProtocols, response.StatusCode)
	assert.Equal(t, "realtime", response.Header.Get("Sec-WebSocket-Protocol"),
		"Sec-WebSocket-Protocol: realtime must be echoed")
	assert.Equal(t, "realtime", conn.Subprotocol())

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(15*time.Second)))
	messageType, payload, err := conn.ReadMessage()
	require.NoError(t, err, "the relay failure must arrive as a frame on the socket")
	assert.Equal(t, gorilla.TextMessage, messageType,
		"realtime events are text frames")

	return string(payload)
}

func decodeRealtimeEvent(t *testing.T, frame string) map[string]any {
	t.Helper()

	decoded := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(frame), &decoded),
		"a realtime frame must be a JSON event: %s", frame)
	return decoded
}

// listenGin starts engine on a random loopback port and returns its base URL.
func listenGin(t *testing.T, engine *gin.Engine) string {
	t.Helper()

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return server.URL
}

// listenFiber starts app on a random loopback port and returns its base URL.
func listenFiber(t *testing.T, app *fiber.App) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() { _ = app.Listener(listener) }()
	t.Cleanup(func() { _ = app.ShutdownWithTimeout(2 * time.Second) })

	return "http://" + listener.Addr().String()
}
