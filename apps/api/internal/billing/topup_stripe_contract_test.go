package billing

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/QuantumNous/new-api/internal/billing/pay_subscription"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestStripeWebhookRejectsDisabledViaForbidden pins the contract when Stripe
// webhook is not configured — HTTP 403 with no body.
func TestStripeWebhookRejectsDisabledViaForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevSecret := pay_subscription.StripeWebhookSecret
	pay_subscription.StripeWebhookSecret = ""
	t.Cleanup(func() { pay_subscription.StripeWebhookSecret = prevSecret })

	req := httptest.NewRequest(http.MethodPost, "/api/stripe/webhook", nil)
	c, rec := ginadapter.NewSyntheticContext(req)

	StripeWebhook(c)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, rec.Body.String())
}

// TestStripeWebhookAcceptsValidSignatureAndReturnsOK constructs a minimal
// checkout.session.completed event, signs it with the test secret, and asserts
// the handler returns 200 OK.
func TestStripeWebhookAcceptsValidSignatureAndReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Setup all required settings for Stripe webhook to be enabled
	prevCompliance := pay_subscription.GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := pay_subscription.GetPaymentSetting().ComplianceTermsVersion
	prevApiSecret := pay_subscription.StripeApiSecret
	prevSecret := pay_subscription.StripeWebhookSecret
	prevPriceId := pay_subscription.StripePriceId
	t.Cleanup(func() {
		pay_subscription.GetPaymentSetting().ComplianceConfirmed = prevCompliance
		pay_subscription.GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		pay_subscription.StripeApiSecret = prevApiSecret
		pay_subscription.StripeWebhookSecret = prevSecret
		pay_subscription.StripePriceId = prevPriceId
	})
	pay_subscription.GetPaymentSetting().ComplianceConfirmed = true
	pay_subscription.GetPaymentSetting().ComplianceTermsVersion = pay_subscription.CurrentComplianceTermsVersion
	secret := "whsec_test_secret"
	pay_subscription.StripeApiSecret = "sk_test_123"
	pay_subscription.StripeWebhookSecret = secret
	pay_subscription.StripePriceId = "price_123"

	// Minimal checkout.session.completed payload
	payload := []byte(`{
		"id": "evt_test_webhook",
		"object": "event",
		"type": "checkout.session.completed",
		"data": {
			"object": {
				"id": "cs_test_123",
				"object": "checkout.session",
				"client_reference_id": "ref_test_123",
				"customer": "cus_test_123",
				"payment_status": "paid",
				"status": "complete"
			}
		}
	}`)

	ts := time.Now().Unix()
	sigBytes := hmac.New(sha256.New, []byte(secret))
	sigBytes.Write([]byte(fmt.Sprintf("%d", ts)))
	sigBytes.Write([]byte("."))
	sigBytes.Write(payload)
	sig := hex.EncodeToString(sigBytes.Sum(nil))
	header := fmt.Sprintf("t=%d,v1=%s", ts, sig)

	req := httptest.NewRequest(http.MethodPost, "/api/stripe/webhook", io.NopCloser(bytes.NewReader(payload)))
	req.Header.Set("Stripe-Signature", header)

	c, rec := ginadapter.NewSyntheticContext(req)

	StripeWebhook(c)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestStripeWebhookRejectsInvalidSignatureViaBadRequest verifies invalid
// signatures produce 400.
func TestStripeWebhookRejectsInvalidSignatureViaBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Setup all required settings
	prevCompliance := pay_subscription.GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := pay_subscription.GetPaymentSetting().ComplianceTermsVersion
	prevApiSecret := pay_subscription.StripeApiSecret
	prevSecret := pay_subscription.StripeWebhookSecret
	prevPriceId := pay_subscription.StripePriceId
	t.Cleanup(func() {
		pay_subscription.GetPaymentSetting().ComplianceConfirmed = prevCompliance
		pay_subscription.GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		pay_subscription.StripeApiSecret = prevApiSecret
		pay_subscription.StripeWebhookSecret = prevSecret
		pay_subscription.StripePriceId = prevPriceId
	})
	pay_subscription.GetPaymentSetting().ComplianceConfirmed = true
	pay_subscription.GetPaymentSetting().ComplianceTermsVersion = pay_subscription.CurrentComplianceTermsVersion
	secret := "whsec_test_secret"
	pay_subscription.StripeApiSecret = "sk_test_123"
	pay_subscription.StripeWebhookSecret = secret
	pay_subscription.StripePriceId = "price_123"

	payload := []byte(`{"id":"evt_test","type":"checkout.session.completed"}`)
	// Wrong signature
	header := "t=1234567890,v1=invalidsignature"

	req := httptest.NewRequest(http.MethodPost, "/api/stripe/webhook", io.NopCloser(bytes.NewReader(payload)))
	req.Header.Set("Stripe-Signature", header)

	c, rec := ginadapter.NewSyntheticContext(req)

	StripeWebhook(c)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
