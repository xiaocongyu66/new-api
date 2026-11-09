package dto

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto/prompt_presets"
)

// TokenSettings is the relay behaviour bound to a single API token. It is
// stored as JSON in the token's setting TEXT column, mirroring how channel
// settings ride in the channel row. The system prompt binding lives here so
// a jailbreak prompt survives channel re-selection: the token binding wins
// over channel settings, so routing a token to another channel no longer
// changes what gets injected.
type TokenSettings struct {
	// SystemPromptPreset binds a built-in prompt preset to the token.
	// Accepted values: "", "auto" (resolve by request model), or a preset
	// name from the promptpresets catalogue. Takes priority over
	// SystemPrompt, and over every channel-level prompt source.
	SystemPromptPreset string `json:"system_prompt_preset,omitempty"`
	// SystemPrompt is the token-level plain-text prompt. It applies when no
	// preset binding resolves, and replaces the channel-level prompt
	// entirely when non-empty.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// UserSystemPromptOverride replaces the caller's own system prompt with
	// the resolved token prompt. The default (false) prepends the token
	// prompt in front of the caller's system prompt. Same semantics as the
	// channel-level flag, applied to whichever prompt the token layer
	// resolves.
	UserSystemPromptOverride bool `json:"user_system_prompt_override,omitempty"`
}

// SystemPromptResolution describes the prompt chosen for a request and where
// it came from. Preset is the preset name that produced the prompt ("" for a
// plain-text prompt); AutoResolved marks preset names picked by the auto
// resolver rather than stored verbatim.
type SystemPromptResolution struct {
	Prompt       string
	Preset       string
	AutoResolved bool
	// PerModel marks prompts served from ChannelSettings.SystemPromptModels,
	// the per-model override table, rather than a preset or plain text.
	// Token-level resolution never sets it: TokenSettings has no per-model
	// table.
	PerModel bool
}

// ResolveSystemPrompt resolves the token-level system prompt for modelName by
// priority: preset binding (auto or fixed) first, then the token plain-text
// prompt. A nil receiver or an unknown preset name resolves to the plain-text
// prompt, so a stale preset name after a catalogue change degrades to the
// token text instead of vanishing.
func (s *TokenSettings) ResolveSystemPrompt(modelName string) SystemPromptResolution {
	if s == nil {
		return SystemPromptResolution{}
	}
	if s.SystemPromptPreset != "" {
		if s.SystemPromptPreset == promptpresets.PresetAuto {
			name := promptpresets.ResolveAuto(modelName)
			if p := promptpresets.Get(name); p != "" {
				return SystemPromptResolution{Prompt: p, Preset: name, AutoResolved: true}
			}
			return SystemPromptResolution{Prompt: s.SystemPrompt}
		}
		if p := promptpresets.Get(s.SystemPromptPreset); p != "" {
			return SystemPromptResolution{Prompt: p, Preset: s.SystemPromptPreset}
		}
		return SystemPromptResolution{Prompt: s.SystemPrompt}
	}
	return SystemPromptResolution{Prompt: s.SystemPrompt}
}

// TokenSettingsToRaw marshals settings back to the JSON form stored in the
// token setting column. Empty settings serialize to "{}" — callers that want
// to clear the column can check the zero value first.
func TokenSettingsToRaw(settings TokenSettings) (string, error) {
	data, err := json.Marshal(settings)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// TokenSettingsFromRaw parses the JSON stored in a token's setting column.
// Empty input yields the zero settings; malformed JSON also yields the zero
// settings rather than an error so one corrupted row cannot break relay of
// every request using that token. Validation of incoming values happens at
// the API boundary, not here.
func TokenSettingsFromRaw(raw string) TokenSettings {
	settings := TokenSettings{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return settings
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return TokenSettings{}
	}
	return settings
}
