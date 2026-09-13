/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package billing

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/settings"
)

// TestApplyOptionSporeInviterReward verifies the admin-facing spore-unit
// option converts to internal tenths and clamps invalid input: the reward
// credits users.spore, so a misparsed value would mint wrong amounts.
func TestApplyOptionSporeInviterReward(t *testing.T) {
	previousMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousMap })

	previous := common.SporeInviterRewardTenths
	t.Cleanup(func() { common.SporeInviterRewardTenths = previous })

	require.NoError(t, settings.ApplyOption("SporeInviterReward", "0.3"))
	require.Equal(t, int64(3), common.SporeInviterRewardTenths)
	require.Equal(t, "0.3", common.OptionMap["SporeInviterReward"])

	// Rounding to the 0.1 precision.
	require.NoError(t, settings.ApplyOption("SporeInviterReward", "1.55"))
	require.Equal(t, int64(16), common.SporeInviterRewardTenths)

	// Negative and garbage input disable the reward instead of minting.
	require.NoError(t, settings.ApplyOption("SporeInviterReward", "-2"))
	require.Equal(t, int64(0), common.SporeInviterRewardTenths)

	require.NoError(t, settings.ApplyOption("SporeInviterReward", "bogus"))
	require.Equal(t, int64(0), common.SporeInviterRewardTenths)
}
