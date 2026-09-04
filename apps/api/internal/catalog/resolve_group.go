package channel

// Group-resolution rules shared by relay selection, token issuance, and the
// pricing/pricing-adjacent controllers. They read group configuration from
// this package and ratio_setting only, so every consumer (including the
// channel capability) can import them without import cycles.

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/settings"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

var userUsableGroups = map[string]string{
	"default": "默认分组",
	"vip":     "vip分组",
}
var userUsableGroupsMutex sync.RWMutex

func GetUserUsableGroupsCopy() map[string]string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	copyUserUsableGroups := make(map[string]string)
	for k, v := range userUsableGroups {
		copyUserUsableGroups[k] = v
	}
	return copyUserUsableGroups
}

func UserUsableGroups2JSONString() string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	jsonBytes, err := json.Marshal(userUsableGroups)
	if err != nil {
		common.SysLog("error marshalling user groups: " + err.Error())
	}
	return string(jsonBytes)
}

func UpdateUserUsableGroupsByJSONString(jsonStr string) error {
	userUsableGroupsMutex.Lock()
	defer userUsableGroupsMutex.Unlock()

	userUsableGroups = make(map[string]string)
	return json.Unmarshal([]byte(jsonStr), &userUsableGroups)
}

func GetUsableGroupDescription(groupName string) string {
	userUsableGroupsMutex.RLock()
	defer userUsableGroupsMutex.RUnlock()

	if desc, ok := userUsableGroups[groupName]; ok {
		return desc
	}
	return groupName
}

const DefaultMaxTokenAutoGroups = 5

var autoGroups = []string{
	"default",
}

var DefaultUseAutoGroup = false

var maxTokenAutoGroups atomic.Int64

func init() {
	maxTokenAutoGroups.Store(DefaultMaxTokenAutoGroups)
}

func UpdateAutoGroupsByJsonString(jsonString string) error {
	autoGroups = make([]string, 0)
	return common.Unmarshal([]byte(jsonString), &autoGroups)
}

func AutoGroups2JsonString() string {
	jsonBytes, err := common.Marshal(autoGroups)
	if err != nil {
		return "[]"
	}
	return string(jsonBytes)
}

func GetAutoGroups() []string {
	return autoGroups
}

func GetMaxTokenAutoGroups() int {
	return int(maxTokenAutoGroups.Load())
}

// ValidateMaxTokenAutoGroups delegates to settings, which owns option
// validation, so the rule cannot drift between the two packages.
func ValidateMaxTokenAutoGroups(value string) error {
	return settings.ValidateOptionValue("MaxTokenAutoGroups", value)
}

func UpdateMaxTokenAutoGroups(value string) error {
	if err := ValidateMaxTokenAutoGroups(value); err != nil {
		return err
	}
	maxCount, _ := strconv.Atoi(value)
	maxTokenAutoGroups.Store(int64(maxCount))
	return nil
}

func GetUserUsableGroups(userGroup string) map[string]string {
	groupsCopy := GetUserUsableGroupsCopy()
	if userGroup != "" {
		specialSettings, b := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.Get(userGroup)
		if b {
			// 处理特殊可用分组
			for specialGroup, desc := range specialSettings {
				if strings.HasPrefix(specialGroup, "-:") {
					// 移除分组
					groupToRemove := strings.TrimPrefix(specialGroup, "-:")
					delete(groupsCopy, groupToRemove)
				} else if strings.HasPrefix(specialGroup, "+:") {
					// 添加分组
					groupToAdd := strings.TrimPrefix(specialGroup, "+:")
					groupsCopy[groupToAdd] = desc
				} else {
					// 直接添加分组
					groupsCopy[specialGroup] = desc
				}
			}
		}
		// 如果userGroup不在UserUsableGroups中，返回UserUsableGroups + userGroup
		if _, ok := groupsCopy[userGroup]; !ok {
			groupsCopy[userGroup] = "用户分组"
		}
	}
	return groupsCopy
}

func GroupInUserUsableGroups(userGroup, groupName string) bool {
	_, ok := GetUserUsableGroups(userGroup)[groupName]
	return ok
}

func IsUserSelectableGroup(userGroup, groupName string) bool {
	if groupName == "" || groupName == "auto" {
		return false
	}
	return GroupInUserUsableGroups(userGroup, groupName) && ratio_setting.ContainsGroupRatio(groupName)
}

// GetUserAutoGroup 根据用户分组获取自动分组设置
func GetUserAutoGroup(userGroup string) []string {
	autoGroups := make([]string, 0)
	seen := make(map[string]struct{})
	for _, group := range GetAutoGroups() {
		if !IsUserSelectableGroup(userGroup, group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		autoGroups = append(autoGroups, group)
	}
	return autoGroups
}

// FilterUserTokenAutoGroups applies current permissions before the current
// per-token limit. It intentionally does not fall back to the global Auto list.
func FilterUserTokenAutoGroups(userGroup string, groups []string) []string {
	maxCount := GetMaxTokenAutoGroups()
	filtered := make([]string, 0, min(len(groups), maxCount))
	seen := make(map[string]struct{})
	for _, group := range groups {
		if !IsUserSelectableGroup(userGroup, group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		filtered = append(filtered, group)
		if len(filtered) == maxCount {
			break
		}
	}
	return filtered
}

// GetRequestAutoGroups resolves the ordered Auto groups for the current token.
// The absence of the context value means that the token inherits the complete
// global Auto list; a present (even empty) value is an explicit token snapshot.
func GetRequestAutoGroups(c contract.Context, userGroup string) []string {
	value, ok := common.GetCtxKey(c, constant.ContextKeyTokenAutoGroups)
	if !ok {
		return GetUserAutoGroup(userGroup)
	}
	groups, ok := value.([]string)
	if !ok {
		return []string{}
	}
	return FilterUserTokenAutoGroups(userGroup, groups)
}

// applyResolveGroupSetting implements the registered hook for DefaultUseAutoGroup,
// UserUsableGroups, AutoGroups, MaxTokenAutoGroups per the settings integration
// pattern in manage_channels.go and track_health.go.
func applyResolveGroupSetting(key, value string) error {
	switch key {
	case "DefaultUseAutoGroup":
		DefaultUseAutoGroup = value == "true"
		return nil
	case "UserUsableGroups":
		return UpdateUserUsableGroupsByJSONString(value)
	case "AutoGroups":
		return UpdateAutoGroupsByJsonString(value)
	case "MaxTokenAutoGroups":
		return UpdateMaxTokenAutoGroups(value)
	}
	return nil
}

// seedResolveGroupOptions returns the map for OnSeedCatalogOptions chaining
// to match the pattern used by manage_channels.go.
func seedResolveGroupOptions() map[string]string {
	return map[string]string{
		"DefaultUseAutoGroup": strconv.FormatBool(DefaultUseAutoGroup),
		"UserUsableGroups":    UserUsableGroups2JSONString(),
		"AutoGroups":          AutoGroups2JsonString(),
		"MaxTokenAutoGroups":  strconv.Itoa(GetMaxTokenAutoGroups()),
	}
}

func init() {
	settings.OnApplyResolveGroupSetting = applyResolveGroupSetting

	// Group membership lookups used by the identity domain. identity must not
	// import this package (it would close a cycle: catalog -> identity -> catalog).
	identity.OnGetMaxTokenAutoGroups = GetMaxTokenAutoGroups
	identity.OnIsUserSelectableGroup = IsUserSelectableGroup
	identity.OnGetUserAutoGroup = GetUserAutoGroup
	identity.OnGetUserUsableGroups = GetUserUsableGroups
	identity.OnDefaultUseAutoGroup = func() bool { return DefaultUseAutoGroup }

	// Chain the seed hook to combine with other catalog domains without overwriting.
	previousSeed := settings.OnSeedCatalogOptions
	settings.OnSeedCatalogOptions = func() map[string]string {
		m := map[string]string{}
		if previousSeed != nil {
			m = previousSeed()
		}
		for k, v := range seedResolveGroupOptions() {
			m[k] = v
		}
		return m
	}
}
