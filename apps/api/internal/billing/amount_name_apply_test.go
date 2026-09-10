package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/settings"

	"github.com/stretchr/testify/require"
)

// TestApplyOptionSyncsAmountName verifies that saving the
// general_setting.amount_name option (the 金额名称: the name shown for
// 充值/兑换/支付 amounts, independent of the 额度 consumption currency) is
// applied to the in-memory GeneralSetting that /api/status exposes. A missed
// in-memory sync would leave GetGeneralSetting().AmountName at its default and
// the admin's configured name would never reach the frontend payment displays.
func TestApplyOptionSyncsAmountName(t *testing.T) {
	previousMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousMap })

	previous := GetGeneralSetting().AmountName
	t.Cleanup(func() { GetGeneralSetting().AmountName = previous })

	require.NoError(t, settings.ApplyOption("general_setting.amount_name", "稀有气体"))
	require.Equal(t, "稀有气体", GetGeneralSetting().AmountName)
	require.Equal(t, "稀有气体", common.OptionMap["general_setting.amount_name"])

	// Clearing the option restores the empty (default) name.
	require.NoError(t, settings.ApplyOption("general_setting.amount_name", ""))
	require.Equal(t, "", GetGeneralSetting().AmountName)
}

// TestApplyOptionSyncsAmountUnit verifies the payment amount unit selector
// (usd/cny/custom) applies to the in-memory GeneralSetting and that empty or
// unknown stored values normalize to "usd" in what /api/status exposes.
func TestApplyOptionSyncsAmountUnit(t *testing.T) {
	previousMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousMap })

	previous := GetGeneralSetting().AmountUnit
	t.Cleanup(func() { GetGeneralSetting().AmountUnit = previous })

	for _, unit := range []string{"usd", "cny", "custom"} {
		require.NoError(t, settings.ApplyOption("general_setting.amount_unit", unit))
		require.Equal(t, unit, GetGeneralSetting().AmountUnit)
		require.Equal(t, unit, GetGeneralSetting().AmountUnitEffective())
	}

	require.NoError(t, settings.ApplyOption("general_setting.amount_unit", ""))
	require.Equal(t, "usd", GetGeneralSetting().AmountUnitEffective())

	require.NoError(t, settings.ApplyOption("general_setting.amount_unit", "bogus"))
	require.Equal(t, "usd", GetGeneralSetting().AmountUnitEffective())
}
