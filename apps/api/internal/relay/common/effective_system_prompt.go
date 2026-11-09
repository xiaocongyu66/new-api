package common

import (
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// System prompt source layers recorded in the consume-log audit. The token
// layer sits above the channel layer, so "token" means a token binding won
// and "channel" means channel settings were used unchanged.
const (
	SystemPromptSourceToken   = "token"
	SystemPromptSourceChannel = "channel"
)

// EffectiveSystemPrompt is the system prompt that should be injected for a
// request, together with the metadata needed to apply it (Override decides
// replace-vs-prepend) and to audit it (Source/Preset/AutoResolved).
type EffectiveSystemPrompt struct {
	// Prompt is the resolved prompt text; empty means nothing to inject.
	Prompt string
	// Override mirrors the winning layer's UserSystemPromptOverride.
	Override bool
	// Source is SystemPromptSourceToken or SystemPromptSourceChannel; empty
	// when Prompt is empty.
	Source string
	// Preset is the preset name that produced the prompt; "" for plain text.
	Preset string
	// AutoResolved marks preset names picked by the auto resolver.
	AutoResolved bool
}

// ResolveEffectiveSystemPrompt resolves the system prompt for a request with
// token-level bindings taking priority over channel settings: a non-empty
// token prompt replaces the channel result entirely (no stacking), while an
// empty token binding falls back to the channel chain. The same priority
// holds inside each layer (preset first, then plain text, then per-model
// entries for channels).
func ResolveEffectiveSystemPrompt(c contract.Context, info *RelayInfo) EffectiveSystemPrompt {
	if info == nil {
		return EffectiveSystemPrompt{}
	}
	modelName := info.OriginModelName
	if c != nil {
		if tokenSettings, ok := common.GetContextKeyType[dto.TokenSettings](c, constant.ContextKeyTokenSetting); ok {
			if resolution := tokenSettings.ResolveSystemPrompt(modelName); resolution.Prompt != "" {
				return EffectiveSystemPrompt{
					Prompt:       resolution.Prompt,
					Override:     tokenSettings.UserSystemPromptOverride,
					Source:       SystemPromptSourceToken,
					Preset:       resolution.Preset,
					AutoResolved: resolution.AutoResolved,
				}
			}
		}
	}
	resolution := info.ChannelSetting.ResolveSystemPromptDetail(modelName)
	if resolution.Prompt == "" {
		return EffectiveSystemPrompt{}
	}
	return EffectiveSystemPrompt{
		Prompt:       resolution.Prompt,
		Override:     info.ChannelSetting.UserSystemPromptOverride,
		Source:       SystemPromptSourceChannel,
		Preset:       resolution.Preset,
		AutoResolved: resolution.AutoResolved,
	}
}
