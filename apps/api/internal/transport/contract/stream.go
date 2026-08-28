package contract

import "io"

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
	Flush() error
}
