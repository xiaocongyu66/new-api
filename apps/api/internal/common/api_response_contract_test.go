package common_test

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/stretchr/testify/assert"
)

// newAPIRecorder builds a context whose response lands in a recorder so the
// serialized envelope can be asserted byte-for-byte.
func newAPIRecorder(t *testing.T) (contract.Context, *httptest.ResponseRecorder) {
	t.Helper()

	c, recorder := ginadapter.NewSyntheticContext(nil)
	ginadapter.ReplaceRequest(c, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	return c, recorder
}

// TestApiSuccessEnvelope pins the success envelope every dashboard endpoint
// returns. The frontend branches on `success` and reads `data`, so the field set
// and the HTTP 200 status must survive the transport refactor.
func TestApiSuccessEnvelope(t *testing.T) {
	c, recorder := newAPIRecorder(t)

	common.CtxApiSuccess(c, map[string]any{"id": 7})

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"success":true,"message":"","data":{"id":7}}`, recorder.Body.String())
}

// TestApiErrorEnvelopeUsesHTTP200 pins the deliberate contract that business
// errors return HTTP 200 with success:false. Changing the status code would break
// every existing client error path.
func TestApiErrorEnvelopeUsesHTTP200(t *testing.T) {
	c, recorder := newAPIRecorder(t)

	common.CtxApiError(c, errors.New("channel not found"))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"success":false,"message":"channel not found"}`, recorder.Body.String())
}

// TestApiErrorMsgEnvelope pins the message-only error variant, including
// non-ASCII passthrough.
func TestApiErrorMsgEnvelope(t *testing.T) {
	c, recorder := newAPIRecorder(t)

	common.CtxApiErrorMsg(c, "无效的参数")

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"success":false,"message":"无效的参数"}`, recorder.Body.String())
}

// TestApiSuccessEnvelopeWithNilData asserts a nil payload still emits the data
// key rather than omitting it, because clients destructure the field directly.
func TestApiSuccessEnvelopeWithNilData(t *testing.T) {
	c, recorder := newAPIRecorder(t)

	common.CtxApiSuccess(c, nil)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.JSONEq(t, `{"success":true,"message":"","data":null}`, recorder.Body.String())
}

// TestGetPageQueryParsesPaginationAliases pins the pagination parsing contract,
// including the legacy `ps`/`size` aliases and the hard page-size ceiling.
func TestGetPageQueryParsesPaginationAliases(t *testing.T) {
	for _, tc := range []struct {
		name             string
		query            string
		expectedPage     int
		expectedPageSize int
	}{
		{name: "canonical params", query: "?p=3&page_size=25", expectedPage: 3, expectedPageSize: 25},
		{name: "missing page defaults to first", query: "?page_size=10", expectedPage: 1, expectedPageSize: 10},
		{name: "ps alias", query: "?p=2&ps=15", expectedPage: 2, expectedPageSize: 15},
		{name: "size alias", query: "?p=2&size=12", expectedPage: 2, expectedPageSize: 12},
		{name: "page size clamped", query: "?p=1&page_size=9999", expectedPage: 1, expectedPageSize: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := ginadapter.NewSyntheticContext(nil)
			ginadapter.ReplaceRequest(c, httptest.NewRequest(http.MethodGet, "/api/log/"+tc.query, nil))

			pageInfo := common.GetPageQuery(c)

			assert.Equal(t, tc.expectedPage, pageInfo.GetPage())
			assert.Equal(t, tc.expectedPageSize, pageInfo.GetPageSize())
		})
	}
}
