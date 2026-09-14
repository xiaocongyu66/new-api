package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateTopupGroupRatioByJSONStringReplacesMapWholesale(t *testing.T) {
	original := TopupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateTopupGroupRatioByJSONString(original))
	})

	require.NoError(t, UpdateTopupGroupRatioByJSONString(`{"牛奶":1}`))
	assert.Equal(t, 1.0, GetTopupGroupRatio("牛奶"))
	// No hardcoded fallback groups: the map holds exactly what the option
	// contains; unlisted groups fall back to 1.0 at lookup time.
	assert.False(t, ContainsTopupGroupRatio("default"))
	assert.False(t, ContainsTopupGroupRatio("vip"))
	assert.False(t, ContainsTopupGroupRatio("svip"))
	assert.Equal(t, 1.0, GetTopupGroupRatio("default"))
}

func TestUpdateTopupGroupRatioByJSONStringKeepsPreviousMapOnBadJSON(t *testing.T) {
	original := TopupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateTopupGroupRatioByJSONString(original))
	})

	require.NoError(t, UpdateTopupGroupRatioByJSONString(`{"牛奶":1}`))
	require.Error(t, UpdateTopupGroupRatioByJSONString(`{not-json`))
	assert.True(t, ContainsTopupGroupRatio("牛奶"), "a failed reload must not wipe the previous ratios")
}
