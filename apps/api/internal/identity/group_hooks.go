package identity

// Group membership rules live in the catalog domain, which imports this package,
// so the lookups are injected here for the same reason as GroupModelsResolver in
// audit_hooks.go. Registered from catalog/resolve_group.go's init().
//
// Nil-safe defaults match the behavior of an installation with no usable-group
// configuration loaded: no auto groups, no selectable groups, auto-group opt-in
// off. That is the same state these accessors returned before any option was
// applied.
var (
	OnGetMaxTokenAutoGroups func() int
	OnIsUserSelectableGroup func(userGroup, groupName string) bool
	OnGetUserAutoGroup      func(userGroup string) []string
	OnGetUserUsableGroups   func(userGroup string) map[string]string
	OnDefaultUseAutoGroup   func() bool
)

func maxTokenAutoGroups() int {
	if OnGetMaxTokenAutoGroups == nil {
		return 0
	}
	return OnGetMaxTokenAutoGroups()
}

func isUserSelectableGroup(userGroup, groupName string) bool {
	return OnIsUserSelectableGroup != nil && OnIsUserSelectableGroup(userGroup, groupName)
}

func userAutoGroup(userGroup string) []string {
	if OnGetUserAutoGroup == nil {
		return nil
	}
	return OnGetUserAutoGroup(userGroup)
}

func userUsableGroups(userGroup string) map[string]string {
	if OnGetUserUsableGroups == nil {
		return map[string]string{}
	}
	return OnGetUserUsableGroups(userGroup)
}

func defaultUseAutoGroup() bool {
	return OnDefaultUseAutoGroup != nil && OnDefaultUseAutoGroup()
}
