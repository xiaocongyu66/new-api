package contract

import (
	"io"
	"time"
)

// EventStreamHeadersKey marks a request whose streaming headers were installed,
// so nested relay helpers can request them without emitting them twice.
//
// It lives in the contract because two paths set it: the adapter's
// EventStream.SetHeaders and the relay helper that installs the same headers
// directly. Both must agree on one key or a single response gets two header
// writes.
const EventStreamHeadersKey = "event_stream_headers_set"

// EventStream writes a Server-Sent Events response.
//
// The relay pipeline streams provider output incrementally, so the byte framing
// produced here is part of the public API surface: clients parse on the exact
// `data: ` prefix and blank-line terminator. Implementations must not buffer
// whole responses, and must surface a disconnected client as an error so the
// caller stops pulling from the upstream provider.
type EventStream interface {
	// SetHeaders installs the streaming response headers. It is idempotent per
	// request so repeated calls from nested relay helpers stay safe.
	SetHeaders()

	// WriteEvent writes one `data: <payload>` frame terminated by a blank line.
	WriteEvent(payload string) error

	// WriteNamedEvent writes an `event: <name>` line followed by a
	// `data: <payload>` frame, used by the Claude-style streams.
	WriteNamedEvent(name, payload string) error

	// WriteComment writes an SSE comment frame, used for keep-alive pings that
	// clients must not surface as content.
	WriteComment(text string) error

	// WriteRaw writes bytes verbatim, for callers that already framed output or
	// that proxy an upstream body through unchanged.
	WriteRaw(payload []byte) (int, error)

	// Flush pushes buffered bytes to the client. It reports an error when the
	// client is gone or the writer cannot flush.
	Flush() error

	// Done reports whether the client disconnected or the request was cancelled.
	Done() bool
}

// ResponseStream exposes the raw response body for endpoints that copy upstream
// bytes (video content, reverse proxies) instead of emitting SSE frames.
type ResponseStream interface {
	io.Writer

	WriteHeader(status int)
	SetHeader(key, value string)
	// Header reads back a response header already staged for this response.
	//
	// The SSE renderer installs Cache-Control only when nothing else set it, so
	// a request that already carries a stricter no-store policy keeps it. That
	// conditional needs a read, and no other accessor provides one.
	Header(key string) string
	// AddHeader appends a value to key rather than replacing it, for upstream
	// headers that legitimately repeat. Codex forwards X-Reasoning-Included and
	// X-Codex-Turn-State this way, and collapsing repeats onto SetHeader would
	// silently drop all but the last value.
	AddHeader(key, value string)
	Flush() error
	// SetWriteDeadline bounds one blocked write to a slow client, so a reader
	// waiting on the streaming goroutine can always finish. It reports false
	// when the writer cannot carry a deadline (an in-process recorder), which
	// callers treat as best-effort rather than as an error.
	SetWriteDeadline(deadline time.Time) bool
	// CanFlush reports whether Flush actually reaches the client. Streaming
	// callers probe it so a writer without flush support is an observable
	// condition rather than a stream that buffers to completion and looks like
	// a stalled provider.
	CanFlush() bool
}
