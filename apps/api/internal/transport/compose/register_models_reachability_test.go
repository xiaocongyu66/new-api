// Copyright (C) 2023-2026 QuantumNous
// SPDX-License-Identifier: AGPL-3.0-or-later

package compose

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	channel "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/handler"
	"github.com/QuantumNous/new-api/internal/transport/testutil"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestModelsRoutesAreBothReachable pins the /api/models pair that fiber's
// non-strict routing once shadowed. StrictRouting is off, so "GET /api/models"
// and "GET /api/models/" are one effective route and whichever registers first
// owns both spellings. The admin meta list used to live on the group root and
// lost that race to the earlier UserAuth registration, leaving it unreachable
// while the snapshot still recorded it: the snapshot compares a sorted set and
// cannot see registration order or reachability.
//
// The admin list now lives at "/api/models/meta", a path with no bare twin, so
// both handlers are always served. Each probe drives a real fiber route
// through fiberadapter.Dispatch and asserts the handler answered, which is
// what the shadowing defect broke and what the snapshot cannot see.
func TestModelsRoutesAreBothReachable(t *testing.T) {
	db := setupModelsReachabilityDB(t)

	require.NoError(t, db.Create(&channel.Model{
		ModelName: "phase6-meta-model",
	}).Error)

	dashboard := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	dashResponse := testutil.ServeBufferedRoute(t,
		http.MethodGet, "/api/models", nil, handler.DashboardListModels, dashboard)
	dashBody := readAll(t, dashResponse)
	require.Equal(t, http.StatusOK, dashResponse.StatusCode,
		"GET /api/models must reach the user-facing dashboard list")
	assert.Contains(t, dashBody, "success",
		"the dashboard list answers with the standard envelope")

	meta := httptest.NewRequest(http.MethodGet, "/api/models/meta", nil)
	metaResponse := testutil.ServeBufferedRoute(t,
		http.MethodGet, "/api/models/meta", nil, handler.GetAllModelsMeta, meta)
	metaBody := readAll(t, metaResponse)
	require.Equal(t, http.StatusOK, metaResponse.StatusCode,
		"GET /api/models/meta must reach the admin meta list")
	assert.Contains(t, metaBody, `"phase6-meta-model"`,
		"the admin meta list serves the model metadata table")

	assert.NotContains(t, dashBody, `"phase6-meta-model"`,
		"the dashboard list must not serve the meta table")
}

// setupModelsReachabilityDB wires an isolated in-memory SQLite database over
// the tables the two handlers read, mirroring setupModelListControllerTestDB.
func setupModelsReachabilityDB(t *testing.T) *gorm.DB {
	t.Helper()

	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")
	defer func() {
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	}()

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	dbx.DB = db
	dbx.LogDB = db

	require.NoError(t, db.AutoMigrate(&identity.User{}, &channel.Channel{}, &channel.Ability{}, &channel.ChannelModelRoute{}, &channel.Model{}, &channel.Vendor{}, &channel.GatewayConfigRevision{}, &channel.GatewayConfigOutbox{}))
	require.NoError(t, channel.InitializeGatewayConfigRevision())

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

// readAll drains a buffered response body for assertion.
func readAll(t testing.TB, response *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return string(body)
}
