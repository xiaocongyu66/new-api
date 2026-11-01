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

func isWaffoPancakeWebhookEnabled() bool {
	return isWaffoPancakeTopUpEnabled()
}

func isEpayTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return isEpayWebhookConfigured() && countPayMethodsByGateway("epay") > 0
}

func isEpayWebhookConfigured() bool {
	return strings.TrimSpace(PayAddress) != "" &&
		strings.TrimSpace(EpayId) != "" &&
		strings.TrimSpace(EpayKey) != ""
}

func isEpayWebhookEnabled() bool {
	return isEpayTopUpEnabled()
}

func isVmqGatewayConfigured() bool {
	return strings.TrimSpace(VmqAddress) != "" &&
		strings.TrimSpace(VmqKey) != ""
}

func isCodePayGatewayConfigured() bool {
	return strings.TrimSpace(CodePayAddress) != "" &&
		strings.TrimSpace(CodePayId) != "" &&
		strings.TrimSpace(CodePayKey) != ""
}

// countPayMethodsByGateway returns how many configured pay methods route to
// the given gateway. Methods without a gateway count as Epay.
func countPayMethodsByGateway(gateway string) int {
	n := 0
	for _, m := range PayMethods {
		if payMethodGateway(m) == gateway {
			n++
		}
	}
	return n
}

func isVmqTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return isVmqGatewayConfigured() && countPayMethodsByGateway("vmq") > 0
}

func isVmqWebhookEnabled() bool {
	// Notify handlers stay gated by credentials only, not by method presence:
	// in-flight orders must complete even if an admin removes the method.
	return isVmqGatewayConfigured()
}

func isCodePayTopUpEnabled() bool {
	if !isPaymentComplianceConfirmed() {
		return false
	}
	return isCodePayGatewayConfigured() && countPayMethodsByGateway("codepay") > 0
}

func isCodePayWebhookEnabled() bool {
	return isCodePayGatewayConfigured()
}
