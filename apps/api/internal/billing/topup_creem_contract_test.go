package billing

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/billing/pay_subscription"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestCreemWebhookRejectsDisabledViaForbidden pins the contract when Creem
// webhook is not configured — HTTP 403 with no body.
func TestCreemWebhookRejectsDisabledViaForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevSecret := pay_subscription.CreemWebhookSecret
	prevProducts := pay_subscription.CreemProducts
	prevTestMode := pay_subscription.CreemTestMode
	pay_subscription.CreemWebhookSecret = ""
	pay_subscription.CreemProducts = "[]"
	pay_subscription.CreemTestMode = false
	t.Cleanup(func() {
		pay_subscription.CreemWebhookSecret = prevSecret
		pay_subscription.CreemProducts = prevProducts
		pay_subscription.CreemTestMode = prevTestMode
	})

	req := httptest.NewRequest(http.MethodPost, "/api/creem/webhook", nil)
	c, rec := ginadapter.NewSyntheticContext(req)

	CreemWebhook(c)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, rec.Body.String())
}

// TestCreemWebhookAcceptsValidSignatureAndReturnsOK constructs a minimal
// checkout.completed event, signs it with the test secret, and asserts
// the handler returns 200 OK.
func TestCreemWebhookAcceptsValidSignatureAndReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Setup all required settings for Creem webhook to be enabled
	prevCompliance := pay_subscription.GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := pay_subscription.GetPaymentSetting().ComplianceTermsVersion
	prevApiKey := pay_subscription.CreemApiKey
	prevProducts := pay_subscription.CreemProducts
	prevSecret := pay_subscription.CreemWebhookSecret
	prevTestMode := pay_subscription.CreemTestMode
	t.Cleanup(func() {
		pay_subscription.GetPaymentSetting().ComplianceConfirmed = prevCompliance
		pay_subscription.GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		pay_subscription.CreemApiKey = prevApiKey
		pay_subscription.CreemProducts = prevProducts
		pay_subscription.CreemWebhookSecret = prevSecret
		pay_subscription.CreemTestMode = prevTestMode
	})
	pay_subscription.GetPaymentSetting().ComplianceConfirmed = true
	pay_subscription.GetPaymentSetting().ComplianceTermsVersion = pay_subscription.CurrentComplianceTermsVersion
	secret := "creem_test_secret"
	pay_subscription.CreemApiKey = "creem_api_key"
	pay_subscription.CreemProducts = `[{"productId":"prod_test_123"}]`
	pay_subscription.CreemWebhookSecret = secret
	pay_subscription.CreemTestMode = false

	// Minimal checkout.completed payload matching CreemWebhookEvent
	payload := []byte(`{
		"id": "evt_test_webhook",
		"eventType": "checkout.completed",
		"created_at": 1700000000,
		"object": {
			"request_id": "ref_test_123",
			"order": {
				"id": "ord_test_123",
				"status": "paid",
				"product_id": "prod_test_123"
			}
		}
	}`)

	// Generate HMAC-SHA256 signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	sig := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/api/creem/webhook", io.NopCloser(bytes.NewReader(payload)))
	req.Header.Set("creem-signature", sig)

	c, rec := ginadapter.NewSyntheticContext(req)

	CreemWebhook(c)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestCreemWebhookRejectsInvalidSignatureViaUnauthorized verifies invalid
// signatures produce 401.
func TestCreemWebhookRejectsInvalidSignatureViaUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Setup all required settings
	prevCompliance := pay_subscription.GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := pay_subscription.GetPaymentSetting().ComplianceTermsVersion
	prevApiKey := pay_subscription.CreemApiKey
	prevProducts := pay_subscription.CreemProducts
	prevSecret := pay_subscription.CreemWebhookSecret
	prevTestMode := pay_subscription.CreemTestMode
	t.Cleanup(func() {
		pay_subscription.GetPaymentSetting().ComplianceConfirmed = prevCompliance
		pay_subscription.GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		pay_subscription.CreemApiKey = prevApiKey
		pay_subscription.CreemProducts = prevProducts
		pay_subscription.CreemWebhookSecret = prevSecret
		pay_subscription.CreemTestMode = prevTestMode
	})
	pay_subscription.GetPaymentSetting().ComplianceConfirmed = true
	pay_subscription.GetPaymentSetting().ComplianceTermsVersion = pay_subscription.CurrentComplianceTermsVersion
	secret := "creem_test_secret"
	pay_subscription.CreemApiKey = "creem_api_key"
	pay_subscription.CreemProducts = `[{"productId":"prod_test_123"}]`
	pay_subscription.CreemWebhookSecret = secret
	pay_subscription.CreemTestMode = false

	payload := []byte(`{"id":"evt_test","eventType":"checkout.completed"}`)
	// Wrong signature
	sig := "invalidsignature"

	req := httptest.NewRequest(http.MethodPost, "/api/creem/webhook", io.NopCloser(bytes.NewReader(payload)))
	req.Header.Set("creem-signature", sig)

	c, rec := ginadapter.NewSyntheticContext(req)

	CreemWebhook(c)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestCreemWebhookRejectsMissingSignatureViaUnauthorized verifies missing
// signatures produce 401.
func TestCreemWebhookRejectsMissingSignatureViaUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Setup all required settings
	prevCompliance := pay_subscription.GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := pay_subscription.GetPaymentSetting().ComplianceTermsVersion
	prevApiKey := pay_subscription.CreemApiKey
	prevProducts := pay_subscription.CreemProducts
	prevSecret := pay_subscription.CreemWebhookSecret
	prevTestMode := pay_subscription.CreemTestMode
	t.Cleanup(func() {
		pay_subscription.GetPaymentSetting().ComplianceConfirmed = prevCompliance
		pay_subscription.GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		pay_subscription.CreemApiKey = prevApiKey
		pay_subscription.CreemProducts = prevProducts
		pay_subscription.CreemWebhookSecret = prevSecret
		pay_subscription.CreemTestMode = prevTestMode
	})
	pay_subscription.GetPaymentSetting().ComplianceConfirmed = true
	pay_subscription.GetPaymentSetting().ComplianceTermsVersion = pay_subscription.CurrentComplianceTermsVersion
	secret := "creem_test_secret"
	pay_subscription.CreemApiKey = "creem_api_key"
	pay_subscription.CreemProducts = `[{"productId":"prod_test_123"}]`
	pay_subscription.CreemWebhookSecret = secret
	pay_subscription.CreemTestMode = false

	payload := []byte(`{"id":"evt_test","eventType":"checkout.completed"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/creem/webhook", io.NopCloser(bytes.NewReader(payload)))
	// No signature header

	c, rec := ginadapter.NewSyntheticContext(req)

	CreemWebhook(c)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
