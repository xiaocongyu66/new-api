package helper

import (
	"bufio"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/internal/gateway"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"

	"github.com/QuantumNous/new-api/internal/transport/contract"
)

const (
	InitialScannerBufferSize    = gateway.InitialScannerBufferSize
	DefaultMaxScannerBufferSize = gateway.DefaultMaxScannerBufferSize
	DefaultPingInterval         = gateway.DefaultPingInterval
)

func NewStreamScanner(reader io.Reader) *bufio.Scanner {
	return gateway.NewStreamScanner(reader)
}

func ExtendWriteDeadline(c contract.Context) {
	gateway.ExtendWriteDeadline(c)
}

// StreamResult is an alias for gateway.StreamResult for backward compatibility.
type StreamResult = gateway.StreamResult

// StreamScannerHandler is the core streaming engine used by all relay channels.
// It forwards to gateway.StreamScannerHandler.
func StreamScannerHandler(
	c contract.Context,
	resp *http.Response,
	info *relaycommon.RelayInfo,
	dataHandler func(data string, sr *StreamResult),
) {
	gateway.StreamScannerHandler(c, resp, info, dataHandler)
}
