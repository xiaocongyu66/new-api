package billing

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// setupWaffoPancakeContext creates a synthetic context with the :env param set,
// since NewSyntheticContext does not run the gin router and therefore cannot
// populate route params.
func setupWaffoPancakeContext(t *testing.T, method, path string, body io.Reader) (contract.Context, *gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(method, path, body)
	ginCtx.Params = gin.Params{{Key: "env", Value: "test"}}
	return ginadapter.Wrap(ginCtx), ginCtx, recorder
}

// TestWaffoPancakeWebhookRejectsDisabledViaForbidden pins the contract when
// Waffo Pancake webhook is not configured — HTTP 403 with body "webhook disabled".
func TestWaffoPancakeWebhookRejectsDisabledViaForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

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

	c, rec := ginadapter.NewSyntheticContext(httptest.NewRequest(http.MethodPost, "/api/waffo-pancake/webhook/test", nil))

	WaffoPancakeWebhook(c)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "webhook disabled", rec.Body.String())
}

// TestWaffoPancakeWebhookRejectsInvalidSignatureViaUnauthorized verifies
// that an invalid signature produces 401 with body "invalid signature".
func TestWaffoPancakeWebhookRejectsInvalidSignatureViaUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)

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

	c, _, rec := setupWaffoPancakeContext(t, http.MethodPost, "/api/waffo-pancake/webhook/test", bytes.NewReader([]byte(payload)))
	c.Headers().Set("X-Waffo-Signature", badSig)

	WaffoPancakeWebhook(c)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "invalid signature", rec.Body.String())
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
