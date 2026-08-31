package channel

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/stretchr/testify/require"
)

func clearPreferredOwnerTables(t *testing.T) {
	t.Helper()
	require.NoError(t, dbx.DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, dbx.DB.Exec("DELETE FROM channels").Error)
}

func insertPreferredOwnerCandidate(
	t *testing.T,
	channelID int,
	modelName string,
	group string,
	channelType int,
	channelStatus int,
	abilityEnabled bool,
) {
	t.Helper()
	require.NoError(t, dbx.DB.Create(&Channel{
		Id:     channelID,
		Type:   channelType,
		Key:    fmt.Sprintf("key-%d", channelID),
		Status: channelStatus,
		Name:   fmt.Sprintf("channel-%d", channelID),
	}).Error)
	require.NoError(t, dbx.DB.Create(&Ability{
		Group:     group,
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   abilityEnabled,
	}).Error)
}

func TestGetPreferredModelOwnerChannelTypes(t *testing.T) {
	const modelName = "gpt-5.4"

	tests := []struct {
		name     string
		setup    func(t *testing.T)
		groups   []string
		expected int
		found    bool
	}{
		{
			name: "openai only",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeOpenAI, common.ChannelStatusEnabled, true)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeOpenAI,
			found:    true,
		},
		{
			name: "codex only",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeCodex, common.ChannelStatusEnabled, true)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeCodex,
			found:    true,
		},
		{
			// Priority and weight used to break this tie; with both columns retired
			// the lowest channel id wins, whichever order the rows were inserted in.
			name: "lowest channel id wins",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeOpenAI, common.ChannelStatusEnabled, true)
				insertPreferredOwnerCandidate(t, 2, modelName, "default", constant.ChannelTypeCodex, common.ChannelStatusEnabled, true)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeOpenAI,
			found:    true,
		},
		{
			name: "insertion order does not decide the owner",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 2, modelName, "default", constant.ChannelTypeCodex, common.ChannelStatusEnabled, true)
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeOpenAI, common.ChannelStatusEnabled, true)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeOpenAI,
			found:    true,
		},
		{
			name: "group filter excludes other groups",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "vip", constant.ChannelTypeCodex, common.ChannelStatusEnabled, true)
				insertPreferredOwnerCandidate(t, 2, modelName, "default", constant.ChannelTypeOpenAI, common.ChannelStatusEnabled, true)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeOpenAI,
			found:    true,
		},
		{
			name: "disabled candidates are ignored",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeCodex, common.ChannelStatusEnabled, false)
				insertPreferredOwnerCandidate(t, 2, modelName, "default", constant.ChannelTypeOpenAI, common.ChannelStatusManuallyDisabled, true)
			},
			groups: []string{"default"},
			found:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearPreferredOwnerTables(t)
			tt.setup(t)

			owners, err := GetPreferredModelOwnerChannelTypes([]string{modelName}, tt.groups)
			require.NoError(t, err)

			got, ok := owners[modelName]
			require.Equal(t, tt.found, ok)
			if tt.found {
				require.Equal(t, tt.expected, got)
			}
		})
	}
}
