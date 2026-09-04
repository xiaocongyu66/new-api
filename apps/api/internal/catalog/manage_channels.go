package channel

import (
	"strconv"
	"strings"

	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/settings"
)

// DemoSiteEnabled is a flag to enable/disable the demo site mode.
var DemoSiteEnabled = false

// SelfUseModeEnabled is a flag to enable/disable self-use mode.
var SelfUseModeEnabled = false

var AutomaticDisableKeywords = []string{
	"Your credit balance is too low",
	"This organization has been disabled.",
	"You exceeded your current quota",
	"Permission denied",
	"The security token included in the request is invalid",
	"Operation not allowed",
	"Your account is not authorized",
}

// AutomaticDisableKeywordsToString converts the automatic disable keywords to a string.
func AutomaticDisableKeywordsToString() string {
	return strings.Join(AutomaticDisableKeywords, "\n")
}

// AutomaticDisableKeywordsFromString parses automatic disable keywords from a string.
func AutomaticDisableKeywordsFromString(s string) {
	AutomaticDisableKeywords = []string{}
	ak := strings.Split(s, "\n")
	for _, k := range ak {
		k = strings.TrimSpace(k)
		k = strings.ToLower(k)
		if k != "" {
			AutomaticDisableKeywords = append(AutomaticDisableKeywords, k)
		}
	}
}

// applyOperationSetting implements the registered hook for DemoSiteEnabled, SelfUseModeEnabled, AutomaticDisableKeywords.
func applyOperationSetting(key, value string) error {
	boolValue := value == "true"
	switch key {
	case "DemoSiteEnabled":
		DemoSiteEnabled = boolValue
	case "SelfUseModeEnabled":
		SelfUseModeEnabled = boolValue
		ratio_setting.SelfUseModeEnabled = boolValue
	case "AutomaticDisableKeywords":
		AutomaticDisableKeywordsFromString(value)
	}
	return nil
}

// seedOperationOptions returns the map for OnSeedCatalogOptions chaining.
func seedOperationOptions() map[string]string {
	return map[string]string{
		"DemoSiteEnabled":          strconv.FormatBool(DemoSiteEnabled),
		"SelfUseModeEnabled":       strconv.FormatBool(SelfUseModeEnabled),
		"AutomaticDisableKeywords": AutomaticDisableKeywordsToString(),
	}
}

func init() {
	settings.OnApplyOperationSetting = applyOperationSetting

	// Chain the seed hook to combine with other catalog domains without overwriting.
	previousSeed := settings.OnSeedCatalogOptions
	settings.OnSeedCatalogOptions = func() map[string]string {
		m := map[string]string{}
		if previousSeed != nil {
			m = previousSeed()
		}
		for k, v := range seedOperationOptions() {
			m[k] = v
		}
		return m
	}
}
