package channel

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/settings"
)

type ChatCompletionsToResponsesPolicy struct {
	Enabled       bool     `json:"enabled"`
	AllChannels   bool     `json:"all_channels"`
	ChannelIDs    []int    `json:"channel_ids,omitempty"`
	ChannelTypes  []int    `json:"channel_types,omitempty"`
	ModelPatterns []string `json:"model_patterns,omitempty"`
}

func (p ChatCompletionsToResponsesPolicy) IsChannelEnabled(channelID int, channelType int) bool {
	if !p.Enabled {
		return false
	}
	if p.AllChannels {
		return true
	}

	if channelID > 0 && len(p.ChannelIDs) > 0 && slices.Contains(p.ChannelIDs, channelID) {
		return true
	}
	if channelType > 0 && len(p.ChannelTypes) > 0 && slices.Contains(p.ChannelTypes, channelType) {
		return true
	}
	return false
}

type GlobalSettings struct {
	PassThroughRequestEnabled        bool                             `json:"pass_through_request_enabled"`
	ThinkingModelBlacklist           []string                         `json:"thinking_model_blacklist"`
	ChatCompletionsToResponsesPolicy ChatCompletionsToResponsesPolicy `json:"chat_completions_to_responses_policy"`
}

var defaultOpenaiSettings = GlobalSettings{
	PassThroughRequestEnabled: false,
	ThinkingModelBlacklist: []string{
		"moonshotai/kimi-k2-thinking",
		"kimi-k2-thinking",
	},
	ChatCompletionsToResponsesPolicy: ChatCompletionsToResponsesPolicy{
		Enabled:     false,
		AllChannels: true,
	},
}

var globalSettings = defaultOpenaiSettings

type ClaudeSettings struct {
	HeadersSettings                       map[string]map[string][]string `json:"model_headers_settings"`
	DefaultMaxTokens                      map[string]int                 `json:"default_max_tokens"`
	ThinkingAdapterEnabled                bool                           `json:"thinking_adapter_enabled"`
	ThinkingAdapterBudgetTokensPercentage float64                        `json:"thinking_adapter_budget_tokens_percentage"`
}

var defaultClaudeSettings = ClaudeSettings{
	HeadersSettings:        map[string]map[string][]string{},
	ThinkingAdapterEnabled: true,
	DefaultMaxTokens: map[string]int{
		"default": 8192,
	},
	ThinkingAdapterBudgetTokensPercentage: 0.8,
}

var claudeSettings = defaultClaudeSettings

const defaultGeminiSafetySetting = "OFF"

var validGeminiSafetySettings = map[string]struct{}{
	"OFF":                              {},
	"BLOCK_NONE":                       {},
	"BLOCK_ONLY_HIGH":                  {},
	"BLOCK_MEDIUM_AND_ABOVE":           {},
	"BLOCK_LOW_AND_ABOVE":              {},
	"HARM_BLOCK_THRESHOLD_UNSPECIFIED": {},
}

type GeminiSettings struct {
	SafetySettings                        map[string]string `json:"safety_settings"`
	VersionSettings                       map[string]string `json:"version_settings"`
	SupportedImagineModels                []string          `json:"supported_imagine_models"`
	ThinkingAdapterEnabled                bool              `json:"thinking_adapter_enabled"`
	ThinkingAdapterBudgetTokensPercentage float64           `json:"thinking_adapter_budget_tokens_percentage"`
	FunctionCallThoughtSignatureEnabled   bool              `json:"function_call_thought_signature_enabled"`
	RemoveFunctionResponseIdEnabled       bool              `json:"remove_function_response_id_enabled"`
}

var defaultGeminiSettings = GeminiSettings{
	SafetySettings: map[string]string{
		"default": defaultGeminiSafetySetting,
	},
	VersionSettings: map[string]string{
		"default":        "v1beta",
		"gemini-1.0-pro": "v1",
	},
	SupportedImagineModels: []string{
		"gemini-2.0-flash-exp-image-generation",
		"gemini-2.0-flash-exp",
		"gemini-3-pro-image-preview",
		"gemini-3-pro-image",
		"gemini-2.5-flash-image",
		"gemini-3.1-flash-image",
		"gemini-3.1-flash-image-preview",
	},
	ThinkingAdapterEnabled:                false,
	ThinkingAdapterBudgetTokensPercentage: 0.6,
	FunctionCallThoughtSignatureEnabled:   true,
	RemoveFunctionResponseIdEnabled:       true,
}

var geminiSettings = defaultGeminiSettings

type GrokSettings struct {
	ViolationDeductionEnabled bool    `json:"violation_deduction_enabled"`
	ViolationDeductionAmount  float64 `json:"violation_deduction_amount"`
}

var defaultGrokSettings = GrokSettings{
	ViolationDeductionEnabled: true,
	ViolationDeductionAmount:  0.05,
}

var grokSettings = defaultGrokSettings

func GetGrokSettings() *GrokSettings {
	return &grokSettings
}

type QwenSettings struct {
	SyncImageModels []string `json:"sync_image_models"`
}

var defaultQwenSettings = QwenSettings{
	SyncImageModels: []string{
		"z-image",
		"qwen-image",
		"wan2.6",
		"wan2.7",
		"qwen-image-edit",
		"qwen-image-edit-max",
		"qwen-image-edit-max-2026-01-16",
		"qwen-image-edit-plus",
		"qwen-image-edit-plus-2025-12-15",
		"qwen-image-edit-plus-2025-10-30",
	},
}

var qwenSettings = defaultQwenSettings

// Getters for other settings (from sub files)
func GetGlobalSettings() *GlobalSettings {
	return &globalSettings
}

func GetClaudeSettings() *ClaudeSettings {
	// check default max tokens must have default key
	if _, ok := claudeSettings.DefaultMaxTokens["default"]; !ok {
		claudeSettings.DefaultMaxTokens["default"] = 8192
	}
	return &claudeSettings
}

func (c *ClaudeSettings) WriteHeaders(originModel string, httpHeader *http.Header) {
	if headers, ok := c.HeadersSettings[originModel]; ok {
		for headerKey, headerValues := range headers {
			mergedValues := normalizeHeaderListValues(
				append(append([]string(nil), httpHeader.Values(headerKey)...), headerValues...),
			)
			if len(mergedValues) == 0 {
				continue
			}
			httpHeader.Set(headerKey, strings.Join(mergedValues, ","))
		}
	}
}

func normalizeHeaderListValues(values []string) []string {
	normalizedValues := make([]string, 0, len(values))
	seenValues := make(map[string]struct{}, len(values))
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			normalizedItem := strings.TrimSpace(item)
			if normalizedItem == "" {
				continue
			}
			if _, exists := seenValues[normalizedItem]; exists {
				continue
			}
			seenValues[normalizedItem] = struct{}{}
			normalizedValues = append(normalizedValues, normalizedItem)
		}
	}
	return normalizedValues
}

func (c *ClaudeSettings) GetDefaultMaxTokens(model string) int {
	if maxTokens, ok := c.DefaultMaxTokens[model]; ok {
		return maxTokens
	}
	return c.DefaultMaxTokens["default"]
}

// ValidateClaudeDefaultMaxTokens validates the JSON persisted by the option
// API. Zero stays allowed — the current Messages API accepts max_tokens: 0 as
// cache pre-warming — but negative values are rejected because they would
// wrap into huge unsigned values during request conversion.
func ValidateClaudeDefaultMaxTokens(value string) error {
	var settings map[string]int
	if err := common.UnmarshalJsonStr(value, &settings); err != nil {
		return fmt.Errorf("Claude default max tokens must be a JSON map of model to integer: %w", err)
	}
	if settings == nil {
		return fmt.Errorf("Claude default max tokens must be a JSON map of model to integer")
	}
	for model, maxTokens := range settings {
		if maxTokens < 0 {
			return fmt.Errorf("negative Claude default max_tokens %d for %q", maxTokens, model)
		}
	}
	return nil
}

func GetGeminiSettings() *GeminiSettings {
	return &geminiSettings
}

func GetGeminiSafetySetting(key string) string {
	settings := geminiSettings.SafetySettings
	if value := settings[key]; value != "" {
		return value
	}
	if value := settings["default"]; value != "" {
		return value
	}
	return defaultGeminiSafetySetting
}

// ValidateGeminiSafetySettings validates the JSON persisted by the option API.
// Empty values remain valid because read-time fallback returns the default.
func ValidateGeminiSafetySettings(value string) error {
	var settings map[string]string
	if err := common.UnmarshalJsonStr(value, &settings); err != nil {
		return fmt.Errorf("Gemini safety settings must be a JSON string map: %w", err)
	}
	if settings == nil {
		return fmt.Errorf("Gemini safety settings must be a JSON string map")
	}
	for category, threshold := range settings {
		if threshold == "" {
			continue
		}
		if _, ok := validGeminiSafetySettings[threshold]; !ok {
			return fmt.Errorf("invalid Gemini safety threshold %q for %q", threshold, category)
		}
	}
	return nil
}

func GetGeminiVersionSetting(key string) string {
	if value, ok := geminiSettings.VersionSettings[key]; ok {
		return value
	}
	return geminiSettings.VersionSettings["default"]
}

func IsGeminiModelSupportImagine(model string) bool {
	for _, v := range geminiSettings.SupportedImagineModels {
		if v == model {
			return true
		}
	}
	return false
}

func GetQwenSettings() *QwenSettings {
	return &qwenSettings
}

func ShouldPreserveThinkingSuffix(modelName string) bool {
	target := strings.TrimSpace(modelName)
	if target == "" {
		return false
	}
	for _, entry := range globalSettings.ThinkingModelBlacklist {
		if strings.TrimSpace(entry) == target {
			return true
		}
	}
	return false
}

func IsSyncImageModel(model string) bool {
	for _, m := range qwenSettings.SyncImageModels {
		if strings.Contains(model, m) {
			return true
		}
	}
	return false
}

// === ChannelModelHealthSetting from configure_model_health.go (per plan.md for manage_models.go) ===
type ChannelModelHealthSetting struct {
	CalmFastBase             int  `json:"calm_fast_base"`
	CalmFastInterval         int  `json:"calm_fast_interval"`
	CalmSlowBase             int  `json:"calm_slow_base"`
	CalmSlowInterval         int  `json:"calm_slow_interval"`
	DormantBase              int  `json:"dormant_base"`
	DormantInterval          int  `json:"dormant_interval"`
	DormantMaxBase           int  `json:"dormant_max_base"`
	DormantDisableThreshold  int  `json:"dormant_disable_threshold"`
	LocalFailureThreshold    int  `json:"local_failure_threshold"`
	UpstreamFailureThreshold int  `json:"upstream_failure_threshold"`
	CalmWeightScale          int  `json:"calm_weight_scale"`
	DormantWeightScale       int  `json:"dormant_weight_scale"`
	EmergencyThreshold       int  `json:"emergency_threshold"`
	WarningThreshold         int  `json:"warning_threshold"`
	AcceleratedDecayStep     int  `json:"accelerated_decay_step"`
	NormalDecayStep          int  `json:"normal_decay_step"`
	KeyProbeEnabled          bool `json:"key_probe_enabled"`
}

func DefaultChannelModelHealthSetting() *ChannelModelHealthSetting {
	return &ChannelModelHealthSetting{
		CalmFastBase: 3, CalmFastInterval: 3,
		CalmSlowBase: 20, CalmSlowInterval: 20,
		DormantBase: 120, DormantInterval: 120, DormantMaxBase: 360,
		DormantDisableThreshold:  3,
		LocalFailureThreshold:    1,
		UpstreamFailureThreshold: 1,
		CalmWeightScale:          50,
		DormantWeightScale:       10,
		EmergencyThreshold:       20,
		WarningThreshold:         50,
		AcceleratedDecayStep:     2,
		NormalDecayStep:          1,
		KeyProbeEnabled:          true,
	}
}

var channelModelHealthSetting atomic.Pointer[ChannelModelHealthSetting]

var channelModelHealthKeys = map[string]struct{}{
	"LocalFailureThreshold":    {},
	"UpstreamFailureThreshold": {},
	"DormantDisableThreshold":  {},
	"CalmFastBase":             {},
	"CalmFastInterval":         {},
	"CalmSlowBase":             {},
	"CalmSlowInterval":         {},
	"DormantBase":              {},
	"DormantInterval":          {},
	"DormantMaxBase":           {},
	"CalmWeightScale":          {},
	"DormantWeightScale":       {},
	"EmergencyThreshold":       {},
	"WarningThreshold":         {},
	"AcceleratedDecayStep":     {},
	"NormalDecayStep":          {},
	"KeyProbeEnabled":          {},
}

func IsChannelModelHealthOptionKey(key string) bool {
	_, ok := channelModelHealthKeys[key]
	return ok
}

func ValidateChannelModelHealthSettingValue(key, value string) error {
	if !IsChannelModelHealthOptionKey(key) {
		return fmt.Errorf("unknown channel model health option %q", key)
	}
	// KeyProbeEnabled is a boolean, not an integer — bypass the strconv path.
	if key == "KeyProbeEnabled" {
		if value != "true" && value != "false" {
			return fmt.Errorf("%s must be \"true\" or \"false\"", key)
		}
		return nil
	}
	v, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("%s must be an integer", key)
	}
	if key == "LocalFailureThreshold" || key == "UpstreamFailureThreshold" {
		if v < 1 {
			return fmt.Errorf("%s must be a positive integer (>=1)", key)
		}
	} else if key == "CalmWeightScale" || key == "DormantWeightScale" {
		if v < 0 || v > 100 {
			return fmt.Errorf("%s must be a percentage 0-100", key)
		}
	} else if key == "EmergencyThreshold" || key == "WarningThreshold" {
		if v < 0 || v > 100 {
			return fmt.Errorf("%s must be a percentage 0-100", key)
		}
	} else if key == "AcceleratedDecayStep" || key == "NormalDecayStep" {
		if v < 1 {
			return fmt.Errorf("%s must be a positive integer (>=1)", key)
		}
	} else if v < 0 {
		return fmt.Errorf("%s must be a non-negative integer", key)
	}
	return nil
}

func UpdateChannelModelHealthSettingValue(key, value string) error {
	if err := ValidateChannelModelHealthSettingValue(key, value); err != nil {
		return err
	}
	v, _ := strconv.Atoi(value)
	boolVal := value == "true"
	old := channelModelHealthSetting.Load()
	next := *old
	switch key {
	case "CalmFastBase":
		next.CalmFastBase = v
	case "CalmFastInterval":
		next.CalmFastInterval = v
	case "CalmSlowBase":
		next.CalmSlowBase = v
	case "CalmSlowInterval":
		next.CalmSlowInterval = v
	case "DormantBase":
		next.DormantBase = v
	case "DormantInterval":
		next.DormantInterval = v
	case "DormantMaxBase":
		next.DormantMaxBase = v
	case "DormantDisableThreshold":
		next.DormantDisableThreshold = v
	case "LocalFailureThreshold":
		next.LocalFailureThreshold = v
	case "UpstreamFailureThreshold":
		next.UpstreamFailureThreshold = v
	case "CalmWeightScale":
		next.CalmWeightScale = v
	case "DormantWeightScale":
		next.DormantWeightScale = v
	case "EmergencyThreshold":
		next.EmergencyThreshold = v
	case "WarningThreshold":
		next.WarningThreshold = v
	case "AcceleratedDecayStep":
		next.AcceleratedDecayStep = v
	case "NormalDecayStep":
		next.NormalDecayStep = v
	case "KeyProbeEnabled":
		next.KeyProbeEnabled = boolVal
	}
	channelModelHealthSetting.Store(&next)
	return nil
}

func GetChannelModelHealthSetting() *ChannelModelHealthSetting {
	return channelModelHealthSetting.Load()
}

func RestoreChannelModelHealthSetting(s *ChannelModelHealthSetting) {
	channelModelHealthSetting.Store(s)
}

// seedChannelModelHealthOptions returns the map for OnSeedCatalogOptions
// chaining. It reads the live atomic config, not the struct defaults, so the
// option-map snapshot reflects the running values.
func seedChannelModelHealthOptions() map[string]string {
	cfg := GetChannelModelHealthSetting()
	if cfg == nil {
		return nil
	}
	return map[string]string{
		"CalmFastBase":             strconv.Itoa(cfg.CalmFastBase),
		"CalmFastInterval":         strconv.Itoa(cfg.CalmFastInterval),
		"CalmSlowBase":             strconv.Itoa(cfg.CalmSlowBase),
		"CalmSlowInterval":         strconv.Itoa(cfg.CalmSlowInterval),
		"DormantBase":              strconv.Itoa(cfg.DormantBase),
		"DormantInterval":          strconv.Itoa(cfg.DormantInterval),
		"DormantMaxBase":           strconv.Itoa(cfg.DormantMaxBase),
		"DormantDisableThreshold":  strconv.Itoa(cfg.DormantDisableThreshold),
		"LocalFailureThreshold":    strconv.Itoa(cfg.LocalFailureThreshold),
		"UpstreamFailureThreshold": strconv.Itoa(cfg.UpstreamFailureThreshold),
		"CalmWeightScale":          strconv.Itoa(cfg.CalmWeightScale),
		"DormantWeightScale":       strconv.Itoa(cfg.DormantWeightScale),
		"EmergencyThreshold":       strconv.Itoa(cfg.EmergencyThreshold),
		"WarningThreshold":         strconv.Itoa(cfg.WarningThreshold),
		"AcceleratedDecayStep":     strconv.Itoa(cfg.AcceleratedDecayStep),
		"NormalDecayStep":          strconv.Itoa(cfg.NormalDecayStep),
		"KeyProbeEnabled":          strconv.FormatBool(cfg.KeyProbeEnabled),
	}
}

func init() {
	channelModelHealthSetting.Store(DefaultChannelModelHealthSetting())

	// Register model-health hooks (distinct from channel-health to prevent init() overwrite per C1 defect #2).
	// settings package must not import catalog children.
	settings.OnIsChannelModelHealthOptionKey = IsChannelModelHealthOptionKey
	settings.OnValidateChannelModelHealthOption = ValidateChannelModelHealthSettingValue
	settings.OnApplyModelHealthOption = UpdateChannelModelHealthSettingValue

	// Chain the seed hook so the 17 model-health keys reach the option map
	// snapshot, as they did before this domain was flattened. Chained, not
	// assigned, so sibling catalog files' seeds are not overwritten.
	previousSeed := settings.OnSeedCatalogOptions
	settings.OnSeedCatalogOptions = func() map[string]string {
		m := map[string]string{}
		if previousSeed != nil {
			m = previousSeed()
		}
		for k, v := range seedChannelModelHealthOptions() {
			m[k] = v
		}
		return m
	}

	// Register all model settings with settings.GlobalConfig (recovered from original configure_*.go files).
	settings.GlobalConfig.Register("global", &globalSettings)
	settings.GlobalConfig.Register("claude", &claudeSettings)
	settings.GlobalConfig.Register("gemini", &geminiSettings)
	settings.GlobalConfig.Register("grok", &grokSettings)
	settings.GlobalConfig.Register("qwen", &qwenSettings)
}
