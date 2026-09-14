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

	// NaN/±Inf parse without error, so the clamp itself must reject them:
	// the naive int64 conversion of these values is implementation-defined
	// (INT64_MIN on amd64) and would poison /api/status and the option row.
	for _, dirty := range []string{"NaN", "Inf", "+Inf", "-Inf", "1e19", "1e30"} {
		require.NoError(t, settings.ApplyOption("SporeInviterReward", dirty))
		require.Equal(t, int64(0), common.SporeInviterRewardTenths,
			"dirty value %q must clamp to disabled", dirty)
	}
}

// TestApplyOptionInviterRewardCurrency verifies the invite-reward currency
// switch accepts "spore" and "both"; any other value (including garbage from
// a stale client) falls back to "quota" so rewards can never be silently
// routed into an unknown currency.
func TestApplyOptionInviterRewardCurrency(t *testing.T) {
	previousMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousMap })

	previous := common.InviterRewardCurrency
	t.Cleanup(func() { common.InviterRewardCurrency = previous })

	require.NoError(t, settings.ApplyOption("InviterRewardCurrency", "spore"))
	require.Equal(t, "spore", common.InviterRewardCurrency)
	require.Equal(t, "spore", common.OptionMap["InviterRewardCurrency"])

	require.NoError(t, settings.ApplyOption("InviterRewardCurrency", "both"))
	require.Equal(t, "both", common.InviterRewardCurrency)
	require.Equal(t, "both", common.OptionMap["InviterRewardCurrency"])

	for _, fallback := range []string{"quota", "", "usd", "SPORE", "bogus"} {
		require.NoError(t, settings.ApplyOption("InviterRewardCurrency", fallback))
		require.Equal(t, "quota", common.InviterRewardCurrency,
			"value %q must fall back to quota", fallback)
	}
}
