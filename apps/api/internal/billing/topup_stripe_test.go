package billing

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/stretchr/testify/require"
)

func TestStripePayUnits(t *testing.T) {
	originalDiscounts := make(map[int]float64, len(GetPaymentSetting().AmountDiscount))
	for k, v := range GetPaymentSetting().AmountDiscount {
		originalDiscounts[k] = v
	}
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()

	t.Cleanup(func() {
		GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	GetPaymentSetting().AmountDiscount = map[int]float64{
		10: 0.8,
		20: 0,
	}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1.5,"bargain":0.4,"token":0.001}`))

	testCases := []struct {
		name     string
		amount   int64
		group    string
		expected int64
	}{
		{name: "baseline unchanged", amount: 50, group: "default", expected: 50},
		{name: "group ratio scales charged units up", amount: 50, group: "vip", expected: 75},
		{name: "group ratio rounds up to whole units", amount: 5, group: "vip", expected: 8},
		{name: "ratio below one reduces charged units", amount: 50, group: "bargain", expected: 20},
		{name: "discount applies to charged units", amount: 10, group: "default", expected: 8},
		{name: "discount and ratio combine", amount: 10, group: "vip", expected: 12},
		{name: "non-positive discount ignored", amount: 20, group: "default", expected: 20},
		{name: "floors at one unit", amount: 1, group: "token", expected: 1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, stripePayUnits(tc.amount, tc.group))
		})
	}
}

func TestGetStripePayMoneyMatchesChargedUnits(t *testing.T) {
	originalUnitPrice := StripeUnitPrice
	originalQuotaDisplayType := GetGeneralSetting().QuotaDisplayType
	originalDiscounts := make(map[int]float64, len(GetPaymentSetting().AmountDiscount))
	for k, v := range GetPaymentSetting().AmountDiscount {
		originalDiscounts[k] = v
	}
	originalTopupGroupRatio := common.TopupGroupRatio2JSONString()

	t.Cleanup(func() {
		StripeUnitPrice = originalUnitPrice
		GetGeneralSetting().QuotaDisplayType = originalQuotaDisplayType
		GetPaymentSetting().AmountDiscount = originalDiscounts
		require.NoError(t, common.UpdateTopupGroupRatioByJSONString(originalTopupGroupRatio))
	})

	StripeUnitPrice = 8
	GetPaymentSetting().AmountDiscount = map[int]float64{10: 0.8}
	require.NoError(t, common.UpdateTopupGroupRatioByJSONString(`{"default":1,"vip":1.5}`))

	testCases := []struct {
		name             string
		amount           int64
		group            string
		quotaDisplayType string
		expected         float64
	}{
		{name: "baseline", amount: 20, group: "default", quotaDisplayType: QuotaDisplayTypeUSD, expected: 160},
		{name: "group ratio enters checkout amount", amount: 20, group: "vip", quotaDisplayType: QuotaDisplayTypeUSD, expected: 240},
		{name: "discount enters checkout amount", amount: 10, group: "default", quotaDisplayType: QuotaDisplayTypeUSD, expected: 64},
		{name: "ratio rounds up charged units", amount: 5, group: "vip", quotaDisplayType: QuotaDisplayTypeUSD, expected: 64},
		{name: "tokens display divides charged units", amount: int64(common.QuotaPerUnit * 2), group: "vip", quotaDisplayType: QuotaDisplayTypeTokens, expected: 24},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			GetGeneralSetting().QuotaDisplayType = tc.quotaDisplayType
			// 预览金额必须与 Checkout 实收同源：计费 Quantity × 单价。
			require.InDelta(t, tc.expected, getStripePayMoney(float64(tc.amount), tc.group), 0.000001)
		})
	}
}

func TestIsPermanentFulfillmentError(t *testing.T) {
	permanent := []error{
		ErrTopUpNotFound,
		ErrTopUpStatusInvalid,
		ErrPaymentMethodMismatch,
		ErrInvalidTopUpQuota,
		ErrTopUpQuotaLimitExceeded,
		ErrSubscriptionOrderStatusInvalid,
		fmt.Errorf("充值结算失败: %w", ErrTopUpStatusInvalid),
	}
	for _, err := range permanent {
		require.Truef(t, isPermanentFulfillmentError(err), "expect permanent: %v", err)
	}

	transient := []error{
		nil,
		errors.New("database is locked"),
		fmt.Errorf("insert failed: %w", errors.New("connection reset by peer")),
	}
	for _, err := range transient {
		require.Falsef(t, isPermanentFulfillmentError(err), "expect retryable: %v", err)
	}
}
