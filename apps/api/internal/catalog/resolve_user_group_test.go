package channel

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/catalog/resolve_group"
	resolvegroup "github.com/QuantumNous/new-api/internal/catalog/resolve_group"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"testing"

	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configureRequestAutoGroupsTest(t *testing.T) {
	t.Helper()
	originalMax := resolvegroup.GetMaxTokenAutoGroups()
	originalAutoGroups := resolvegroup.AutoGroups2JsonString()
	originalUsableGroups := resolvegroup.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, resolvegroup.UpdateMaxTokenAutoGroups("2"))
	require.NoError(t, resolve_group.UpdateAutoGroupsByJsonString(`["vip","default","svip"]`))
	require.NoError(t, resolve_group.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP","svip":"SVIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1,"svip":1}`))
	t.Cleanup(func() {
		require.NoError(t, resolvegroup.UpdateMaxTokenAutoGroups(fmt.Sprintf("%d", originalMax)))
		require.NoError(t, resolve_group.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, resolve_group.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})
}

func newRequestAutoGroupsContext() contract.Context {
	ctx, _ := ginadapter.NewSyntheticContext(nil)
	return ctx
}

func TestGetRequestAutoGroupsInheritedListIsNotLimited(t *testing.T) {
	configureRequestAutoGroupsTest(t)
	ctx := newRequestAutoGroupsContext()

	groups := resolve_group.GetRequestAutoGroups(ctx, "default")

	assert.Equal(t, []string{"vip", "default", "svip"}, groups)
}

func TestGetRequestAutoGroupsFiltersBeforeApplyingCurrentLimit(t *testing.T) {
	configureRequestAutoGroupsTest(t)
	ctx := newRequestAutoGroupsContext()
	common.SetCtxKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"revoked", "vip", "default", "svip"})
	require.NoError(t, resolve_group.UpdateAutoGroupsByJsonString(`[]`))

	groups := resolve_group.GetRequestAutoGroups(ctx, "default")

	assert.Equal(t, []string{"vip", "default"}, groups)
	require.NoError(t, resolvegroup.UpdateMaxTokenAutoGroups("1"))
	assert.Equal(t, []string{"vip"}, resolve_group.GetRequestAutoGroups(ctx, "default"))
}

func TestGetRequestAutoGroupsDoesNotFallBackAfterPermissionChange(t *testing.T) {
	configureRequestAutoGroupsTest(t)
	ctx := newRequestAutoGroupsContext()
	common.SetCtxKey(ctx, constant.ContextKeyTokenAutoGroups, []string{"vip"})
	require.NoError(t, resolve_group.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))

	groups := resolve_group.GetRequestAutoGroups(ctx, "default")

	assert.Empty(t, groups)
}
