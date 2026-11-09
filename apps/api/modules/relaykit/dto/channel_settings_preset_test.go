package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto/prompt_presets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSystemPromptAutoSelectsPerModel(t *testing.T) {
	setting := &ChannelSettings{SystemPromptPreset: promptpresets.PresetAuto}

	gemini := setting.ResolveSystemPrompt("gemini-3.6-flash")
	require.NotEmpty(t, gemini)
	assert.Equal(t, promptpresets.Get(promptpresets.PresetColdCoffeeGemini), gemini)

	claude := setting.ResolveSystemPrompt("claude-opus-4-6")
	require.NotEmpty(t, claude)
	assert.Equal(t, promptpresets.Get(promptpresets.PresetColdCoffeeClaude), claude)

	// An unknown model still gets a jailbreak prompt instead of nothing.
	unknown := setting.ResolveSystemPrompt("some-unlisted-model")
	require.NotEmpty(t, unknown)
	assert.Equal(t, promptpresets.Get(promptpresets.PresetFallback), unknown)
}

func TestResolveSystemPromptModelOverrideBeatsAutoPreset(t *testing.T) {
	setting := &ChannelSettings{
		SystemPromptPreset: promptpresets.PresetAuto,
		SystemPromptModels: map[string]string{"gemini-3.6-flash": "MODEL-SPECIFIC"},
	}

	assert.Equal(t, "MODEL-SPECIFIC", setting.ResolveSystemPrompt("gemini-3.6-flash"))
	// Other models still use the automatic preset.
	assert.Equal(t,
		promptpresets.Get(promptpresets.PresetColdCoffeeClaude),
		setting.ResolveSystemPrompt("claude-opus-4-6"),
	)
}

func TestResolveSystemPromptFixedPresetIgnoresModelFamily(t *testing.T) {
	setting := &ChannelSettings{SystemPromptPreset: promptpresets.PresetTavernCard}

	// A fixed preset applies to every model, including one that a
	// model-specific preset would otherwise capture.
	assert.Equal(t,
		promptpresets.Get(promptpresets.PresetTavernCard),
		setting.ResolveSystemPrompt("gemini-3.6-flash"),
	)
}

func TestResolveSystemPromptEmptyPresetFallsBackToChannelPrompt(t *testing.T) {
	setting := &ChannelSettings{SystemPrompt: "CHANNEL"}
	assert.Equal(t, "CHANNEL", setting.ResolveSystemPrompt("gemini-3.6-flash"))
}

func TestResolveSystemPromptUnknownPresetFallsBackToChannelPrompt(t *testing.T) {
	setting := &ChannelSettings{
		SystemPrompt:       "CHANNEL",
		SystemPromptPreset: "not-a-preset",
	}
	assert.Equal(t, "CHANNEL", setting.ResolveSystemPrompt("gemini-3.6-flash"))
}

func TestValidatePresetAcceptsEmptyAutoAndKnownPresets(t *testing.T) {
	assert.NoError(t, (&ChannelSettings{}).ValidatePreset())
	assert.NoError(t, (&ChannelSettings{SystemPromptPreset: ""}).ValidatePreset())
	assert.NoError(t, (&ChannelSettings{SystemPromptPreset: promptpresets.PresetAuto}).ValidatePreset())
	assert.NoError(t, (&ChannelSettings{SystemPromptPreset: promptpresets.PresetTavernCard}).ValidatePreset())
	assert.NoError(t, (&ChannelSettings{SystemPromptPreset: promptpresets.PresetColdCoffeeClaude}).ValidatePreset())
}

func TestValidatePresetRejectsUnknownPreset(t *testing.T) {
	var nilSetting *ChannelSettings
	assert.NoError(t, nilSetting.ValidatePreset())

	err := (&ChannelSettings{SystemPromptPreset: "not-a-preset"}).ValidatePreset()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid system_prompt_preset")
	assert.Contains(t, err.Error(), "not-a-preset")
}
