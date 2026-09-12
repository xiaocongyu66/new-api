package configure_ratio

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateGroupRatioByJSONStringKeepsDefaultGroups(t *testing.T) {
	original := GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateGroupRatioByJSONString(original))
	})

	require.NoError(t, UpdateGroupRatioByJSONString(`{"牛奶":1}`))
	assert.Equal(t, 1.0, GetGroupRatio("牛奶"))
	// Hardcoded fallback groups survive option reloads instead of silently
	// drifting to the 1.0 lookup fallback.
	assert.True(t, ContainsGroupRatio("default"))
	assert.True(t, ContainsGroupRatio("vip"))
	assert.True(t, ContainsGroupRatio("svip"))
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
