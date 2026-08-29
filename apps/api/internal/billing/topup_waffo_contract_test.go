package billing

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/billing/pay_subscription"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waffoTestKeys holds an RSA key pair generated per-test for Waffo webhook
// signature construction. The handler verifies against the public cert, so
// we only need the keys to live for one test run.
func waffoTestKeys(t *testing.T) (privateKeyB64, publicCertB64 string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	privateKeyB64 = base64.StdEncoding.EncodeToString(pkcs8)

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	publicCertB64 = base64.StdEncoding.EncodeToString(pubDER)
	return
}

// waffoSign signs data with a base64 PKCS#8 private key using SHA256withRSA,
// matching the Waffo SDK's utils.Sign algorithm.
func waffoSign(t *testing.T, data, privateKeyB64 string) string {
	t.Helper()
	keyBytes, err := base64.StdEncoding.DecodeString(privateKeyB64)
	require.NoError(t, err)
	priv, err := x509.ParsePKCS8PrivateKey(keyBytes)
	require.NoError(t, err)
	rsaKey, ok := priv.(*rsa.PrivateKey)
	require.True(t, ok)

	hashed := sha256.Sum256([]byte(data))
	sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, hashed[:])
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(sig)
}

// TestWaffoWebhookRejectsDisabledViaForbidden pins the contract when Waffo
// webhook is not configured — HTTP 403 with no body.
func TestWaffoWebhookRejectsDisabledViaForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	prevEnabled := pay_subscription.WaffoEnabled
	prevSandbox := pay_subscription.WaffoSandbox
	pay_subscription.WaffoEnabled = false
	pay_subscription.WaffoSandbox = false
	t.Cleanup(func() {
		pay_subscription.WaffoEnabled = prevEnabled
		pay_subscription.WaffoSandbox = prevSandbox
	})

	req := httptest.NewRequest(http.MethodPost, "/api/waffo/webhook", nil)
	c, rec := ginadapter.NewSyntheticContext(req)

	WaffoWebhook(c)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, rec.Body.String())
}

// TestWaffoWebhookRejectsInvalidSignatureViaBadRequest verifies that an
// invalid signature produces 400.
func TestWaffoWebhookRejectsInvalidSignatureViaBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	privateKeyB64, publicCertB64 := waffoTestKeys(t)

	// Setup all required settings for Waffo webhook to be enabled
	prevCompliance := pay_subscription.GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := pay_subscription.GetPaymentSetting().ComplianceTermsVersion
	prevEnabled := pay_subscription.WaffoEnabled
	prevSandbox := pay_subscription.WaffoSandbox
	prevApiKey := pay_subscription.WaffoApiKey
	prevPrivateKey := pay_subscription.WaffoPrivateKey
	prevPublicCert := pay_subscription.WaffoPublicCert
	t.Cleanup(func() {
		pay_subscription.GetPaymentSetting().ComplianceConfirmed = prevCompliance
		pay_subscription.GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		pay_subscription.WaffoEnabled = prevEnabled
		pay_subscription.WaffoSandbox = prevSandbox
		pay_subscription.WaffoApiKey = prevApiKey
		pay_subscription.WaffoPrivateKey = prevPrivateKey
		pay_subscription.WaffoPublicCert = prevPublicCert
	})
	pay_subscription.GetPaymentSetting().ComplianceConfirmed = true
	pay_subscription.GetPaymentSetting().ComplianceTermsVersion = pay_subscription.CurrentComplianceTermsVersion
	pay_subscription.WaffoEnabled = true
	pay_subscription.WaffoSandbox = false
	pay_subscription.WaffoApiKey = "waffo_api_key"
	pay_subscription.WaffoPrivateKey = privateKeyB64
	pay_subscription.WaffoPublicCert = publicCertB64

	payload := `{"eventType":"PAYMENT_NOTIFICATION","result":{"merchantOrderId":"test_123","orderStatus":"PAY_SUCCESS"}}`
	// Wrong signature
	badSig := "invalidsignature"

	req := httptest.NewRequest(http.MethodPost, "/api/waffo/webhook", io.NopCloser(bytes.NewReader([]byte(payload))))
	req.Header.Set("X-SIGNATURE", badSig)

	c, rec := ginadapter.NewSyntheticContext(req)

	WaffoWebhook(c)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestWaffoWebhookAcceptsValidSignatureAndReturnsOK constructs a valid
// payment notification with a correct RSA signature and asserts the handler
// returns 200 OK with a signed response body.
func TestWaffoWebhookAcceptsValidSignatureAndReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)

	privateKeyB64, publicCertB64 := waffoTestKeys(t)

	// Setup all required settings
	prevCompliance := pay_subscription.GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := pay_subscription.GetPaymentSetting().ComplianceTermsVersion
	prevEnabled := pay_subscription.WaffoEnabled
	prevSandbox := pay_subscription.WaffoSandbox
	prevApiKey := pay_subscription.WaffoApiKey
	prevPrivateKey := pay_subscription.WaffoPrivateKey
	prevPublicCert := pay_subscription.WaffoPublicCert
	t.Cleanup(func() {
		pay_subscription.GetPaymentSetting().ComplianceConfirmed = prevCompliance
		pay_subscription.GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		pay_subscription.WaffoEnabled = prevEnabled
		pay_subscription.WaffoSandbox = prevSandbox
		pay_subscription.WaffoApiKey = prevApiKey
		pay_subscription.WaffoPrivateKey = prevPrivateKey
		pay_subscription.WaffoPublicCert = prevPublicCert
	})
	pay_subscription.GetPaymentSetting().ComplianceConfirmed = true
	pay_subscription.GetPaymentSetting().ComplianceTermsVersion = pay_subscription.CurrentComplianceTermsVersion
	pay_subscription.WaffoEnabled = true
	pay_subscription.WaffoSandbox = false
	pay_subscription.WaffoApiKey = "waffo_api_key"
	pay_subscription.WaffoPrivateKey = privateKeyB64
	pay_subscription.WaffoPublicCert = publicCertB64

	// A non-PAYMENT event type is ignored with a success response — no DB
	// lookup required, so the test is deterministic without a database.
	payload := `{"eventType":"REFUND_NOTIFICATION","result":{"merchantOrderId":"test_123"}}`
	sig := waffoSign(t, payload, privateKeyB64)

	req := httptest.NewRequest(http.MethodPost, "/api/waffo/webhook", io.NopCloser(bytes.NewReader([]byte(payload))))
	req.Header.Set("X-SIGNATURE", sig)

	c, rec := ginadapter.NewSyntheticContext(req)

	WaffoWebhook(c)

	assert.Equal(t, http.StatusOK, rec.Code)
	// Waffo sends a signed JSON response body, not empty
	assert.NotEmpty(t, rec.Body.String())
	// Response carries a signature header
	assert.NotEmpty(t, rec.Header().Get("X-SIGNATURE"))
}
