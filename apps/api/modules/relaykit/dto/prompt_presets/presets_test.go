package promptpresets

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAutoPicksPresetByExactModel(t *testing.T) {
	tests := []struct {
		model   string
		want    string
		wantNot string
	}{
		{model: "gemini-3.6-flash", want: Get(PresetColdCoffeeGemini)},
		{model: "claude-opus-4-6", want: Get(PresetColdCoffeeClaude)},
		{model: "grok-4.7", want: Get(PresetColdCoffeeGrok)},
		{model: "deepseek-v4.1-flash", want: Get(PresetColdCoffeeDeepSeek)},
		{model: "glm-5.3", want: Get(PresetColdCoffeeGLM53)},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := ResolveAuto(tt.model)
			require.NotEmpty(t, got)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveAutoFallsBackToModelFamily(t *testing.T) {
	// A model that no preset lists exactly still lands on its family preset
	// instead of the generic fallback.
	got := ResolveAuto("gemini-3.6-flash-preview")
	require.NotEmpty(t, got)
	assert.Equal(t, Get(PresetColdCoffeeGemini), got)

	got = ResolveAuto("claude-sonnet-5-5-thinking")
	require.NotEmpty(t, got)
	assert.Equal(t, Get(PresetColdCoffeeClaude), got)
}

func TestResolveAutoFallsBackToGenericPresetForUnknownModel(t *testing.T) {
	got := ResolveAuto("some-unlisted-model")
	require.NotEmpty(t, got)
	assert.Equal(t, Get(PresetFallback), got)
}

func TestResolveAutoIsCaseInsensitive(t *testing.T) {
	got := ResolveAuto("GEMINI-3.6-FLASH")
	require.NotEmpty(t, got)
	assert.Equal(t, Get(PresetColdCoffeeGemini), got)
}

func TestResolveAutoReturnsEmptyForEmptyModel(t *testing.T) {
	assert.Empty(t, ResolveAuto(""))
	assert.Empty(t, ResolveAuto("   "))
}

func TestGetReturnsEmptyForUnknownPreset(t *testing.T) {
	assert.Empty(t, Get(""))
	assert.Empty(t, Get("not-a-preset"))
}

func TestIsValidAcceptsAutoAndKnownPresets(t *testing.T) {
	assert.True(t, IsValid(PresetAuto))
	assert.True(t, IsValid(PresetTavernCard))
	assert.True(t, IsValid(PresetColdCoffeeGemini))
	assert.False(t, IsValid("not-a-preset"))
	assert.False(t, IsValid(""))
}

func TestListExposesEveryPresetWithContent(t *testing.T) {
	presets := List()
	require.NotEmpty(t, presets)
	seen := map[string]bool{}
	for _, preset := range presets {
		assert.False(t, seen[preset.Name], "duplicate preset name %s", preset.Name)
		seen[preset.Name] = true
		assert.NotEmpty(t, preset.Prompt, "preset %s has no content", preset.Name)
		assert.NotEmpty(t, preset.ModelFamily, "preset %s has no family", preset.Name)
	}
	assert.True(t, seen[PresetTavernCard])
	assert.True(t, seen[PresetColdCoffeeGemini])
}

func TestModelSpecificPresetsDeclareTargetModels(t *testing.T) {
	// A model-specific preset must list at least one model, otherwise the
	// automatic resolver can never route a request to it by name.
	for _, preset := range List() {
		if preset.ModelFamily == "generic" {
			continue
		}
		assert.NotEmpty(t, preset.Models, "preset %s declares a family but no models", preset.Name)
	}
}
