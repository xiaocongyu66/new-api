package ginadapter

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/gin-gonic/gin"
)

// EventStreamHeadersKey is re-exported from the contract so existing adapter
// callers keep compiling; the constant itself lives there because non-adapter
// code sets the same flag.
const EventStreamHeadersKey = contract.EventStreamHeadersKey

// eventStream implements contract.EventStream over a gin response writer.
type eventStream struct {
	gin *gin.Context
}

// EventStream returns an SSE writer for the request.
func EventStream(c contract.Context) (contract.EventStream, error) {
	ginCtx, ok := Unwrap(c)
	if !ok {
		return nil, errors.New("ginadapter: context is not a gin request context")
	}
	return &eventStream{gin: ginCtx}, nil
}

func (s *eventStream) SetHeaders() {
	if _, exists := s.gin.Get(EventStreamHeadersKey); exists {
		return
	}
	s.gin.Set(EventStreamHeadersKey, true)

	header := s.gin.Writer.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("Transfer-Encoding", "chunked")
	header.Set("X-Accel-Buffering", "no")
}

func (s *eventStream) WriteEvent(payload string) error {
	if s.Done() {
		return s.contextErr()
	}
	if _, err := s.gin.Writer.WriteString("data: " + payload + "\n\n"); err != nil {
		return fmt.Errorf("write sse event: %w", err)
	}
	return s.Flush()
}

func (s *eventStream) WriteNamedEvent(name, payload string) error {
	if s.Done() {
		return s.contextErr()
	}
	if _, err := s.gin.Writer.WriteString("event: " + name + "\ndata: " + payload + "\n\n"); err != nil {
		return fmt.Errorf("write named sse event: %w", err)
	}
	return s.Flush()
}

func (s *eventStream) WriteComment(text string) error {
	if s.Done() {
		return s.contextErr()
	}
	if _, err := s.gin.Writer.WriteString(": " + text + "\n\n"); err != nil {
		return fmt.Errorf("write sse comment: %w", err)
	}
	return s.Flush()
}

func (s *eventStream) WriteRaw(payload []byte) (int, error) {
	if s.Done() {
		return 0, s.contextErr()
	}
	return s.gin.Writer.Write(payload)
}

// Flush mirrors the existing relay flush semantics, including recovering from a
// panic raised by a writer whose connection is already closed.
func (s *eventStream) Flush() (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("flush panic recovered: %v", recovered)
		}
	}()

	if s.gin == nil || s.gin.Writer == nil {
		return nil
	}
	if s.Done() {
		return s.contextErr()
	}

	flusher, ok := s.gin.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}
	flusher.Flush()
	return nil
}

func (s *eventStream) Done() bool {
	return s.gin != nil && s.gin.Request != nil && s.gin.Request.Context().Err() != nil
}

func (s *eventStream) contextErr() error {
	if s.gin == nil || s.gin.Request == nil {
		return errors.New("request context unavailable")
	}
	return fmt.Errorf("request context done: %w", s.gin.Request.Context().Err())
}

// responseStream implements contract.ResponseStream for raw body copies.
type responseStream struct {
	gin *gin.Context
}

// ResponseStream returns a raw response writer for proxy-style endpoints.
func ResponseStream(c contract.Context) (contract.ResponseStream, error) {
	ginCtx, ok := Unwrap(c)
	if !ok {
		return nil, errors.New("ginadapter: context is not a gin request context")
	}
	return &responseStream{gin: ginCtx}, nil
}

func (s *responseStream) Write(payload []byte) (int, error) {
	return s.gin.Writer.Write(payload)
}

func (s *responseStream) WriteHeader(status int) {
	s.gin.Writer.WriteHeader(status)
}

func (s *responseStream) SetHeader(key, value string) {
	s.gin.Writer.Header().Set(key, value)
}

func (s *responseStream) AddHeader(key, value string) {
	s.gin.Writer.Header().Add(key, value)
}

func (s *responseStream) Header(key string) string {
	return s.gin.Writer.Header().Get(key)
}

// Flush keeps going through the framework writer, not http.ResponseController.
// gin's Flush calls WriteHeaderNow before forwarding, so the status and headers
// commit ahead of the first flushed bytes; ResponseController would unwrap past
// that and change when headers reach the client on every SSE response.
//
// The type assertion is not a capability check: gin's ResponseWriter declares
// http.Flusher unconditionally and asserts the wrapped writer at call time, so
// it panics when nothing underneath can flush. CanFlush answers that question
// separately, and the recover here turns a writer that lies about it into an
// error rather than a crashed request.
func (s *responseStream) Flush() (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("streaming error: flusher not found: %v", recovered)
		}
	}()

	flusher, ok := s.gin.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}
	flusher.Flush()
	return nil
}

// SetWriteDeadline bounds one blocked write. Writers with no connection
// underneath (an in-process recorder) report false rather than failing the
// request, since the deadline is a safety bound and not part of the response.
func (s *responseStream) SetWriteDeadline(deadline time.Time) bool {
	return http.NewResponseController(s.gin.Writer).SetWriteDeadline(deadline) == nil
}

// CanFlush walks the writer's Unwrap chain looking for a real http.Flusher,
// which is what http.ResponseController does internally.
//
// It cannot be answered by asserting http.Flusher on the framework writer (gin
// always satisfies it) and it must not be answered by attempting a flush, since
// gin's Flush commits the status code as a side effect. Walking is the only
// probe that is both accurate and free of observable effect.
func (s *responseStream) CanFlush() bool {
	var writer http.ResponseWriter = s.gin.Writer
	for {
		switch candidate := writer.(type) {
		case interface{ Unwrap() http.ResponseWriter }:
			writer = candidate.Unwrap()
		case http.Flusher:
			return true
		default:
			return false
		}
	}
}

// ---- contract.Streaming on requestContext ----

// EventStream and ResponseStream are the seam business code streams through, so
// it never has to import this package to get a writer.
func (r *requestContext) EventStream() contract.EventStream {
	return &eventStream{gin: r.gin}
}

func (r *requestContext) ResponseStream() contract.ResponseStream {
	if r.gin.Writer == nil {
		return nil
	}
	return &responseStream{gin: r.gin}
}

// assert the adapter satisfies the contract at compile time.
var (
	_ contract.EventStream    = (*eventStream)(nil)
	_ contract.ResponseStream = (*responseStream)(nil)
	_ contract.Context        = (*requestContext)(nil)
)
