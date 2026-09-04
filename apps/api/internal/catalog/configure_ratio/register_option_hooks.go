package configure_ratio

import (
	"strconv"

	"github.com/QuantumNous/new-api/internal/settings"
)

// Ratio option seeding and application belong to this package; settings keeps
// only the generic option mechanism and reaches them through these nil-safe
// hooks (same convention as catalog/resolve_group.go).
func seedRatioOptions() map[string]string {
	return map[string]string{
		"ModelRatio":           ModelRatio2JSONString(),
		"ModelPrice":           ModelPrice2JSONString(),
		"CacheRatio":           CacheRatio2JSONString(),
		"CreateCacheRatio":     CreateCacheRatio2JSONString(),
		"GroupRatio":           GroupRatio2JSONString(),
		"GroupGroupRatio":      GroupGroupRatio2JSONString(),
		"CompletionRatio":      CompletionRatio2JSONString(),
		"ImageRatio":           ImageRatio2JSONString(),
		"AudioRatio":           AudioRatio2JSONString(),
		"AudioCompletionRatio": AudioCompletionRatio2JSONString(),
		"ExposeRatioEnabled":   strconv.FormatBool(IsExposeRatioEnabled()),
	}
}

func applyRatioOption(key, value string) error {
	switch key {
	case "ModelRatio":
		return UpdateModelRatioByJSONString(value)
	case "ModelPrice":
		return UpdateModelPriceByJSONString(value)
	case "CacheRatio":
		return UpdateCacheRatioByJSONString(value)
	case "CreateCacheRatio":
		return UpdateCreateCacheRatioByJSONString(value)
	case "GroupRatio":
		return UpdateGroupRatioByJSONString(value)
	case "GroupGroupRatio":
		return UpdateGroupGroupRatioByJSONString(value)
	case "CompletionRatio":
		return UpdateCompletionRatioByJSONString(value)
	case "ImageRatio":
		return UpdateImageRatioByJSONString(value)
	case "AudioRatio":
		return UpdateAudioRatioByJSONString(value)
	case "AudioCompletionRatio":
		return UpdateAudioCompletionRatioByJSONString(value)
	case "ExposeRatioEnabled":
		SetExposeRatioEnabled(value == "true")
	}
	return nil
}

func init() {
	settings.OnSeedRatioOptions = seedRatioOptions
	settings.OnApplyRatioOption = applyRatioOption
}
