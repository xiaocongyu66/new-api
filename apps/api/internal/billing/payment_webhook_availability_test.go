package billing

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/billing/pay_subscription"
	"github.com/stretchr/testify/require"
)

func confirmPaymentComplianceForTest(t *testing.T) {
	t.Helper()
	paymentSetting := pay_subscription.GetPaymentSetting()
	originalConfirmed := paymentSetting.ComplianceConfirmed
	originalTermsVersion := paymentSetting.ComplianceTermsVersion
	t.Cleanup(func() {
		paymentSetting.ComplianceConfirmed = originalConfirmed
		paymentSetting.ComplianceTermsVersion = originalTermsVersion
	})
	paymentSetting.ComplianceConfirmed = true
	paymentSetting.ComplianceTermsVersion = pay_subscription.CurrentComplianceTermsVersion
}

func TestStripeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPISecret := pay_subscription.StripeApiSecret
	originalWebhookSecret := pay_subscription.StripeWebhookSecret
	originalPriceID := pay_subscription.StripePriceId
	t.Cleanup(func() {
		pay_subscription.StripeApiSecret = originalAPISecret
		pay_subscription.StripeWebhookSecret = originalWebhookSecret
		pay_subscription.StripePriceId = originalPriceID
	})

	pay_subscription.StripeWebhookSecret = ""
	pay_subscription.StripeApiSecret = "sk_test_123"
	pay_subscription.StripePriceId = "price_123"
	require.False(t, isStripeWebhookEnabled())

	pay_subscription.StripeWebhookSecret = "whsec_test"
	require.True(t, isStripeWebhookEnabled())

	pay_subscription.StripePriceId = ""
	require.False(t, isStripeWebhookEnabled())
}

func TestCreemWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalAPIKey := pay_subscription.CreemApiKey
	originalProducts := pay_subscription.CreemProducts
	originalWebhookSecret := pay_subscription.CreemWebhookSecret
	t.Cleanup(func() {
		pay_subscription.CreemApiKey = originalAPIKey
		pay_subscription.CreemProducts = originalProducts
		pay_subscription.CreemWebhookSecret = originalWebhookSecret
	})

	pay_subscription.CreemWebhookSecret = ""
	pay_subscription.CreemApiKey = "creem_api_key"
	pay_subscription.CreemProducts = `[{"productId":"prod_123"}]`
	require.False(t, isCreemWebhookEnabled())

	pay_subscription.CreemWebhookSecret = "creem_secret"
	require.True(t, isCreemWebhookEnabled())

	pay_subscription.CreemProducts = "[]"
	require.False(t, isCreemWebhookEnabled())
}

func TestWaffoWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalEnabled := pay_subscription.WaffoEnabled
	originalSandbox := pay_subscription.WaffoSandbox
	originalAPIKey := pay_subscription.WaffoApiKey
	originalPrivateKey := pay_subscription.WaffoPrivateKey
	originalPublicCert := pay_subscription.WaffoPublicCert
	originalSandboxAPIKey := pay_subscription.WaffoSandboxApiKey
	originalSandboxPrivateKey := pay_subscription.WaffoSandboxPrivateKey
	originalSandboxPublicCert := pay_subscription.WaffoSandboxPublicCert
	t.Cleanup(func() {
		pay_subscription.WaffoEnabled = originalEnabled
		pay_subscription.WaffoSandbox = originalSandbox
		pay_subscription.WaffoApiKey = originalAPIKey
		pay_subscription.WaffoPrivateKey = originalPrivateKey
		pay_subscription.WaffoPublicCert = originalPublicCert
		pay_subscription.WaffoSandboxApiKey = originalSandboxAPIKey
		pay_subscription.WaffoSandboxPrivateKey = originalSandboxPrivateKey
		pay_subscription.WaffoSandboxPublicCert = originalSandboxPublicCert
	})

	pay_subscription.WaffoEnabled = true
	pay_subscription.WaffoSandbox = false
	pay_subscription.WaffoApiKey = ""
	pay_subscription.WaffoPrivateKey = "private"
	pay_subscription.WaffoPublicCert = "public"
	require.False(t, isWaffoWebhookEnabled())

	pay_subscription.WaffoApiKey = "api"
	require.True(t, isWaffoWebhookEnabled())

	pay_subscription.WaffoEnabled = false
	require.False(t, isWaffoWebhookEnabled())

	pay_subscription.WaffoEnabled = true
	pay_subscription.WaffoSandbox = true
	pay_subscription.WaffoSandboxApiKey = ""
	pay_subscription.WaffoSandboxPrivateKey = "sandbox_private"
	pay_subscription.WaffoSandboxPublicCert = "sandbox_public"
	require.False(t, isWaffoWebhookEnabled())

	pay_subscription.WaffoSandboxApiKey = "sandbox_api"
	require.True(t, isWaffoWebhookEnabled())
}

func TestWaffoPancakeWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalMerchantID := pay_subscription.WaffoPancakeMerchantID
	originalPrivateKey := pay_subscription.WaffoPancakePrivateKey
	originalProductID := pay_subscription.WaffoPancakeProductID
	t.Cleanup(func() {
		pay_subscription.WaffoPancakeMerchantID = originalMerchantID
		pay_subscription.WaffoPancakePrivateKey = originalPrivateKey
		pay_subscription.WaffoPancakeProductID = originalProductID
	})

	// Presence of all three credentials enables the gateway. Webhook public
	// keys are bundled in the SDK and there is no separate Enabled toggle —
	// clear any of the three fields to disable.
	pay_subscription.WaffoPancakeMerchantID = ""
	pay_subscription.WaffoPancakePrivateKey = "private"
	pay_subscription.WaffoPancakeProductID = "product"
	require.False(t, isWaffoPancakeWebhookEnabled())

	pay_subscription.WaffoPancakeMerchantID = "merchant"
	require.True(t, isWaffoPancakeWebhookEnabled())

	pay_subscription.WaffoPancakeProductID = ""
	require.False(t, isWaffoPancakeWebhookEnabled())

	pay_subscription.WaffoPancakeProductID = "product"
	pay_subscription.WaffoPancakePrivateKey = ""
	require.False(t, isWaffoPancakeWebhookEnabled())
}

func TestEpayWebhookEnabledRequiresTopUpAndWebhookConfig(t *testing.T) {
	confirmPaymentComplianceForTest(t)
	originalPayAddress := pay_subscription.PayAddress
	originalEpayID := pay_subscription.EpayId
	originalEpayKey := pay_subscription.EpayKey
	originalPayMethods := pay_subscription.PayMethods
	t.Cleanup(func() {
		pay_subscription.PayAddress = originalPayAddress
		pay_subscription.EpayId = originalEpayID
		pay_subscription.EpayKey = originalEpayKey
		pay_subscription.PayMethods = originalPayMethods
	})

	pay_subscription.PayAddress = "https://pay.example.com"
	pay_subscription.EpayId = "epay_id"
	pay_subscription.EpayKey = ""
	pay_subscription.PayMethods = []map[string]string{{"type": "alipay"}}
	require.False(t, isEpayWebhookEnabled())

	pay_subscription.EpayKey = "epay_key"
	require.True(t, isEpayWebhookEnabled())

	pay_subscription.PayMethods = nil
	require.False(t, isEpayWebhookEnabled())
}
