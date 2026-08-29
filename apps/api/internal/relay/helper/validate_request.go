package helper

import (
	"math"

	"github.com/QuantumNous/new-api/internal/gateway"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/QuantumNous/new-api/internal/transport/contract"
)

const maxTokensLimit = math.MaxInt32 / 2

func exceedsMaxTokensLimit(values ...*uint) bool {
	return gateway.ExceedsMaxTokensLimit(values...)
}

func GetAndValidateRequest(c contract.Context, format types.RelayFormat) (request dto.Request, err error) {
	return gateway.GetAndValidateRequest(c, format)
}

func GetAndValidAudioRequest(c contract.Context, relayMode int) (*dto.AudioRequest, error) {
	return gateway.GetAndValidAudioRequest(c, relayMode)
}

func GetAndValidateRerankRequest(c contract.Context) (*dto.RerankRequest, error) {
	return gateway.GetAndValidateRerankRequest(c)
}

func GetAndValidateEmbeddingRequest(c contract.Context, relayMode int) (*dto.EmbeddingRequest, error) {
	return gateway.GetAndValidateEmbeddingRequest(c, relayMode)
}

func GetAndValidateResponsesRequest(c contract.Context) (*dto.OpenAIResponsesRequest, error) {
	return gateway.GetAndValidateResponsesRequest(c)
}

func GetAndValidateAlphaSearchRequest(c contract.Context) (*dto.AlphaSearchRequest, error) {
	return gateway.GetAndValidateAlphaSearchRequest(c)
}

func GetAndValidateResponsesCompactionRequest(c contract.Context) (*dto.OpenAIResponsesCompactionRequest, error) {
	return gateway.GetAndValidateResponsesCompactionRequest(c)
}

func GetAndValidOpenAIImageRequest(c contract.Context, relayMode int) (*dto.ImageRequest, error) {
	return gateway.GetAndValidOpenAIImageRequest(c, relayMode)
}

func GetAndValidateClaudeRequest(c contract.Context) (textRequest *dto.ClaudeRequest, err error) {
	return gateway.GetAndValidateClaudeRequest(c)
}

func GetAndValidateTextRequest(c contract.Context, relayMode int) (*dto.GeneralOpenAIRequest, error) {
	return gateway.GetAndValidateTextRequest(c, relayMode)
}

func GetAndValidateGeminiRequest(c contract.Context) (*dto.GeminiChatRequest, error) {
	return gateway.GetAndValidateGeminiRequest(c)
}

func GetAndValidateGeminiEmbeddingRequest(c contract.Context) (*dto.GeminiEmbeddingRequest, error) {
	return gateway.GetAndValidateGeminiEmbeddingRequest(c)
}

func GetAndValidateGeminiBatchEmbeddingRequest(c contract.Context) (*dto.GeminiBatchEmbeddingRequest, error) {
	return gateway.GetAndValidateGeminiBatchEmbeddingRequest(c)
}
