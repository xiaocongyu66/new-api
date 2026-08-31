package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupRouteUnitTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	gin.SetMode(gin.TestMode)
	previousDB := dbx.DB
	previousLogDB := dbx.LogDB
	previousRedis := common.RedisEnabled
	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	dbx.DB = db
	dbx.LogDB = db

	require.NoError(t, db.AutoMigrate(&channelpkg.Channel{}, &channelpkg.ChannelModelRoute{}))

	t.Cleanup(func() {
		dbx.DB = previousDB
		dbx.LogDB = previousLogDB
		common.RedisEnabled = previousRedis
		common.SetDatabaseTypes(previousMain, previousLog)
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func seedRouteUnit(t *testing.T, db *gorm.DB, id int, alias string, channelID, weight int, enabled bool) channelpkg.ChannelModelRoute {
	t.Helper()
	ch := &channelpkg.Channel{Id: channelID, Name: "ch-" + alias, Type: 1, Status: 1}
	require.NoError(t, db.Create(ch).Error)
	route := channelpkg.ChannelModelRoute{
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

// newRouteUnitRouter registers the route-unit handlers through the transport
// adapter, which is the only way they are reachable in production: they are
// written against contract.Context, so gin cannot call them directly.
func newRouteUnitRouter() *gin.Engine {
	r := gin.New()
	r.GET("/route_unit/", ginadapter.Handler(GetRouteUnitViews))
	r.GET("/route_unit/aliases", ginadapter.Handler(ListRouteUnitAliases))
	r.PUT("/route_unit/:id", ginadapter.Handler(UpdateRouteUnit))
	return r
}

func decodeRouteUnitList(t *testing.T, recorder *httptest.ResponseRecorder) struct {
	Success bool `json:"success"`
	Data    struct {
		Items []channelpkg.RouteUnitView `json:"items"`
	} `json:"data"`
} {
	t.Helper()
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Items []channelpkg.RouteUnitView `json:"items"`
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
	// total_weight is gone: it summed weights across every group while selection
	// draws from one (group, alias) pool, so the number it reported was not the
	// denominator of any real share. expected_share carries that intent per group
	// instead — here both routes sit in the same group with weights 3 and 2.
	shares := map[int]float64{}
	for _, item := range resp.Data.Items {
		shares[item.ChannelId] = item.ExpectedShare
		assert.Contains(t, rec.Body.String(), "health_score")
	}
	assert.InDelta(t, 0.6, shares[100], 0.0001)
	assert.InDelta(t, 0.4, shares[101], 0.0001)
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
		Success bool                               `json:"success"`
		Data    []channelpkg.RouteUnitAliasSummary `json:"data"`
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
		Success bool                     `json:"success"`
		Data    channelpkg.RouteUnitView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.Equal(t, 7, resp.Data.StaticWeight)
	assert.False(t, resp.Data.Enabled)

	// Verify persisted
	var route channelpkg.ChannelModelRoute
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
		Success bool                     `json:"success"`
		Data    channelpkg.RouteUnitView `json:"data"`
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
		Success bool                     `json:"success"`
		Data    channelpkg.RouteUnitView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	assert.Equal(t, 0, resp.Data.StaticWeight)
}

// TestRouteUnit_HandlersRunOnContractContext pins the transport seam for the
// route-unit handlers: they are declared as contract.Handler, so they must be
// callable with any contract.Context — not only one that happens to wrap gin.
//
// The other tests here drive them through a real gin engine, which would keep
// passing even if a handler reached back for *gin.Context via MustUnwrap. That
// would silently re-bind these endpoints to gin and break when the framework is
// replaced, so the contract is asserted on a synthetic context that no router
// ever touched.
func TestRouteUnit_HandlersRunOnContractContext(t *testing.T) {
	db := setupRouteUnitTestDB(t)
	seedRouteUnit(t, db, 1, "gpt-4", 100, 3, true)

	// The compile-time half: each handler satisfies the contract signature.
	for name, handler := range map[string]contract.Handler{
		"views":   GetRouteUnitViews,
		"aliases": ListRouteUnitAliases,
		"update":  UpdateRouteUnit,
	} {
		require.NotNil(t, handler, name)
	}

	t.Run("query is read through the contract", func(t *testing.T) {
		ctx, rec := ginadapter.NewSyntheticContext(
			httptest.NewRequest(http.MethodGet, "/route_unit/?alias=gpt-4", nil))
		GetRouteUnitViews(ctx)

		require.Equal(t, http.StatusOK, rec.Code)
		resp := decodeRouteUnitList(t, rec)
		require.True(t, resp.Success)
		require.Len(t, resp.Data.Items, 1)
		assert.Equal(t, 100, resp.Data.Items[0].ChannelId)
	})

	t.Run("missing alias aborts through the contract", func(t *testing.T) {
		ctx, rec := ginadapter.NewSyntheticContext(
			httptest.NewRequest(http.MethodGet, "/route_unit/", nil))
		GetRouteUnitViews(ctx)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("body and path param are read through the contract", func(t *testing.T) {
		// A synthetic context carries no route params, so the id has to come from
		// the router. Registering through the adapter is what production does, and
		// it proves Param() resolves without the handler unwrapping gin.
		router := newRouteUnitRouter()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/route_unit/1", strings.NewReader(`{"static_weight":11}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		var route channelpkg.ChannelModelRoute
		require.NoError(t, db.First(&route, 1).Error)
		assert.Equal(t, 11, route.StaticWeight)
	})
}
