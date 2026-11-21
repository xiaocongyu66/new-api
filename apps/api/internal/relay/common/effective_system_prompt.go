package common

import (
	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// System prompt source layers recorded in the consume-log audit. The token
// layer sits above the channel layer, so "token" means a token binding won
// and "channel" means channel settings were used unchanged. "jailbreak" means
// at least one DB-managed rule was merged into the resolved prompt.
const (
	SystemPromptSourceToken     = "token"
	SystemPromptSourceChannel   = "channel"
	SystemPromptSourceJailbreak = "jailbreak"
)

// EffectiveSystemPrompt is the system prompt that should be injected for a
// request, together with the metadata needed to apply it (Override decides
// replace-vs-prepend) and to audit it (Source/Preset/AutoResolved).
type EffectiveSystemPrompt struct {
	// Prompt is the resolved prompt text; empty means nothing to inject.
	Prompt string
	// Override mirrors the winning layer's UserSystemPromptOverride.
	Override bool
	// Source is SystemPromptSourceToken, SystemPromptSourceChannel, or
	// SystemPromptSourceJailbreak; empty when Prompt is empty.
	Source string
	// Preset is the preset name that produced the prompt; "" for plain text.
	Preset string
	// AutoResolved marks preset names picked by the auto resolver.
	AutoResolved bool
	// PerModel marks channel prompts served from the per-model override table
	// (SystemPromptModels) for the request model. Always false for the token
	// source, which has no per-model table.
	PerModel bool
	// JailbreakRules lists the matched rule names merged into Prompt, in
	// application order. Empty when no DB rule contributed.
	JailbreakRules []string
}

// ResolveEffectiveSystemPrompt resolves the system prompt for a request with
// token-level bindings taking priority over channel settings: a non-empty
// token prompt replaces the channel result entirely (no stacking), while an
// empty token binding falls back to the channel chain. The same priority
// holds inside each layer (preset first, then plain text, then per-model
// entries for channels).
//
// DB-managed jailbreak rules are then merged on top of the resolved prompt.
// Rules apply in ascending priority order, so the highest-priority rule wins
// for Replace while Prepend/Append compose deterministically.
func ResolveEffectiveSystemPrompt(c contract.Context, info *RelayInfo) EffectiveSystemPrompt {
	if info == nil {
		return EffectiveSystemPrompt{}
	}
	modelName := info.OriginModelName
	resolved := EffectiveSystemPrompt{}
	if c != nil {
		if tokenSettings, ok := common.GetContextKeyType[dto.TokenSettings](c, constant.ContextKeyTokenSetting); ok {
			if resolution := tokenSettings.ResolveSystemPrompt(modelName); resolution.Prompt != "" {
				resolved = EffectiveSystemPrompt{
					Prompt:       resolution.Prompt,
					Override:     tokenSettings.UserSystemPromptOverride,
					Source:       SystemPromptSourceToken,
					Preset:       resolution.Preset,
					AutoResolved: resolution.AutoResolved,
				}
			}
		}
	}
	if resolved.Prompt == "" {
		// Channel settings live in context from the distribute middleware and are
		// mirrored onto ChannelMeta by InitChannelMeta. The pre-consume estimate
		// runs before InitChannelMeta, so fall back to the meta copy when the
		// context does not carry one yet (e.g. realtime infos, which never build
		// the meta, and synthetic test infos that set the meta directly).
		var channelSettings dto.ChannelSettings
		var ok bool
		if c != nil {
			channelSettings, ok = common.GetContextKeyType[dto.ChannelSettings](c, constant.ContextKeyChannelSetting)
		}
		if !ok && info.ChannelMeta != nil {
			channelSettings = info.ChannelSetting
		}
		resolution := channelSettings.ResolveSystemPromptDetail(modelName)
		if resolution.Prompt != "" {
			resolved = EffectiveSystemPrompt{
				Prompt:       resolution.Prompt,
				Override:     channelSettings.UserSystemPromptOverride,
				Source:       SystemPromptSourceChannel,
				Preset:       resolution.Preset,
				AutoResolved: resolution.AutoResolved,
				PerModel:     resolution.PerModel,
			}
		}
	}
	return applyJailbreakRules(c, info, modelName, resolved, nil, 0)
}

// applyJailbreakRules merges matched DB rules into the resolved prompt and
// records which rules contributed. The input is returned unchanged when no
// rule applies.
func applyJailbreakRules(c contract.Context, info *RelayInfo, modelName string, resolved EffectiveSystemPrompt, extra *channelpkg.JailbreakRule, excludeID int) EffectiveSystemPrompt {
	channelID := 0
	if info.ChannelMeta != nil {
		channelID = info.ChannelMeta.ChannelId
	}
	if channelID == 0 && c != nil {
		channelID = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	}
	rules := channelpkg.MatchJailbreakRules(channelID, modelName)
	if len(rules) == 0 && extra == nil {
		return resolved
	}
	prompt := resolved.Prompt
	override := resolved.Override
	applied := make([]string, 0, len(rules))
	for i := len(rules) - 1; i >= 0; i-- {
		rule := rules[i]
		if rule.Kind != channelpkg.JailbreakKindPrompt {
			continue
		}
		if excludeID > 0 && rule.Id == excludeID {
			continue
		}
		switch rule.Mode {
		case channelpkg.JailbreakModeReplace:
			prompt = rule.Prompt
			override = true
		case channelpkg.JailbreakModePrepend:
			prompt = joinSystemPrompts(rule.Prompt, prompt)
		default:
			prompt = joinSystemPrompts(prompt, rule.Prompt)
		}
		applied = append(applied, rule.Name)
	}
	if extra != nil && extra.Prompt != "" {
		switch extra.Mode {
		case channelpkg.JailbreakModeReplace:
			prompt = extra.Prompt
			override = true
		case channelpkg.JailbreakModePrepend:
			prompt = joinSystemPrompts(extra.Prompt, prompt)
		default:
			prompt = joinSystemPrompts(prompt, extra.Prompt)
		}
		applied = append(applied, extra.Name)
	}
	if len(applied) == 0 {
		return resolved
	}
	resolved.Prompt = prompt
	resolved.Override = override
	resolved.Source = SystemPromptSourceJailbreak
	resolved.JailbreakRules = applied
	return resolved
}

func joinSystemPrompts(parts ...string) string {
	joined := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		if joined == "" {
			joined = part
			continue
		}
		joined += "\n\n" + part
	}
	return joined
}

// PreviewEffectiveHeaders composes the effective upstream headers for an
// admin dry run: the caller supplies the channel-level headers (already
// decoded) and receives the same merged map the relay path would produce,
// together with the applied rule names. No upstream request is made.
func PreviewEffectiveHeaders(channelID int, modelName string, baseHeaders map[string]interface{}, extraHeaders map[string]interface{}, extraName string, excludeID int) (map[string]interface{}, []string) {
	if baseHeaders == nil {
		baseHeaders = map[string]interface{}{}
	}
	merged := make(map[string]interface{}, len(baseHeaders)+8)
	for key, value := range baseHeaders {
		merged[key] = value
	}
	applied := make([]string, 0)
	for _, rule := range channelpkg.MatchJailbreakHeaderRules(channelID, modelName) {
		if excludeID > 0 && rule.Id == excludeID {
			continue
		}
		for key, value := range sanitizeHeaderOverrideMap(rule.Headers) {
			merged[key] = value
		}
		applied = append(applied, rule.Name)
	}
	for key, value := range extraHeaders {
		merged[key] = value
	}
	if len(extraHeaders) > 0 && extraName != "" {
		applied = append(applied, extraName)
	}
	return merged, applied
}

// PreviewEffectiveSystemPrompt composes the prompt for an admin dry run: the
// caller supplies the simulated base prompt (and which layer it represents),
// the target channel and model, and receives the same merged result the relay
// path would produce. No upstream request is made.
func PreviewEffectiveSystemPrompt(channelID int, modelName, basePrompt, baseSource string, baseOverride bool, extra *channelpkg.JailbreakRule, excludeID int) EffectiveSystemPrompt {
	base := EffectiveSystemPrompt{}
	if basePrompt != "" {
		if baseSource == "" {
			baseSource = SystemPromptSourceChannel
		}
		base = EffectiveSystemPrompt{
			Prompt:   basePrompt,
			Override: baseOverride,
			Source:   baseSource,
		}
	}
	info := &RelayInfo{
		OriginModelName: modelName,
		ChannelMeta:     &ChannelMeta{ChannelId: channelID},
	}
	return applyJailbreakRules(nil, info, modelName, base, extra, excludeID)
}
