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

	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
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
	prevEnabled := WaffoEnabled
	prevSandbox := WaffoSandbox
	WaffoEnabled = false
	WaffoSandbox = false
	t.Cleanup(func() {
		WaffoEnabled = prevEnabled
		WaffoSandbox = prevSandbox
	})

	req := httptest.NewRequest(http.MethodPost, "/api/waffo/webhook", nil)
	c, rec := fiberadapter.NewSyntheticContext(req)

	WaffoWebhook(c)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Empty(t, rec.Body.String())
}

// TestWaffoWebhookRejectsInvalidSignatureViaBadRequest verifies that an
// invalid signature produces 400.
func TestWaffoWebhookRejectsInvalidSignatureViaBadRequest(t *testing.T) {
	privateKeyB64, publicCertB64 := waffoTestKeys(t)

	// Setup all required settings for Waffo webhook to be enabled
	prevCompliance := GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := GetPaymentSetting().ComplianceTermsVersion
	prevEnabled := WaffoEnabled
	prevSandbox := WaffoSandbox
	prevApiKey := WaffoApiKey
	prevPrivateKey := WaffoPrivateKey
	prevPublicCert := WaffoPublicCert
	t.Cleanup(func() {
		GetPaymentSetting().ComplianceConfirmed = prevCompliance
		GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		WaffoEnabled = prevEnabled
		WaffoSandbox = prevSandbox
		WaffoApiKey = prevApiKey
		WaffoPrivateKey = prevPrivateKey
		WaffoPublicCert = prevPublicCert
	})
	GetPaymentSetting().ComplianceConfirmed = true
	GetPaymentSetting().ComplianceTermsVersion = CurrentComplianceTermsVersion
	WaffoEnabled = true
	WaffoSandbox = false
	WaffoApiKey = "waffo_api_key"
	WaffoPrivateKey = privateKeyB64
	WaffoPublicCert = publicCertB64

	payload := `{"eventType":"PAYMENT_NOTIFICATION","result":{"merchantOrderId":"test_123","orderStatus":"PAY_SUCCESS"}}`
	// Wrong signature
	badSig := "invalidsignature"

	req := httptest.NewRequest(http.MethodPost, "/api/waffo/webhook", io.NopCloser(bytes.NewReader([]byte(payload))))
	req.Header.Set("X-SIGNATURE", badSig)

	c, rec := fiberadapter.NewSyntheticContext(req)

	WaffoWebhook(c)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestWaffoWebhookAcceptsValidSignatureAndReturnsOK constructs a valid
// payment notification with a correct RSA signature and asserts the handler
// returns 200 OK with a signed response body.
func TestWaffoWebhookAcceptsValidSignatureAndReturnsOK(t *testing.T) {
	privateKeyB64, publicCertB64 := waffoTestKeys(t)

	// Setup all required settings
	prevCompliance := GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := GetPaymentSetting().ComplianceTermsVersion
	prevEnabled := WaffoEnabled
	prevSandbox := WaffoSandbox
	prevApiKey := WaffoApiKey
	prevPrivateKey := WaffoPrivateKey
	prevPublicCert := WaffoPublicCert
	t.Cleanup(func() {
		GetPaymentSetting().ComplianceConfirmed = prevCompliance
		GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		WaffoEnabled = prevEnabled
		WaffoSandbox = prevSandbox
		WaffoApiKey = prevApiKey
		WaffoPrivateKey = prevPrivateKey
		WaffoPublicCert = prevPublicCert
	})
	GetPaymentSetting().ComplianceConfirmed = true
	GetPaymentSetting().ComplianceTermsVersion = CurrentComplianceTermsVersion
	WaffoEnabled = true
	WaffoSandbox = false
	WaffoApiKey = "waffo_api_key"
	WaffoPrivateKey = privateKeyB64
	WaffoPublicCert = publicCertB64

	// A non-PAYMENT event type is ignored with a success response — no DB
	// lookup required, so the test is deterministic without a database.
	payload := `{"eventType":"REFUND_NOTIFICATION","result":{"merchantOrderId":"test_123"}}`
	sig := waffoSign(t, payload, privateKeyB64)

	req := httptest.NewRequest(http.MethodPost, "/api/waffo/webhook", io.NopCloser(bytes.NewReader([]byte(payload))))
	req.Header.Set("X-SIGNATURE", sig)

	c, rec := fiberadapter.NewSyntheticContext(req)

	WaffoWebhook(c)

	assert.Equal(t, http.StatusOK, rec.Code)
	// Waffo sends a signed JSON response body, not empty
	assert.NotEmpty(t, rec.Body.String())
	// Response carries a signature header
	assert.NotEmpty(t, rec.Header().Get("X-SIGNATURE"))
}
