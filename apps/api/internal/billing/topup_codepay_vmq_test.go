package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVmqNotifySignFormula(t *testing.T) {
	payId := "T1"
	param := "topup"
	payType := "2"
	price := "10.00"
	reallyPrice := "10.01"
	key := "k"

	expected := vmqMd5(payId + param + payType + price + reallyPrice + key)
	got := vmqNotifySign(payId, param, payType, price, reallyPrice, key)
	require.Equal(t, expected, got)

	assert.True(t, constantTimeCompare(got, expected))

	// Tampering reallyPrice must invalidate the signature.
	tampered := vmqNotifySign(payId, param, payType, price, "9.99", key)
	assert.NotEqual(t, expected, tampered)
	assert.False(t, constantTimeCompare(tampered, expected))

	// Tampering price must invalidate the signature.
	tamperedPrice := vmqNotifySign(payId, param, payType, "9.99", reallyPrice, key)
	assert.NotEqual(t, expected, tamperedPrice)
	assert.False(t, constantTimeCompare(tamperedPrice, expected))
}

func TestCodePaySignOrderIndependence(t *testing.T) {
	key := "secret"
	paramsA := map[string]string{
		"id":         "mch_123",
		"type":       "1",
		"price":      "10.00",
		"pay_id":     "CPUSR1NOabc",
		"param":      "topup",
		"notify_url": "https://example.com/notify",
		"return_url": "https://example.com/return",
		"sign":       "",
		"empty":      "",
	}
	paramsB := map[string]string{
		"return_url": "https://example.com/return",
		"notify_url": "https://example.com/notify",
		"pay_id":     "CPUSR1NOabc",
		"param":      "topup",
		"price":      "10.00",
		"type":       "1",
		"id":         "mch_123",
		"sign":       "ignored",
		"empty":      "",
	}

	signA := codepaySign(paramsA, key)
	signB := codepaySign(paramsB, key)
	require.Equal(t, signA, signB)

	// The expected sign is md5 over the sorted raw k=v join plus key.
	expectedJoin := "id=mch_123&notify_url=https://example.com/notify&param=topup&pay_id=CPUSR1NOabc&price=10.00&return_url=https://example.com/return&type=1"
	assert.Equal(t, vmqMd5(expectedJoin+key), signA)

	// Tampering any included value must change the sign.
	tampered := map[string]string(paramsA)
	tampered["price"] = "10.01"
	assert.NotEqual(t, signA, codepaySign(tampered, key))
}

func TestRechargeTopUpVmqAmountMismatch(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 601, 0)
	tradeNo := "VMQTESTMISMATCH"
	topUp := &TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           10.00,
		TradeNo:         tradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderVmq,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, dbx.DB.Create(topUp).Error)

	alreadyDone, err := rechargeTopUp(tradeNo, PaymentProviderVmq, "", "10.01", "", 0, "127.0.0.1")
	assert.ErrorIs(t, err, ErrPaymentAmountMismatch)
	assert.False(t, alreadyDone)

	reloaded := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, common.TopUpStatusPending, reloaded.Status)
	assert.Zero(t, reloaded.CompleteTime)
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestRechargeTopUpVmqCreditsExactlyOnce(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 602, 0)
	tradeNo := "VMQTESTONCE"
	topUp := &TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           10.00,
		TradeNo:         tradeNo,
		PaymentMethod:   "wxpay",
		PaymentProvider: PaymentProviderVmq,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, dbx.DB.Create(topUp).Error)

	alreadyDone, err := rechargeTopUp(tradeNo, PaymentProviderVmq, "", "10.00", "", 0, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))

	reloaded := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	assert.NotZero(t, reloaded.CompleteTime)

	// Repeat callback is a no-op (idempotent), no double credit.
	alreadyDone, err = rechargeTopUp(tradeNo, PaymentProviderVmq, "", "10.00", "", 0, "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))
}

func TestRechargeTopUpVmqReallyPriceTolerance(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 603, 0)
	tradeNo := "VMQTESTREALLY"
	topUp := &TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           10.00,
		TradeNo:         tradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderVmq,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, dbx.DB.Create(topUp).Error)

	// reallyPrice far below order money → rejected, order stays pending, no credit.
	alreadyDone, err := rechargeTopUp(tradeNo, PaymentProviderVmq, "", "10.00", "0.01", 0.10, "127.0.0.1")
	assert.ErrorIs(t, err, ErrPaymentAmountMismatch)
	assert.False(t, alreadyDone)
	reloaded := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, common.TopUpStatusPending, reloaded.Status)
	assert.Zero(t, reloaded.CompleteTime)
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, user.Id))

	// reallyPrice = money + 0.01 (within tolerance) → accepted and credited.
	alreadyDone, err = rechargeTopUp(tradeNo, PaymentProviderVmq, "", "10.00", "10.01", 0.10, "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	assert.Equal(t, 2*500000, getUserQuotaForPaymentGuardTest(t, user.Id))

	reloaded = GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, common.TopUpStatusSuccess, reloaded.Status)
	assert.NotZero(t, reloaded.CompleteTime)
}

func TestRechargeTopUpRejectsProviderMismatch(t *testing.T) {
	truncateTables(t)

	oldQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = oldQuotaPerUnit })

	user := insertUserForPaymentGuardTest(t, 603, 0)
	tradeNo := "EPAYTESTFOREIGN"
	epayOrder := &TopUp{
		UserId:          user.Id,
		Amount:          2,
		Money:           10.00,
		TradeNo:         tradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, dbx.DB.Create(epayOrder).Error)

	_, err := rechargeTopUp(tradeNo, PaymentProviderVmq, "", "10.00", "", 0, "127.0.0.1")
	assert.ErrorIs(t, err, ErrPaymentMethodMismatch)
	assert.Equal(t, 0, getUserQuotaForPaymentGuardTest(t, user.Id))

	reloaded := GetTopUpByTradeNo(tradeNo)
	require.NotNil(t, reloaded)
	assert.Equal(t, common.TopUpStatusPending, reloaded.Status)
}

func TestVmqCreateSignFormula(t *testing.T) {
	payId := "T1"
	param := "topup"
	payType := "2"
	price := "10.00"
	key := "k"

	expected := vmqMd5(payId + param + payType + price + key)
	got := vmqCreateSign(payId, param, payType, price, key)
	require.Equal(t, expected, got)
}

func TestDecimalCompareAtCentLevel(t *testing.T) {
	// 10.001 rounds to 10.00 → matches; 10.005 rounds half away from zero → 10.01 → mismatch.
	match := decimal.NewFromFloat(10.001).Round(2).Equals(decimal.NewFromFloat(10.00).Round(2))
	assert.True(t, match)

	mismatch := decimal.NewFromFloat(10.005).Round(2).Equals(decimal.NewFromFloat(10.00).Round(2))
	assert.False(t, mismatch)
}
