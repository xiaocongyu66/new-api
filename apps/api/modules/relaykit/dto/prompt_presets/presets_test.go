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
		{model: "gemini-3.6-flash", want: PresetColdCoffeeGemini},
		{model: "claude-opus-4-6", want: PresetColdCoffeeClaude},
		{model: "grok-4.7", want: PresetColdCoffeeGrok},
		{model: "deepseek-v4.1-flash", want: PresetColdCoffeeDeepSeek},
		{model: "glm-5.3", want: PresetColdCoffeeGLM53},
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
	assert.Equal(t, PresetColdCoffeeGemini, got)

	got = ResolveAuto("claude-sonnet-5-5-thinking")
	require.NotEmpty(t, got)
	assert.Equal(t, PresetColdCoffeeClaude, got)
}

func TestResolveAutoFallsBackToGenericPresetForUnknownModel(t *testing.T) {
	got := ResolveAuto("some-unlisted-model")
	require.NotEmpty(t, got)
	assert.Equal(t, PresetFallback, got)
}

func TestResolveAutoIsCaseInsensitive(t *testing.T) {
	got := ResolveAuto("GEMINI-3.6-FLASH")
	require.NotEmpty(t, got)
	assert.Equal(t, PresetColdCoffeeGemini, got)
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

func TestListReturnsIndependentModelSlices(t *testing.T) {
	first := List()
	var claude *Preset
	for i := range first {
		if first[i].Name == PresetColdCoffeeClaude {
			claude = &first[i]
		}
	}
	require.NotNil(t, claude)
	require.NotEmpty(t, claude.Models)
	claude.Models[0] = "mutated-model"
	second := List()
	for _, p := range second {
		if p.Name == PresetColdCoffeeClaude {
			assert.NotContains(t, p.Models, "mutated-model",
				"mutating a returned List() must not corrupt the package catalogue")
		}
	}
}

func TestResolveAutoFamilyBoundary(t *testing.T) {
	assert.Equal(t, Get(PresetFallback), ResolveAuto("geminix-1"))
	assert.Equal(t, PresetColdCoffeeGemini, ResolveAuto("gemini-3.6-flash-preview"))
}

func TestCatalogueAnchors(t *testing.T) {
	// The automatic resolver routes by these names; dropping one from a
	// catalogue silently reroutes live traffic to the generic fallback.
	type anchor struct {
		presetName string
		model      string
	}
	anchors := []anchor{
		{PresetColdCoffeeGemini, "gemini-3.6-flash"},
		{PresetColdCoffeeGemini, "gemini-3.1-pro"},
		{PresetColdCoffeeClaude, "claude-opus-4-6"},
		{PresetColdCoffeeClaude, "claude-sonnet-4-6"},
		{PresetColdCoffeeDeepSeek, "deepseek-v4.1-flash"},
	}
	modelsByPreset := make(map[string]map[string]bool)
	for _, preset := range List() {
		set := make(map[string]bool, len(preset.Models))
		for _, model := range preset.Models {
			set[model] = true
		}
		modelsByPreset[preset.Name] = set
	}
	for _, a := range anchors {
		assert.True(t, modelsByPreset[a.presetName][a.model],
			"preset %s must list model %s", a.presetName, a.model)
	}
	assert.Equal(t, PresetTavernCard, PresetFallback)
}
