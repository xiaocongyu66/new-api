package usage

import (
	"strconv"

	"github.com/QuantumNous/new-api/internal/settings"
)

// Chat and midjourney option seeding/application belong to this domain; settings
// keeps only the generic option mechanism and reaches them through these
// nil-safe hooks (same convention as catalog/resolve_group.go).
func seedUsageOptions() map[string]string {
	return map[string]string{
		"Chats":                       Chats2JsonString(),
		"MjNotifyEnabled":             strconv.FormatBool(MjNotifyEnabled),
		"MjAccountFilterEnabled":      strconv.FormatBool(MjAccountFilterEnabled),
		"MjModeClearEnabled":          strconv.FormatBool(MjModeClearEnabled),
		"MjForwardUrlEnabled":         strconv.FormatBool(MjForwardUrlEnabled),
		"MjActionCheckSuccessEnabled": strconv.FormatBool(MjActionCheckSuccessEnabled),
	}
}

func applyUsageOption(key, value string) error {
	boolValue := value == "true"
	switch key {
	case "Chats":
		return UpdateChatsByJsonString(value)
	case "MjNotifyEnabled":
		MjNotifyEnabled = boolValue
	case "MjAccountFilterEnabled":
		MjAccountFilterEnabled = boolValue
	case "MjModeClearEnabled":
		MjModeClearEnabled = boolValue
	case "MjForwardUrlEnabled":
		MjForwardUrlEnabled = boolValue
	case "MjActionCheckSuccessEnabled":
		MjActionCheckSuccessEnabled = boolValue
	}
	return nil
}

func init() {
	settings.OnSeedUsageOptions = seedUsageOptions
	settings.OnApplyUsageOption = applyUsageOption
}
