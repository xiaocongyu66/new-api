package billing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func confirmPaymentComplianceForTest(t *testing.T) {
	t.Helper()
	paymentSetting := GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = CurrentComplianceTermsVersion
}

func TestStripeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPISecret := StripeApiSecret
	originalWebhookSecret := StripeWebhookSecret
	originalPriceID := StripePriceId
	t.Cleanup(func() {
		StripeApiSecret = originalAPISecret
		StripeWebhookSecret = originalWebhookSecret
		StripePriceId = originalPriceID
	})

	StripeWebhookSecret = ""
	StripeApiSecret = "sk_test_123"
	StripePriceId = "price_123"
	require.False(t, isStripeWebhookEnabled())

	StripeWebhookSecret = "whsec_test"
	require.True(t, isStripeWebhookEnabled())

	StripePriceId = ""
	require.False(t, isStripeWebhookEnabled())
}

func TestCreemWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPIKey := CreemApiKey
	originalProducts := CreemProducts
	originalWebhookSecret := CreemWebhookSecret
	t.Cleanup(func() {
		CreemApiKey = originalAPIKey
		CreemProducts = originalProducts
		CreemWebhookSecret = originalWebhookSecret
	})

	CreemWebhookSecret = ""
	CreemApiKey = "creem_api_key"
	CreemProducts = `[{"productId":"prod_123"}]`
	require.False(t, isCreemWebhookEnabled())

	CreemWebhookSecret = "creem_secret"
	require.True(t, isCreemWebhookEnabled())

	CreemProducts = "[]"
	require.False(t, isCreemWebhookEnabled())
}

func TestWaffoWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalEnabled := WaffoEnabled
	originalSandbox := WaffoSandbox
	originalAPIKey := WaffoApiKey
	originalPrivateKey := WaffoPrivateKey
	originalPublicCert := WaffoPublicCert
	originalSandboxAPIKey := WaffoSandboxApiKey
	originalSandboxPrivateKey := WaffoSandboxPrivateKey
	originalSandboxPublicCert := WaffoSandboxPublicCert
	t.Cleanup(func() {
		WaffoEnabled = originalEnabled
		WaffoSandbox = originalSandbox
		WaffoApiKey = originalAPIKey
		WaffoPrivateKey = originalPrivateKey
		WaffoPublicCert = originalPublicCert
		WaffoSandboxApiKey = originalSandboxAPIKey
		WaffoSandboxPrivateKey = originalSandboxPrivateKey
		WaffoSandboxPublicCert = originalSandboxPublicCert
	})

	WaffoEnabled = true
	WaffoSandbox = false
	WaffoApiKey = ""
	WaffoPrivateKey = "private"
	WaffoPublicCert = "public"
	require.False(t, isWaffoWebhookEnabled())

	WaffoApiKey = "api"
	require.True(t, isWaffoWebhookEnabled())

	WaffoEnabled = false
	require.False(t, isWaffoWebhookEnabled())

	WaffoEnabled = true
	WaffoSandbox = true
	WaffoSandboxApiKey = ""
	WaffoSandboxPrivateKey = "sandbox_private"
	WaffoSandboxPublicCert = "sandbox_public"
	require.False(t, isWaffoWebhookEnabled())

	WaffoSandboxApiKey = "sandbox_api"
	require.True(t, isWaffoWebhookEnabled())
}

func TestWaffoPancakeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalMerchantID := WaffoPancakeMerchantID
	originalPrivateKey := WaffoPancakePrivateKey
	originalProductID := WaffoPancakeProductID
	t.Cleanup(func() {
		WaffoPancakeMerchantID = originalMerchantID
		WaffoPancakePrivateKey = originalPrivateKey
		WaffoPancakeProductID = originalProductID
	})

	// Presence of all three credentials enables the gateway. Webhook public
	// keys are bundled in the SDK and there is no separate Enabled toggle —
	// clear any of the three fields to disable.
	WaffoPancakeMerchantID = ""
	WaffoPancakePrivateKey = "private"
	WaffoPancakeProductID = "product"
	require.False(t, isWaffoPancakeWebhookEnabled())

	WaffoPancakeMerchantID = "merchant"
	require.True(t, isWaffoPancakeWebhookEnabled())

	WaffoPancakeProductID = ""
	require.False(t, isWaffoPancakeWebhookEnabled())

	WaffoPancakeProductID = "product"
	WaffoPancakePrivateKey = ""
	require.False(t, isWaffoPancakeWebhookEnabled())
}

func TestEpayWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalPayAddress := PayAddress
	originalEpayID := EpayId
	originalEpayKey := EpayKey
	originalPayMethods := PayMethods
	t.Cleanup(func() {
		PayAddress = originalPayAddress
		EpayId = originalEpayID
		EpayKey = originalEpayKey
		PayMethods = originalPayMethods
	})

	PayAddress = "https://pay.example.com"
	EpayId = "epay_id"
	EpayKey = ""
	PayMethods = []map[string]string{{"type": "alipay"}}
	require.False(t, isEpayWebhookEnabled())

	EpayKey = "epay_key"
	require.True(t, isEpayWebhookEnabled())

	PayMethods = nil
	require.False(t, isEpayWebhookEnabled())
}
