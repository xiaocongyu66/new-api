package handler

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	prompt_presets "github.com/QuantumNous/new-api/relaykit/dto/prompt_presets"
)

// SystemPromptPresetItem is the operator-facing description of one built-in
// jailbreak preset. The prompt text itself stays in the binary and is never
// sent to the browser.
type SystemPromptPresetItem struct {
	// Name is the value stored in the channel setting.
	Name string `json:"name"`
	// ModelFamily groups presets in the dropdown.
	ModelFamily string `json:"model_family"`
	// Models lists the exact model names the preset targets. An empty list
	// means the preset applies to every model.
	Models []string `json:"models"`
	// Bytes is the prompt size, a rough proxy for its token cost.
	Bytes int `json:"bytes"`
}

// ListSystemPromptPresets returns the built-in jailbreak preset catalogue so
// the channel drawer can offer a dropdown instead of a hardcoded switch.
func ListSystemPromptPresets(c contract.Context) {
	presets := promptpresets.List()
	items := make([]SystemPromptPresetItem, 0, len(presets)+1)
	// The automatic mode is a resolver, not a prompt, but the operator picks
	// it from the same dropdown so it has to be listed first.
	items = append(items, SystemPromptPresetItem{
		Name:        promptpresets.PresetAuto,
		ModelFamily: "generic",
		Models:      []string{},
	})
	for _, preset := range presets {
		models := preset.Models
		if models == nil {
			models = []string{}
		}
		items = append(items, SystemPromptPresetItem{
			Name:        preset.Name,
			ModelFamily: preset.ModelFamily,
			Models:      models,
			Bytes:       len(preset.Prompt),
		})
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data":    items,
	})
}
