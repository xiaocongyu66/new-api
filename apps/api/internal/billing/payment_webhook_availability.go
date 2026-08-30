package billing

import (
	"strings"
)

func isPaymentComplianceConfirmed() bool {
	return IsPaymentComplianceConfirmed()
}

func isStripeTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return strings.TrimSpace(StripeApiSecret) != "" &&
		strings.TrimSpace(StripeWebhookSecret) != "" &&
		strings.TrimSpace(StripePriceId) != ""
}

func isStripeWebhookConfigured() bool {
	return strings.TrimSpace(StripeWebhookSecret) != ""
}

func isStripeWebhookEnabled() bool {
	return isStripeTopUpEnabled()
}

func isCreemTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	products := strings.TrimSpace(CreemProducts)
	return strings.TrimSpace(CreemApiKey) != "" &&
		products != "" &&
		products != "[]"
}

func isCreemWebhookConfigured() bool {
	return strings.TrimSpace(CreemWebhookSecret) != ""
}

func isCreemWebhookEnabled() bool {
	return isCreemTopUpEnabled() && isCreemWebhookConfigured()
}

func isWaffoTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	if !WaffoEnabled {
		return false
	}

	return isWaffoWebhookConfigured()
}

func isWaffoWebhookConfigured() bool {
	if WaffoSandbox {
		return strings.TrimSpace(WaffoSandboxApiKey) != "" &&
			strings.TrimSpace(WaffoSandboxPrivateKey) != "" &&
			strings.TrimSpace(WaffoSandboxPublicCert) != ""
	}

	return strings.TrimSpace(WaffoApiKey) != "" &&
		strings.TrimSpace(WaffoPrivateKey) != "" &&
		strings.TrimSpace(WaffoPublicCert) != ""
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
	return strings.TrimSpace(WaffoPancakeMerchantID) != "" &&
		strings.TrimSpace(WaffoPancakePrivateKey) != "" &&
		strings.TrimSpace(WaffoPancakeProductID) != ""
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
	return isEpayWebhookConfigured() && len(PayMethods) > 0
}

func isEpayWebhookConfigured() bool {
	return strings.TrimSpace(PayAddress) != "" &&
		strings.TrimSpace(EpayId) != "" &&
		strings.TrimSpace(EpayKey) != ""
}

func isEpayWebhookEnabled() bool {
	return isEpayTopUpEnabled()
}
