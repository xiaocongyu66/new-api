package ginadapter

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/gorilla/websocket"
)

// UpgradeWebSocket completes the handshake through gorilla, which hijacks the
// hijackable http.ResponseWriter gin hands out.
//
// The concrete conn is held in its own variable and only widened into the
// interface on success. Returning `conn` directly would produce a non-nil
// interface holding a nil *websocket.Conn on failure, which defeats the
// `ws == nil` guard the relay's error path relies on and dereferences nil on
// every failed handshake.
func (r *requestContext) UpgradeWebSocket(subprotocols ...string) (contract.WSConn, error) {
	writer := r.gin.Writer
	if writer == nil {
		return nil, errors.New("ginadapter: no response writer to upgrade")
	}

	upgrader := websocket.Upgrader{
		Subprotocols: subprotocols,
		// Every origin is accepted: see contract.WebSocket.UpgradeWebSocket.
		CheckOrigin: func(*http.Request) bool { return true },
	}

	conn, err := upgrader.Upgrade(writer, r.gin.Request, nil)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
