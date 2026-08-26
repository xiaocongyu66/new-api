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

	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/gin-gonic/gin"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
)

// TestCreemWebhookRejectsDisabledViaForbidden pins the contract when Creem
// webhook is not configured — HTTP 403 with no body.
func TestCreemWebhookRejectsDisabledViaForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevSecret := setting.CreemWebhookSecret
	prevProducts := setting.CreemProducts
	prevTestMode := setting.CreemTestMode
	setting.CreemWebhookSecret = ""
	setting.CreemProducts = "[]"
	setting.CreemTestMode = false
	t.Cleanup(func() {
		setting.CreemWebhookSecret = prevSecret
		setting.CreemProducts = prevProducts
		setting.CreemTestMode = prevTestMode
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
	prevCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	prevApiKey := setting.CreemApiKey
	prevProducts := setting.CreemProducts
	prevSecret := setting.CreemWebhookSecret
	prevTestMode := setting.CreemTestMode
	t.Cleanup(func() {
		operation_setting.GetPaymentSetting().ComplianceConfirmed = prevCompliance
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		setting.CreemApiKey = prevApiKey
		setting.CreemProducts = prevProducts
		setting.CreemWebhookSecret = prevSecret
		setting.CreemTestMode = prevTestMode
	})
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	secret := "creem_test_secret"
	setting.CreemApiKey = "creem_api_key"
	setting.CreemProducts = `[{"productId":"prod_test_123"}]`
	setting.CreemWebhookSecret = secret
	setting.CreemTestMode = false

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
	prevCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	prevApiKey := setting.CreemApiKey
	prevProducts := setting.CreemProducts
	prevSecret := setting.CreemWebhookSecret
	prevTestMode := setting.CreemTestMode
	t.Cleanup(func() {
		operation_setting.GetPaymentSetting().ComplianceConfirmed = prevCompliance
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		setting.CreemApiKey = prevApiKey
		setting.CreemProducts = prevProducts
		setting.CreemWebhookSecret = prevSecret
		setting.CreemTestMode = prevTestMode
	})
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	secret := "creem_test_secret"
	setting.CreemApiKey = "creem_api_key"
	setting.CreemProducts = `[{"productId":"prod_test_123"}]`
	setting.CreemWebhookSecret = secret
	setting.CreemTestMode = false

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
	prevCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	prevApiKey := setting.CreemApiKey
	prevProducts := setting.CreemProducts
	prevSecret := setting.CreemWebhookSecret
	prevTestMode := setting.CreemTestMode
	t.Cleanup(func() {
		operation_setting.GetPaymentSetting().ComplianceConfirmed = prevCompliance
		operation_setting.GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		setting.CreemApiKey = prevApiKey
		setting.CreemProducts = prevProducts
		setting.CreemWebhookSecret = prevSecret
		setting.CreemTestMode = prevTestMode
	})
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	secret := "creem_test_secret"
	setting.CreemApiKey = "creem_api_key"
	setting.CreemProducts = `[{"productId":"prod_test_123"}]`
	setting.CreemWebhookSecret = secret
	setting.CreemTestMode = false

	payload := []byte(`{"id":"evt_test","eventType":"checkout.completed"}`)

	req := httptest.NewRequest(http.MethodPost, "/api/creem/webhook", io.NopCloser(bytes.NewReader(payload)))
	// No signature header

	c, rec := ginadapter.NewSyntheticContext(req)

	CreemWebhook(c)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}