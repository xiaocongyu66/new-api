package channel

import (
	"testing"

	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupRatioCoverageGapsReportsMissingGroups(t *testing.T) {
	originalRatio := ratio_setting.GroupRatio2JSONString()
	originalTopup := common.TopupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopup))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatio))
	})

	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"牛奶":1}`))
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"牛奶":1}`))

	modelGroupsMap := map[string]*types.Set[string]{
		"glm-5.3-flash": types.NewSet[string](),
	}
	modelGroupsMap["glm-5.3-flash"].Add("牛奶")
	modelGroupsMap["glm-5.3-flash"].Add("芝士")

	missingRatio, missingTopup := groupRatioCoverageGaps(modelGroupsMap)
	assert.Equal(t, []string{"芝士"}, missingRatio)
	assert.Equal(t, []string{"芝士"}, missingTopup)

	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"牛奶":1,"芝士":10}`))
	_, missingTopup = groupRatioCoverageGaps(modelGroupsMap)
	assert.Empty(t, missingTopup)
}
