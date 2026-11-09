// Package promptpresets embeds built-in system prompt presets that channels
// can select by name. The prompt content ships inside the binary so operators
// pick a preset from a dropdown instead of pasting 50KB of text into a textarea.
//
// A preset may declare the exact model names it targets. When a channel opts
// into PresetAuto, the resolver picks the preset whose target models match the
// request model, so one channel can serve several model families with a
// purpose-built jailbreak prompt each.
package promptpresets

import (
	_ "embed"
	"strings"
)

//go:embed silly_tavern.txt
var sillyTavernPrompt string

//go:embed tavern_card.txt
var tavernCardPrompt string

//go:embed templates/coldcoffee_claude.md
var coldCoffeeClaudePrompt string

//go:embed templates/coldcoffee_grok.md
var coldCoffeeGrokPrompt string

//go:embed templates/coldcoffee_gemini.md
var coldCoffeeGeminiPrompt string

//go:embed templates/coldcoffee_deepseek.md
var coldCoffeeDeepSeekPrompt string

//go:embed templates/coldcoffee_glm53.md
var coldCoffeeGLM53Prompt string

// Preset names matching the frontend dropdown values.
const (
	// PresetAuto resolves the preset from the request model name: the preset
	// whose target models cover that model wins. It is not a prompt itself.
	PresetAuto = "auto"

	PresetSillyTavern = "silly_tavern"
	PresetTavernCard  = "tavern_card"

	PresetColdCoffeeClaude   = "coldcoffee_claude"
	PresetColdCoffeeGrok     = "coldcoffee_grok"
	PresetColdCoffeeGemini   = "coldcoffee_gemini"
	PresetColdCoffeeDeepSeek = "coldcoffee_deepseek"
	PresetColdCoffeeGLM53    = "coldcoffee_glm53"
)

// PresetFallback is the preset used when an automatic match finds no
// model-specific preset. It is a full jailbreak card rather than a stub.
const PresetFallback = PresetTavernCard

// Preset describes one built-in jailbreak prompt and the models it targets.
type Preset struct {
	// Name is the value stored in ChannelSettings.SystemPromptPreset.
	Name string
	// Prompt is the raw prompt text injected ahead of the caller's system prompt.
	Prompt string
	// Models lists the exact model names this preset is written for. An empty
	// list means the preset applies to every model and therefore only wins an
	// automatic match when no targeted preset covers the model.
	Models []string
	// ModelFamily groups presets for the operator-facing dropdown.
	ModelFamily string
}

// presets lists every built-in preset. Order matters only for the dropdown;
// automatic resolution picks by explicit model match, then by model family.
var presets = []Preset{
	{
		Name:        PresetTavernCard,
		Prompt:      tavernCardPrompt,
		ModelFamily: "generic",
	},
	{
		Name:        PresetSillyTavern,
		Prompt:      sillyTavernPrompt,
		ModelFamily: "generic",
	},
	{
		Name:        PresetColdCoffeeClaude,
		Prompt:      coldCoffeeClaudePrompt,
		ModelFamily: "claude",
		Models: []string{
			"claude-fable-5",
			"claude-fable-5-1",
			"claude-haiku-4-5",
			"claude-haiku-5-5",
			"claude-opus-4-5",
			"claude-opus-4-6",
			"claude-opus-4-7",
			"claude-opus-4-8",
			"claude-opus-5",
			"claude-opus-5-5",
			"claude-sonnet-4-5",
			"claude-sonnet-4-6",
			"claude-sonnet-5",
			"claude-sonnet-5-5",
		},
	},
	{
		Name:        PresetColdCoffeeGrok,
		Prompt:      coldCoffeeGrokPrompt,
		ModelFamily: "grok",
		Models: []string{
			"grok-4.7",
		},
	},
	{
		Name:        PresetColdCoffeeGemini,
		Prompt:      coldCoffeeGeminiPrompt,
		ModelFamily: "gemini",
		Models: []string{
			"gemini-3-flash",
			"gemini-3.1-flash-image",
			"gemini-3.1-pro",
			"gemini-3.1-pro-preview",
			"gemini-3.6-flash",
			"gemini-3.7-flash",
			"gemini-3.8-flash",
		},
	},
	{
		Name:        PresetColdCoffeeDeepSeek,
		Prompt:      coldCoffeeDeepSeekPrompt,
		ModelFamily: "deepseek",
		Models: []string{
			"deepseek-v4-flash",
			"deepseek-v4-flash-0731",
			"deepseek-v4-pro",
			"deepseek-v4-pro-0813",
			"deepseek-v4.1-flash",
		},
	},
	{
		Name:        PresetColdCoffeeGLM53,
		Prompt:      coldCoffeeGLM53Prompt,
		ModelFamily: "glm",
		Models: []string{
			"glm-5.3",
		},
	},
}

// Get returns the built-in prompt for the given preset name.
// An empty name or unknown preset returns "".
func Get(name string) string {
	if name == "" {
		return ""
	}
	for _, preset := range presets {
		if preset.Name == name {
			return preset.Name
		}
	}
	return ""
}

// IsValid reports whether name is a known preset.
func IsValid(name string) bool {
	if name == PresetAuto {
		return true
	}
	for _, preset := range presets {
		if preset.Name == name {
			return true
		}
	}
	return false
}

// List returns every built-in preset for the operator-facing catalogue. The
// returned presets share no mutable state with the package, so a caller that
// edits the model list cannot corrupt later resolutions.
func List() []Preset {
	out := make([]Preset, len(presets))
	for i, preset := range presets {
		out[i] = preset
		if preset.Models != nil {
			out[i].Models = make([]string, len(preset.Models))
			copy(out[i].Models, preset.Models)
		}
	}
	return out
}

// IsSelectable reports whether name is a preset an operator (or a token
// binding) may pin: any catalogue entry, or the automatic resolver.
func IsSelectable(name string) bool {
	return IsValid(name)
}

// Names lists every preset name in catalogue order.
func Names() []string {
	out := make([]string, 0, len(presets))
	for _, preset := range presets {
		out = append(out, preset.Name)
	}
	return out
}

// ResolveAuto picks the preset name for a model name. A preset that lists the
// model explicitly wins; otherwise a preset whose model family prefixes the
// model name wins ("gemini-3.6-flash-preview" lands on the Gemini preset,
// while "geminix-1" does not). Anything else — including a blank model name,
// which cannot be attributed to a family — resolves to "" so the caller falls
// through to the channel-level SystemPrompt. Callers turn the name into text
// with Get(name); returning the name keeps the audit trail expressible.
func ResolveAuto(modelName string) string {
	model := strings.ToLower(strings.TrimSpace(modelName))
	if model == "" {
		return ""
	}

	for _, preset := range presets {
		for _, target := range preset.Models {
			if strings.ToLower(target) == model {
				return preset.Name
			}
		}
	}

	for _, preset := range presets {
		if preset.ModelFamily == "" || preset.ModelFamily == "generic" {
			continue
		}
		if strings.HasPrefix(model, preset.ModelFamily+"-") {
			return preset.Name
		}
	}

	return Get(PresetFallback)
}
