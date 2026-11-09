package identity

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/dto/prompt_presets"
)

// NormalizeTokenSetting validates the relay settings JSON submitted with a
// token create/update request and returns the canonical form to store in the
// setting column. Empty input clears the binding. The preset name must be a
// known preset or the auto resolver; anything else is rejected so a typo
// cannot silently disable injection. Unknown JSON keys are dropped by the
// round-trip, keeping the stored payload exactly the supported shape.
func NormalizeTokenSetting(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	settings := dto.TokenSettings{}
	if err := common.UnmarshalJsonStr(raw, &settings); err != nil {
		return "", fmt.Errorf("invalid token setting: %w", err)
	}
	settings.SystemPromptPreset = strings.TrimSpace(settings.SystemPromptPreset)
	if settings.SystemPromptPreset != "" && !promptpresets.IsSelectable(settings.SystemPromptPreset) {
		return "", fmt.Errorf("invalid system_prompt_preset: %s", settings.SystemPromptPreset)
	}
	if settings.SystemPromptPreset == "" && strings.TrimSpace(settings.SystemPrompt) == "" && !settings.UserSystemPromptOverride {
		return "", nil
	}
	return dto.TokenSettingsToRaw(settings)
}
