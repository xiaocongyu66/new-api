package common

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
)

// ExtractOutputTextFromResponses is the one Responses-format helper the relay
// channels still reach for through this package (openai/chat_via_responses.go
// and openai/responses_via_chat.go). The rest of the former forwarding shims
// were removed: their callers now use relaykit/relayconvert directly.
func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	return relayconvert.ExtractOutputTextFromResponses(resp)
}
