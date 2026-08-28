package billing

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/billing/pay_subscription"
)

func isPaymentComplianceConfirmed() bool {
	return pay_subscription.IsPaymentComplianceConfirmed()
}

func isStripeTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return strings.TrimSpace(pay_subscription.StripeApiSecret) != "" &&
		strings.TrimSpace(pay_subscription.StripeWebhookSecret) != "" &&
		strings.TrimSpace(pay_subscription.StripePriceId) != ""
}

func isStripeWebhookConfigured() bool {
	return strings.TrimSpace(pay_subscription.StripeWebhookSecret) != ""
}

func isStripeWebhookEnabled() bool {
	return isStripeTopUpEnabled()
}

func isCreemTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	products := strings.TrimSpace(pay_subscription.CreemProducts)
	return strings.TrimSpace(pay_subscription.CreemApiKey) != "" &&
		products != "" &&
		products != "[]"
}

func isCreemWebhookConfigured() bool {
	return strings.TrimSpace(pay_subscription.CreemWebhookSecret) != ""
}

func isCreemWebhookEnabled() bool {
	return isCreemTopUpEnabled() && isCreemWebhookConfigured()
}

func isWaffoTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	if !pay_subscription.WaffoEnabled {
		return false
	}

	return isWaffoWebhookConfigured()
}

func isWaffoWebhookConfigured() bool {
	if pay_subscription.WaffoSandbox {
		return strings.TrimSpace(pay_subscription.WaffoSandboxApiKey) != "" &&
			strings.TrimSpace(pay_subscription.WaffoSandboxPrivateKey) != "" &&
			strings.TrimSpace(pay_subscription.WaffoSandboxPublicCert) != ""
	}

	return strings.TrimSpace(pay_subscription.WaffoApiKey) != "" &&
		strings.TrimSpace(pay_subscription.WaffoPrivateKey) != "" &&
		strings.TrimSpace(pay_subscription.WaffoPublicCert) != ""
}

func isWaffoWebhookEnabled() bool {
	return isWaffoTopUpEnabled()
}

func isWaffoPancakeTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	// Presence-of-credentials = enabled. Webhook public keys ship inside
	// the SDK; mode (test/prod) is read from each event.
	return strings.TrimSpace(pay_subscription.WaffoPancakeMerchantID) != "" &&
		strings.TrimSpace(pay_subscription.WaffoPancakePrivateKey) != "" &&
		strings.TrimSpace(pay_subscription.WaffoPancakeProductID) != ""
}

func isWaffoPancakeWebhookConfigured() bool {
	return isWaffoPancakeTopUpEnabled()
}

func isWaffoPancakeWebhookEnabled() bool {
	return isWaffoPancakeTopUpEnabled()
}

func isEpayTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return isEpayWebhookConfigured() && len(pay_subscription.PayMethods) > 0
}

func isEpayWebhookConfigured() bool {
	return strings.TrimSpace(pay_subscription.PayAddress) != "" &&
		strings.TrimSpace(pay_subscription.EpayId) != "" &&
		strings.TrimSpace(pay_subscription.EpayKey) != ""
}

func isEpayWebhookEnabled() bool {
	return isEpayTopUpEnabled()
}
