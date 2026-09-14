package configure_ratio

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateGroupRatioByJSONStringReplacesMapWholesale(t *testing.T) {
	original := GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupRatioByJSONString(original))
	})

	require.NoError(t, UpdateGroupRatioByJSONString(`{"牛奶":1}`))
	assert.Equal(t, 1.0, GetGroupRatio("牛奶"))
	// No hardcoded fallback groups: the map holds exactly what the option
	// contains; unlisted groups fall back to 1.0 at lookup time.
	assert.False(t, ContainsGroupRatio("default"))
	assert.False(t, ContainsGroupRatio("vip"))
	assert.False(t, ContainsGroupRatio("svip"))
	assert.Equal(t, 1.0, GetGroupRatio("default"))
}

func TestUpdateGroupRatioByJSONStringKeepsPreviousMapOnBadJSON(t *testing.T) {
	original := GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupRatioByJSONString(original))
	})

	require.NoError(t, UpdateGroupRatioByJSONString(`{"牛奶":1}`))
	require.Error(t, UpdateGroupRatioByJSONString(`{not-json`))
	assert.True(t, ContainsGroupRatio("牛奶"), "a failed reload must not wipe the previous ratios")
}
