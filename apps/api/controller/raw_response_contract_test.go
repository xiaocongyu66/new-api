package controller

import (
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEpayNotifyRejectsDisabledWebhookWithRawFail pins the raw acknowledgement
// body of the 易支付 webhook.
//
// The payment provider inspects the literal response body, not a JSON envelope:
// anything other than an exact "fail"/"success" string is treated as a delivery
// failure and triggers provider-side retries. This response is produced by a
// direct writer call, which the transport migration replaces, so the exact bytes
// are asserted here.
func TestEpayNotifyRejectsDisabledWebhookWithRawFail(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// The webhook is enabled only when the 易支付 credentials are configured, so
	// clearing them exercises the disabled-webhook rejection path.
	previousAddress := operation_setting.PayAddress
	previousID := operation_setting.EpayId
	previousKey := operation_setting.EpayKey
	operation_setting.PayAddress = ""
	operation_setting.EpayId = ""
	operation_setting.EpayKey = ""
	t.Cleanup(func() {
		operation_setting.PayAddress = previousAddress
		operation_setting.EpayId = previousID
		operation_setting.EpayKey = previousKey
	})
	recorder := httptest.NewRecorder()
	c, _ := ginadapter.NewSyntheticContext(httptest.NewRequest(http.MethodPost, "/api/user/epay/notify", nil))

	EpayNotify(c)

	assert.Equal(t, "fail", recorder.Body.String(),
		"the provider parses the literal body; a JSON envelope would be read as a delivery failure")
}

// TestVideoProxyRequiresTaskIDWithJSONError pins the error envelope of the video
// proxy endpoint, which otherwise streams raw upstream bytes through the writer.
func TestVideoProxyRequiresTaskIDWithJSONError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	c, recorder := ginadapter.NewSyntheticContext(httptest.NewRequest(http.MethodGet, "/v1/videos//content", nil))

	VideoProxy(c)

	require.Equal(t, http.StatusBadRequest, recorder.Code)

	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))

	assert.Equal(t, "invalid_request_error", body.Error.Type)
	assert.NotEmpty(t, body.Error.Message)
}
