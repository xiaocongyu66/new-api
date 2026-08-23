package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRouteUnitTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.ChannelModelRoute{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func seedRouteUnit(t *testing.T, db *gorm.DB, id int, alias string, channelID, weight int, enabled bool) model.ChannelModelRoute {
	t.Helper()
	ch := &model.Channel{Id: channelID, Name: "ch-" + alias, Type: 1, Status: 1}
	require.NoError(t, db.Create(ch).Error)
	route := model.ChannelModelRoute{
		Id:               id,
		Group:            "default",
		PublicModelAlias: alias,
		ChannelId:        channelID,
		KeyIndex:         0,
		UpstreamModel:    alias,
		StaticWeight:     weight,
		Enabled:          enabled,
	}
	require.NoError(t, db.Create(&route).Error)
	return route
}

func newRouteUnitRouter() *gin.Engine {
	r := gin.New()
	r.GET("/route_unit/", GetRouteUnitViews)
	r.GET("/route_unit/aliases", ListRouteUnitAliases)
	r.PUT("/route_unit/:id", UpdateRouteUnit)
	return r
}

func decodeRouteUnitList(t *testing.T, recorder *httptest.ResponseRecorder) struct {
	Success bool `json:"success"`
	Data    struct {
		Items       []model.RouteUnitView `json:"items"`
		TotalWeight int                   `json:"total_weight"`
	} `json:"data"`
} {
	t.Helper()
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Items       []model.RouteUnitView `json:"items"`
			TotalWeight int                   `json:"total_weight"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	return resp
}

func TestRouteUnit_GetRouteUnitViews_MissingAlias(t *testing.T) {
	setupRouteUnitTestDB(t)
	router := newRouteUnitRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/route_unit/", nil)
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRouteUnit_GetRouteUnitViews_ReturnsItemsWithHealthScore(t *testing.T) {
	db := setupRouteUnitTestDB(t)
	seedRouteUnit(t, db, 1, "gpt-4", 100, 3, true)
	seedRouteUnit(t, db, 2, "gpt-4", 101, 2, true)

	router := newRouteUnitRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/route_unit/?alias=gpt-4", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	resp := decodeRouteUnitList(t, rec)
	require.True(t, resp.Success)
	require.Len(t, resp.Data.Items, 2)
	assert.Equal(t, 5, resp.Data.TotalWeight)
	// HealthScore field is present in the JSON (default 0 when no health data)
	for _, item := range resp.Data.Items {
		assert.Contains(t, rec.Body.String(), "health_score")
		_ = item
	}
}

func TestRouteUnit_ListAliases(t *testing.T) {
	db := setupRouteUnitTestDB(t)
	seedRouteUnit(t, db, 1, "gpt-4", 100, 3, true)
	seedRouteUnit(t, db, 2, "claude", 101, 2, true)

	router := newRouteUnitRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/route_unit/aliases", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Success bool                          `json:"success"`
		Data    []model.RouteUnitAliasSummary `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.Len(t, resp.Data, 2)
}

func TestRouteUnit_Update_InvalidWeight_Negative(t *testing.T) {
	db := setupRouteUnitTestDB(t)
	seedRouteUnit(t, db, 1, "gpt-4", 100, 3, true)

	router := newRouteUnitRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/route_unit/1", strings.NewReader(`{"static_weight":-1}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRouteUnit_Update_InvalidWeight_Decimal(t *testing.T) {
	db := setupRouteUnitTestDB(t)
	seedRouteUnit(t, db, 1, "gpt-4", 100, 3, true)

	router := newRouteUnitRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/route_unit/1", strings.NewReader(`{"static_weight":1.5}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRouteUnit_Update_InvalidWeight_NaNString(t *testing.T) {
	db := setupRouteUnitTestDB(t)
	seedRouteUnit(t, db, 1, "gpt-4", 100, 3, true)

	router := newRouteUnitRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/route_unit/1", strings.NewReader(`{"static_weight":"abc"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	// JSON decode of string into *float64 fails -> ShouldBindJSON error -> 400
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRouteUnit_Update_NotFound(t *testing.T) {
	setupRouteUnitTestDB(t)

	router := newRouteUnitRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/route_unit/9999", strings.NewReader(`{"static_weight":5}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRouteUnit_Update_ValidWeightAndEnabled(t *testing.T) {
	db := setupRouteUnitTestDB(t)
	seedRouteUnit(t, db, 1, "gpt-4", 100, 3, true)

	router := newRouteUnitRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/route_unit/1", strings.NewReader(`{"static_weight":7,"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Success bool                 `json:"success"`
		Data    model.RouteUnitView  `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.Equal(t, 7, resp.Data.StaticWeight)
	assert.False(t, resp.Data.Enabled)

	// Verify persisted
	var route model.ChannelModelRoute
	require.NoError(t, db.First(&route, 1).Error)
	assert.Equal(t, 7, route.StaticWeight)
	assert.False(t, route.Enabled)
}
func TestRouteUnit_Update_WeightUpperBoundPasses(t *testing.T) {
	db := setupRouteUnitTestDB(t)
	seedRouteUnit(t, db, 1, "gpt-4", 100, 3, true)

	router := newRouteUnitRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/route_unit/1", strings.NewReader(`{"static_weight":1000000000}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Success bool                 `json:"success"`
		Data    model.RouteUnitView  `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.Equal(t, 1000000000, resp.Data.StaticWeight)
}

func TestRouteUnit_Update_WeightUpperBoundExceeded(t *testing.T) {
	db := setupRouteUnitTestDB(t)
	seedRouteUnit(t, db, 1, "gpt-4", 100, 3, true)

	router := newRouteUnitRouter()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/route_unit/1", strings.NewReader(`{"static_weight":1000000001}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var resp struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "1000000000")
}

func TestRouteUnit_Update_WeightNegativeZeroValid(t *testing.T) {
	db := setupRouteUnitTestDB(t)
	seedRouteUnit(t, db, 1, "gpt-4", 100, 3, true)

	router := newRouteUnitRouter()
	rec := httptest.NewRecorder()
	// -0 is valid JSON and should be treated as 0
	req := httptest.NewRequest(http.MethodPut, "/route_unit/1", strings.NewReader(`{"static_weight":-0}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Success bool                 `json:"success"`
		Data    model.RouteUnitView  `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.Equal(t, 0, resp.Data.StaticWeight)
}
