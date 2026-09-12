package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateTopupGroupRatioByJSONStringKeepsDefaultGroups(t *testing.T) {
	original := TopupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateTopupGroupRatioByJSONString(original))
	})

	require.NoError(t, UpdateTopupGroupRatioByJSONString(`{"牛奶":1}`))
	assert.Equal(t, 1.0, GetTopupGroupRatio("牛奶"))
	assert.True(t, ContainsTopupGroupRatio("default"))
	assert.True(t, ContainsTopupGroupRatio("vip"))
	assert.True(t, ContainsTopupGroupRatio("svip"))
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
