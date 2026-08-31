package handler

import (
	"encoding/json"
	"fmt"
	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withChannelModelHealthControllerDB(t *testing.T) {
	t.Helper()
	previousDB := dbx.DB
	previousType := common.MainDatabaseType()
	gin.SetMode(gin.TestMode)
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&channelpkg.ChannelModelHealth{}))
	dbx.DB = db
	channelpkg.ClearRouteHealthCache()
	t.Cleanup(func() {
		dbx.DB = previousDB
		common.SetMainDatabaseType(previousType)
		channelpkg.ClearRouteHealthCache()
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestChannelModelHealthAdminAPI(t *testing.T) {
	withChannelModelHealthControllerDB(t)

	now := time.Now().Unix()
	until := now + 30
	require.NoError(t, dbx.DB.Create(&channelpkg.ChannelModelHealth{
		ChannelId:           71,
		Model:               "gpt-health",
		State:               channelpkg.HealthCalm,
		IsolationLevel:      2,
		Until:               &until,
		Version:             1,
		DormantDisableCount: 1,
		LastErrorCode:       "bad_response",
		LastErrorAt:         &now,
		UpdatedAt:           now,
	}).Error)

	t.Run("lists one channel's model matrix", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/health?channel_id=71", nil)

		GetChannelModelHealth(ginadapter.Wrap(ctx))

		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool                    `json:"success"`
			Data    []channelModelHealthRow `json:"data"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success)
		require.Len(t, response.Data, 1)
		assert.Equal(t, "gpt-health", response.Data[0].Model)
		assert.Equal(t, channelpkg.HealthCalm, response.Data[0].State)
		assert.Greater(t, response.Data[0].RemainingSeconds, int64(0))
		assert.Equal(t, 1, response.Data[0].DormantDisableCount)
	})

	t.Run("disable then recover changes the persisted route", func(t *testing.T) {
		post := func(action string) *httptest.ResponseRecorder {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Params = gin.Params{{Key: "action", Value: action}}
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/health/"+action, strings.NewReader(`{"channel_id":71,"model":"gpt-health"}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			UpdateChannelModelHealth(ginadapter.Wrap(ctx))
			return recorder
		}

		require.Equal(t, http.StatusOK, post("disable").Code)
		var row channelpkg.ChannelModelHealth
		require.NoError(t, dbx.DB.Where("channel_id = ? AND model = ?", 71, "gpt-health").First(&row).Error)
		assert.Equal(t, channelpkg.HealthDisabled, row.State)
		assert.Nil(t, row.Until)

		require.Equal(t, http.StatusOK, post("recover").Code)
		require.NoError(t, dbx.DB.Where("channel_id = ? AND model = ?", 71, "gpt-health").First(&row).Error)
		assert.Equal(t, channelpkg.HealthHealthy, row.State)
		assert.Zero(t, row.IsolationLevel)
		assert.Zero(t, row.DormantDisableCount)
	})

	t.Run("rejects unknown action", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Params = gin.Params{{Key: "action", Value: "unknown"}}
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/health/unknown", strings.NewReader(`{"channel_id":71,"model":"gpt-health"}`))
		ctx.Request.Header.Set("Content-Type", "application/json")

		UpdateChannelModelHealth(ginadapter.Wrap(ctx))

		var response struct {
			Success bool `json:"success"`
		}
		require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
		assert.False(t, response.Success)
	})
}
