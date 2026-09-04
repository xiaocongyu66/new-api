package common

import (
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

// CountTextToken routes every OpenAI text model through getTokenEncoder, which
// falls back to defaultTokenEncoder whenever tiktoken does not recognise the
// model name. tiktoken rejects newer names (gpt-5-chat), so the fallback is a
// live path, not a theoretical one — and until InitTokenEncoders() runs,
// defaultTokenEncoder is a nil interface and the fallback nil-panics.
//
// This is the runtime half of the bootstrap guard: main.go must call
// InitTokenEncoders() on THIS package (a dead duplicate lived in internal/usage
// and was the only one main called after the phase 0 move).
func TestCountTextTokenFallsBackWithoutPanicAfterInit(t *testing.T) {
	// Pin the premise: the fallback is reachable because tiktoken really does
	// reject this name. If tiktoken later learns it, this test must be re-aimed
	// at a name it still rejects rather than silently passing.
	const unknownOpenAIModel = "gpt-5-chat"
	if _, err := tokenizer.ForModel(tokenizer.Model(unknownOpenAIModel)); err == nil {
		t.Fatalf("tokenizer now supports %q; pick another unsupported OpenAI text model to keep exercising the fallback", unknownOpenAIModel)
	}

	previousDefault := defaultTokenEncoder
	previousMap := tokenEncoderMap
	t.Cleanup(func() {
		tokenEncoderMutex.Lock()
		defaultTokenEncoder = previousDefault
		tokenEncoderMap = previousMap
		tokenEncoderMutex.Unlock()
	})

	// Unwarmed state, exactly what a binary that never calls InitTokenEncoders
	// runs with: the fallback must panic, which is the bug being guarded.
	tokenEncoderMutex.Lock()
	defaultTokenEncoder = nil
	tokenEncoderMap = make(map[string]tokenizer.Codec)
	tokenEncoderMutex.Unlock()

	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected CountTextToken to panic while defaultTokenEncoder is nil; if it no longer does, the startup call may have become optional and this guard is stale")
			}
		}()
		CountTextToken("hello world", unknownOpenAIModel)
	}()

	// Warmed state: the same call must succeed and return a real count.
	tokenEncoderMutex.Lock()
	tokenEncoderMap = make(map[string]tokenizer.Codec)
	tokenEncoderMutex.Unlock()
	InitTokenEncoders()

	if got := CountTextToken("hello world", unknownOpenAIModel); got <= 0 {
		t.Errorf("CountTextToken(%q) = %d after InitTokenEncoders, want a positive token count", unknownOpenAIModel, got)
	}
}
