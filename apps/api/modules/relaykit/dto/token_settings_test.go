package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto/prompt_presets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenSettingsResolveSystemPromptPriority(t *testing.T) {
	fixedPreset := promptpresets.Get(promptpresets.PresetTavernCard)

	tests := []struct {
		name       string
		settings   TokenSettings
		model      string
		wantPrompt string
		wantPreset string
		wantAuto   bool
	}{
		{
			name:       "preset wins over token plain text",
			settings:   TokenSettings{SystemPromptPreset: promptpresets.PresetTavernCard, SystemPrompt: "plain"},
			model:      "gpt-4o",
			wantPrompt: fixedPreset,
			wantPreset: promptpresets.PresetTavernCard,
		},
		{
			name:       "plain text applies without a preset",
			settings:   TokenSettings{SystemPrompt: "plain"},
			model:      "gpt-4o",
			wantPrompt: "plain",
		},
		{
			name:       "auto resolves by model family",
			settings:   TokenSettings{SystemPromptPreset: promptpresets.PresetAuto, SystemPrompt: "plain"},
			model:      "claude-sonnet-4-5",
			wantPrompt: promptpresets.Get(promptpresets.ResolveAuto("claude-sonnet-4-5")),
			wantPreset: promptpresets.ResolveAuto("claude-sonnet-4-5"),
			wantAuto:   true,
		},
		{
			name:       "empty settings resolve to nothing",
			settings:   TokenSettings{},
			model:      "gpt-4o",
			wantPrompt: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.settings.ResolveSystemPrompt(tt.model)
			assert.Equal(t, tt.wantPrompt, got.Prompt)
			assert.Equal(t, tt.wantPreset, got.Preset)
			assert.Equal(t, tt.wantAuto, got.AutoResolved)
		})
	}
	require.NotEmpty(t, fixedPreset)
}

func TestTokenSettingsUnknownPresetFallsBackToPlainText(t *testing.T) {
	settings := TokenSettings{SystemPromptPreset: "removed_preset", SystemPrompt: "plain"}
	got := settings.ResolveSystemPrompt("gpt-4o")
	assert.Equal(t, "plain", got.Prompt)
	assert.Empty(t, got.Preset)
	assert.False(t, got.AutoResolved)
}

func TestTokenSettingsRoundTrip(t *testing.T) {
	raw := `{"system_prompt_preset":"auto","system_prompt":"text","user_system_prompt_override":true}`
	settings := TokenSettingsFromRaw(raw)
	assert.Equal(t, promptpresets.PresetAuto, settings.SystemPromptPreset)
	assert.Equal(t, "text", settings.SystemPrompt)
	assert.True(t, settings.UserSystemPromptOverride)

	canonical, err := TokenSettingsToRaw(settings)
	assert.NoError(t, err)
	assert.Equal(t, settings, TokenSettingsFromRaw(canonical))

	assert.Equal(t, TokenSettings{}, TokenSettingsFromRaw(""))
	assert.Equal(t, TokenSettings{}, TokenSettingsFromRaw("not-json"))
}
