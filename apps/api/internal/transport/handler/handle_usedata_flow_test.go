package handler

import (
	channel "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/stretchr/testify/require"
)

type flowQuotaResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Data    []usage.FlowQuotaData `json:"data"`
}

func setupFlowControllerTestDB(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&identity.Token{}, &usage.QuotaData{}))
	require.NoError(t, dbx.DB.Create(&channel.Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, dbx.DB.Create(&identity.Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, dbx.DB.Create(&identity.Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, dbx.DB.Create(&usage.QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "default",
		ChannelID: 1,
		ModelName: "gpt-a",
		CreatedAt: 1100,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	}).Error)
	require.NoError(t, dbx.DB.Create(&usage.QuotaData{
		UserID:    2,
		Username:  "bob",
		NodeName:  "node-b",
		TokenID:   22,
		UseGroup:  "vip",
		ChannelID: 1,
		ModelName: "gpt-b",
		CreatedAt: 1200,
		Count:     1,
		Quota:     70,
		TokenUsed: 30,
	}).Error)
}

func decodeFlowQuotaResponse(t *testing.T, recorder *httptest.ResponseRecorder) flowQuotaResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	return payload
}

func TestGetAllFlowQuotaDatesUsesAdminDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctxRaw, _ := gin.CreateTestContext(recorder)
	ctxRaw.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=bob", nil)
	ctx := ginadapter.Wrap(ctxRaw)
	ctx.Set("role", common.RoleAdminUser)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "bob", payload.Data[0].Username)
	require.Equal(t, "vip", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
	require.Empty(t, payload.Data[0].TokenName)
	require.Empty(t, payload.Data[0].NodeName)
}

func TestGetAllFlowQuotaDatesUsesRootDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctxRaw, _ := gin.CreateTestContext(recorder)
	ctxRaw.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=alice", nil)
	ctx := ginadapter.Wrap(ctxRaw)
	ctx.Set("role", common.RoleRootUser)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "alice", payload.Data[0].Username)
	require.Equal(t, "node-a", payload.Data[0].NodeName)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Equal(t, "east", payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRestrictsToAuthenticatedUser(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctxRaw, _ := gin.CreateTestContext(recorder)
	ctxRaw.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=1000&end_timestamp=2000", nil)
	ctx := ginadapter.Wrap(ctxRaw)
	ctx.Set("id", 1)

	GetUserFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Empty(t, payload.Data[0].Username)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "default", payload.Data[0].UseGroup)
	require.Empty(t, payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRejectsInvalidTimeRange(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctxRaw, _ := gin.CreateTestContext(recorder)
	ctxRaw.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=bad&end_timestamp=2000", nil)
	ctx := ginadapter.Wrap(ctxRaw)
	ctx.Set("id", 1)

	GetUserFlowQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.False(t, payload.Success)
	require.Equal(t, "invalid start_timestamp", payload.Message)
}
