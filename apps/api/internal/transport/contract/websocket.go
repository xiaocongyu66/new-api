package contract

// WSConn is the websocket surface the relay actually uses.
//
// It is exactly the three methods the relay calls, so both a gorilla
// *websocket.Conn (the upstream dial) and a gofiber/contrib *websocket.Conn (the
// server side after the Fiber cutover) satisfy it with zero wrappers. That is
// what lets one RelayInfo hold connections of two unrelated concrete types.
//
// It lives here rather than beside RelayInfo because UpgradeWebSocket returns
// it: the contract cannot import the relay packages without inverting the
// dependency arrow the transport layer exists to create. internal/relay/common
// keeps an alias, so the relay code that names relaycommon.WSConn is unaffected.
type WSConn interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	Close() error
}

// WebSocket upgrades a request off HTTP.
type WebSocket interface {
	// UpgradeWebSocket completes the protocol handshake and returns the
	// connection, which the caller owns and must Close.
	//
	// subprotocols are the values the server is willing to negotiate through
	// Sec-WebSocket-Protocol. A client offering none of them still upgrades,
	// without a negotiated subprotocol, which is what the realtime route has
	// always done.
	//
	// The returned WSConn is nil exactly when the error is non-nil, and it is a
	// true nil interface rather than an interface holding a nil pointer. The
	// distinction is load-bearing: the relay's error path checks `ws == nil`
	// before writing an error frame, so a non-nil interface wrapping a nil
	// pointer would dereference nil on every failed handshake. An
	// implementation must therefore never widen a failed upgrade's concrete
	// result into the interface.
	//
	// A failed handshake has already been answered: the upgrader writes its own
	// HTTP error response, so the caller must not write another one.
	//
	// Every origin is accepted. The handshake carries no credentials of its
	// own, the route behind it authenticates by bearer token like every other
	// relay route, and rejecting cross-origin handshakes here would break the
	// browser clients this endpoint exists for. This is the behaviour the route
	// has always had, stated rather than implied.
	//
	// Errors when the transport cannot hand out a connection at all: a context
	// with no client attached (the in-process contexts internal callers build)
	// reports one instead of pretending to upgrade.
	UpgradeWebSocket(subprotocols ...string) (WSConn, error)
}
