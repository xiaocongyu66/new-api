package handler

import (
	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/transport/fiberadapter"
	"github.com/QuantumNous/new-api/internal/transport/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// apiEnvelope is the dashboard response envelope every channel endpoint returns.
type apiEnvelope struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// TestAddChannelRejectsInvalidPayload pins the create-channel rejection contract.
//
// Validation failures return HTTP 200 with success:false, which is the repository
// convention the dashboard branches on. Returning a 4xx here would break existing
// clients, so the status and envelope are asserted together.
func TestAddChannelRejectsInvalidPayload(t *testing.T) {
	setupModelListControllerTestDB(t)

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `{"name":`},
		{name: "missing name", body: `{"type":1,"key":"sk-test","models":"gpt-4"}`},
		{name: "missing key", body: `{"type":1,"name":"c1","models":"gpt-4"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, recorder := fiberadapter.NewSyntheticContext(
				httptest.NewRequest(http.MethodPost, "/api/channel/", strings.NewReader(tc.body)))
			ctx.Headers().Set("Content-Type", "application/json")

			AddChannel(ctx)

			require.Equal(t, http.StatusOK, recorder.Code,
				"business validation failures use HTTP 200 with success:false")

			var envelope apiEnvelope
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
			assert.False(t, envelope.Success)
			assert.NotEmpty(t, envelope.Message)
		})
	}
}

// TestGetChannelRejectsInvalidID pins the read path's rejection of a non-numeric
// path parameter.
func TestGetChannelRejectsInvalidID(t *testing.T) {
	setupModelListControllerTestDB(t)

	recorder := recordResponse(t, testutil.ServeBufferedRoute(t, http.MethodGet, "/api/channel/:id",
		nil, GetChannel,
		httptest.NewRequest(http.MethodGet, "/api/channel/not-a-number", nil)))

	require.Equal(t, http.StatusOK, recorder.Code)

	var envelope apiEnvelope
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
	assert.False(t, envelope.Success)
}

// TestGetAllChannelsReturnsPaginatedEnvelope pins the list response shape, which
// the dashboard table reads directly: a data object carrying items and total.
func TestGetAllChannelsReturnsPaginatedEnvelope(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	baseURL := "https://upstream.example.com"
	require.NoError(t, db.Create(&channelpkg.Channel{
		Id: 4101, Type: 1, Name: "crud-contract-channel", Key: "sk-crud",
		Models: "gpt-4", Group: "default", BaseURL: &baseURL,
		Status: common.ChannelStatusEnabled,
	}).Error)

	ctx, recorder := fiberadapter.NewSyntheticContext(
		httptest.NewRequest(http.MethodGet, "/api/channel/?p=1&page_size=10", nil))

	GetAllChannels(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Items []struct {
				Id   int    `json:"id"`
				Name string `json:"name"`
			} `json:"items"`
			Total int `json:"total"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))

	assert.True(t, response.Success)
	require.NotEmpty(t, response.Data.Items, "seeded channel must appear in the list")

	found := false
	for _, item := range response.Data.Items {
		if item.Id == 4101 {
			found = true
			assert.Equal(t, "crud-contract-channel", item.Name)
		}
	}
	assert.True(t, found, "list response must include the seeded channel")
}

// TestGetAllChannelsOmitsChannelKey asserts the list endpoint never serializes
// channel credentials. The key is a secret, and the list query explicitly omits
// it; a serialization change that leaked it would be a credential disclosure.
func TestGetAllChannelsOmitsChannelKey(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	const secret = "sk-must-not-leak-4102"
	baseURL := "https://upstream.example.com"
	require.NoError(t, db.Create(&channelpkg.Channel{
		Id: 4102, Type: 1, Name: "secret-channel", Key: secret,
		Models: "gpt-4", Group: "default", BaseURL: &baseURL,
		Status: common.ChannelStatusEnabled,
	}).Error)

	ctx, recorder := fiberadapter.NewSyntheticContext(
		httptest.NewRequest(http.MethodGet, "/api/channel/?p=1&page_size=10", nil))

	GetAllChannels(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), secret,
		"channel list must not serialize the channel key")
}

// TestDeleteChannelRemovesRecord pins the delete path end to end: the endpoint
// reports success and the row is gone.
func TestDeleteChannelRemovesRecord(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	baseURL := "https://upstream.example.com"
	require.NoError(t, db.Create(&channelpkg.Channel{
		Id: 4103, Type: 1, Name: "delete-me", Key: "sk-delete",
		Models: "gpt-4", Group: "default", BaseURL: &baseURL,
		Status: common.ChannelStatusEnabled,
	}).Error)

	recorder := recordResponse(t, testutil.ServeBufferedRoute(t, http.MethodDelete, "/api/channel/:id",
		nil, DeleteChannel,
		httptest.NewRequest(http.MethodDelete, "/api/channel/4103", nil)))

	require.Equal(t, http.StatusOK, recorder.Code)

	var envelope apiEnvelope
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
	assert.True(t, envelope.Success)

	var remaining int64
	require.NoError(t, db.Model(&channelpkg.Channel{}).Where("id = ?", 4103).Count(&remaining).Error)
	assert.Zero(t, remaining, "deleted channel must not remain queryable")
}
