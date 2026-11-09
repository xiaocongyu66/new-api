package identity

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	promptpresets "github.com/QuantumNous/new-api/relaykit/dto/prompt_presets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTokenSettingAcceptsKnownPreset(t *testing.T) {
	for _, name := range []string{promptpresets.PresetAuto, promptpresets.PresetSillyTavern, promptpresets.PresetTavernCard} {
		got, err := NormalizeTokenSetting(`{"system_prompt_preset":"` + name + `"}`)
		require.NoError(t, err, name)
		assert.Equal(t, name, dto.TokenSettingsFromRaw(got).SystemPromptPreset, name)
	}
}

func TestNormalizeTokenSettingRejectsUnknownPreset(t *testing.T) {
	for _, name := range []string{"auto_", "tavern_card_v2", "Silly_Tavern", ""} {
		raw := `{"system_prompt_preset":"` + name + `"}`
		if name == "" {
			raw = `{"system_prompt_preset":"","system_prompt":"text"}`
		}
		_, err := NormalizeTokenSetting(raw)
		if name == "" {
			assert.NoError(t, err, "empty preset falls through to plain text")
			continue
		}
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), "invalid system_prompt_preset")
	}
}

func TestNormalizeTokenSettingEmptyAndInvalidJSON(t *testing.T) {
	got, err := NormalizeTokenSetting("")
	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = NormalizeTokenSetting("   ")
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = NormalizeTokenSetting("not-json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token setting")
}

func TestNormalizeTokenSettingClearsZeroValueBinding(t *testing.T) {
	got, err := NormalizeTokenSetting(`{"system_prompt":"","user_system_prompt_override":false}`)
	require.NoError(t, err)
	assert.Empty(t, got, "a binding with nothing set should not persist JSON noise")
}

func TestNormalizeTokenSettingKeepsOverrideOnlyBinding(t *testing.T) {
	got, err := NormalizeTokenSetting(`{"user_system_prompt_override":true}`)
	require.NoError(t, err)
	require.NotEmpty(t, got)
	assert.True(t, dto.TokenSettingsFromRaw(got).UserSystemPromptOverride)
}

func TestTokenSettingRoundTripsThroughStore(t *testing.T) {
	canonical, err := NormalizeTokenSetting(`{"system_prompt_preset":"auto","unknown_key":1}`)
	require.NoError(t, err)

	token := Token{Setting: canonical}
	settings := token.GetTokenSettings()
	assert.Equal(t, promptpresets.PresetAuto, settings.SystemPromptPreset)
	assert.False(t, settings.UserSystemPromptOverride)
	assert.Empty(t, settings.SystemPrompt)
}
