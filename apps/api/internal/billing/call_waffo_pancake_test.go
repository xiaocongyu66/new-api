package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pancakeTestEvent(storeID, amount, currency string) *WaffoPancakeWebhookEvent {
	return &WaffoPancakeWebhookEvent{
		StoreID: storeID,
		Data: WaffoPancakeWebhookData{
			Amount:   amount,
			Currency: currency,
		},
	}
}

func TestVerifyWaffoPancakeEventStore(t *testing.T) {
	originalStore := WaffoPancakeStoreID
	t.Cleanup(func() { WaffoPancakeStoreID = originalStore })

	t.Run("matching store passes", func(t *testing.T) {
		WaffoPancakeStoreID = "str_live_123"
		require.NoError(t, verifyWaffoPancakeEventStore(pancakeTestEvent("str_live_123", "10.00", "USD"), "T1"))
	})

	t.Run("foreign store rejected", func(t *testing.T) {
		WaffoPancakeStoreID = "str_live_123"
		err := verifyWaffoPancakeEventStore(pancakeTestEvent("str_other_merchant", "10.00", "USD"), "T1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "store mismatch")
	})

	t.Run("unconfigured store skips check", func(t *testing.T) {
		WaffoPancakeStoreID = ""
		require.NoError(t, verifyWaffoPancakeEventStore(pancakeTestEvent("anything", "10.00", "USD"), "T1"))
	})
}

func TestVerifyWaffoPancakeEventMoney(t *testing.T) {
	tests := []struct {
		name          string
		expectedMoney float64
		eventAmount   string
		eventCurrency string
		wantErr       bool
	}{
		{name: "exact match", expectedMoney: 10, eventAmount: "10.00", eventCurrency: "USD"},
		{name: "scale variant match", expectedMoney: 10, eventAmount: "10.0", eventCurrency: "USD"},
		{name: "cents match", expectedMoney: 7.3, eventAmount: "7.30", eventCurrency: "USD"},
		{name: "lowercase currency ok", expectedMoney: 10, eventAmount: "10.00", eventCurrency: "usd"},
		{name: "underpaid rejected", expectedMoney: 10, eventAmount: "9.99", eventCurrency: "USD", wantErr: true},
		{name: "overpaid rejected", expectedMoney: 10, eventAmount: "10.01", eventCurrency: "USD", wantErr: true},
		{name: "empty amount rejected", expectedMoney: 10, eventAmount: "", eventCurrency: "USD", wantErr: true},
		{name: "garbage amount rejected", expectedMoney: 10, eventAmount: "1e999", eventCurrency: "USD", wantErr: true},
		{name: "wrong currency rejected", expectedMoney: 10, eventAmount: "10.00", eventCurrency: "EUR", wantErr: true},
		{name: "empty currency rejected", expectedMoney: 10, eventAmount: "10.00", eventCurrency: "", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyWaffoPancakeEventMoney(
				pancakeTestEvent("str_1", tc.eventAmount, tc.eventCurrency),
				tc.expectedMoney,
				"WAFFO_PANCAKE-1-2-abc",
			)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "WAFFO_PANCAKE-1-2-abc")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
