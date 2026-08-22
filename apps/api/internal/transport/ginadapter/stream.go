package ginadapter

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/internal/transport/contract"

	"github.com/gin-gonic/gin"
)

// EventStreamHeadersKey marks a request whose streaming headers were installed,
// so nested relay helpers can call SetHeaders without emitting them twice.
//
// It is exported because relay/helper.SetEventStreamHeaders sets the same flag
// while the migration is in progress; both paths must agree on one key or the
// headers can be written twice for a single response.
const EventStreamHeadersKey = "event_stream_headers_set"

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

func (s *responseStream) Flush() error {
	flusher, ok := s.gin.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}
	flusher.Flush()
	return nil
}

// assert the adapter satisfies the contract at compile time.
var (
	_ contract.EventStream    = (*eventStream)(nil)
	_ contract.ResponseStream = (*responseStream)(nil)
	_ contract.Context        = (*requestContext)(nil)
)
