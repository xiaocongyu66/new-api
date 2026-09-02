package billing

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/transport/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveWaffoPancakeWebhook drives the handler through a real Fiber route
// registered on the production pattern, because the handler reads the :env
// route param and a synthetic context has no router to populate it.
func serveWaffoPancakeWebhook(t *testing.T, body io.Reader, signature string) (int, string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/api/waffo-pancake/webhook/test", body)
	if signature != "" {
		request.Header.Set("X-Waffo-Signature", signature)
	}
	response := testutil.ServeBufferedRoute(t, http.MethodPost, "/api/waffo-pancake/webhook/:env",
		nil, WaffoPancakeWebhook, request)

	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	return response.StatusCode, string(payload)
}

// TestWaffoPancakeWebhookRejectsDisabledViaForbidden pins the contract when
// Waffo Pancake webhook is not configured — HTTP 403 with body "webhook disabled".
func TestWaffoPancakeWebhookRejectsDisabledViaForbidden(t *testing.T) {
	prevMerchantID := WaffoPancakeMerchantID
	prevPrivateKey := WaffoPancakePrivateKey
	prevProductID := WaffoPancakeProductID
	WaffoPancakeMerchantID = ""
	WaffoPancakePrivateKey = ""
	WaffoPancakeProductID = ""
	t.Cleanup(func() {
		WaffoPancakeMerchantID = prevMerchantID
		WaffoPancakePrivateKey = prevPrivateKey
		WaffoPancakeProductID = prevProductID
	})

	status, body := serveWaffoPancakeWebhook(t, nil, "")

	assert.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, "webhook disabled", body)
}

// TestWaffoPancakeWebhookRejectsInvalidSignatureViaUnauthorized verifies
// that an invalid signature produces 401 with body "invalid signature".
func TestWaffoPancakeWebhookRejectsInvalidSignatureViaUnauthorized(t *testing.T) {
	// Setup minimal config to enable the webhook
	prevCompliance := GetPaymentSetting().ComplianceConfirmed
	prevTermsVersion := GetPaymentSetting().ComplianceTermsVersion
	prevMerchantID := WaffoPancakeMerchantID
	prevPrivateKey := WaffoPancakePrivateKey
	prevProductID := WaffoPancakeProductID
	t.Cleanup(func() {
		GetPaymentSetting().ComplianceConfirmed = prevCompliance
		GetPaymentSetting().ComplianceTermsVersion = prevTermsVersion
		WaffoPancakeMerchantID = prevMerchantID
		WaffoPancakePrivateKey = prevPrivateKey
		WaffoPancakeProductID = prevProductID
	})
	GetPaymentSetting().ComplianceConfirmed = true
	GetPaymentSetting().ComplianceTermsVersion = CurrentComplianceTermsVersion
	WaffoPancakeMerchantID = "merch_test"
	WaffoPancakePrivateKey = "dummy_private_key"
	WaffoPancakeProductID = "prod_test"

	payload := `{"id":"evt_test","timestamp":"2026-05-13T00:00:00Z","eventType":"order.completed","eventId":"PAY_test","storeId":"STO_test","storeName":"Test","mode":"test","data":{"orderId":"ORD_test","orderMerchantExternalID":"trade_test"}}`
	badSig := "t=1234567890000,v1=invalidsignature"

	status, body := serveWaffoPancakeWebhook(t, bytes.NewReader([]byte(payload)), badSig)

	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "invalid signature", body)
}

// TestWaffoPancakeWebhookAcceptsValidSignature_SKIPPED documents that a valid
// signature test is not feasible without the private key corresponding to the
// SDK's builtin test/prod public keys. The handler calls
// VerifyConfiguredWaffoPancakeWebhook with nil options, which auto-detects
// between the two hardcoded public keys. Since the private keys are not
// exposed, we cannot construct a signature that passes verification. This test
// is intentionally skipped and serves as documentation of the gap.
func TestWaffoPancakeWebhookAcceptsValidSignature_SKIPPED(t *testing.T) {
	t.Skip("Cannot construct valid signature without SDK builtin test private key. Handler uses auto-detection against hardcoded test/prod public keys.")
}
