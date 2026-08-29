package handler

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"

	ratio_setting "github.com/QuantumNous/new-api/internal/catalog/configure_ratio"
	"github.com/QuantumNous/new-api/internal/catalog/resolve_group"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func configureTokenAutoGroupsTest(t *testing.T, maxCount string, autoGroups string) {
	t.Helper()
	originalMax := resolve_group.GetMaxTokenAutoGroups()
	originalAutoGroups := resolve_group.AutoGroups2JsonString()
	originalUsableGroups := resolve_group.UserUsableGroups2JSONString()
	originalRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, resolve_group.UpdateMaxTokenAutoGroups(maxCount))
	require.NoError(t, resolve_group.UpdateAutoGroupsByJsonString(autoGroups))
	require.NoError(t, resolve_group.UpdateUserUsableGroupsByJSONString(`{"default":"Default","vip":"VIP"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"vip":1}`))
	t.Cleanup(func() {
		require.NoError(t, resolve_group.UpdateMaxTokenAutoGroups(stringInt(originalMax)))
		require.NoError(t, resolve_group.UpdateAutoGroupsByJsonString(originalAutoGroups))
		require.NoError(t, resolve_group.UpdateUserUsableGroupsByJSONString(originalUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalRatios))
	})
}

func stringInt(value int) string {
	return fmt.Sprintf("%d", value)
}

func setupTokenAutoGroupsControllerTest(t *testing.T) *identity.User {
	t.Helper()
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&identity.User{}))
	user := &identity.User{
		Id:       101,
		Username: "token-auto-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)
	return user
}

func baseAutoTokenRequest(name string) map[string]any {
	return map[string]any{
		"name":              name,
		"expired_time":      -1,
		"remain_quota":      0,
		"unlimited_quota":   true,
		"group":             "auto",
		"cross_group_retry": true,
	}
}

func newTokenAutoGroupsAuthenticatedContext(t *testing.T, method string, target string, body any, userID int) (contract.Context, *httptest.ResponseRecorder) {
	t.Helper()
	ctx, recorder := newAuthenticatedContext(t, method, target, body, userID)
	common.SetCtxKey(ctx, constant.ContextKeyUserGroup, "default")
	return ctx, recorder
}

func TestAddTokenEmptyAutoGroupsInheritGlobalAuto(t *testing.T) {
	tests := []struct {
		name         string
		includeField bool
		value        any
	}{
		{name: "omitted"},
		{name: "null", includeField: true, value: nil},
		{name: "empty array", includeField: true, value: []string{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenAutoGroupsTest(t, "5", `["default","vip"]`)
			user := setupTokenAutoGroupsControllerTest(t)
			request := baseAutoTokenRequest("create-" + test.name)
			if test.includeField {
				request["auto_groups"] = test.value
			}

			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
			identity.AddToken(ctx)

			response := decodeAPIResponse(t, recorder)
			require.True(t, response.Success, response.Message)
			var token identity.Token
			require.NoError(t, dbx.DB.Where("name = ?", request["name"]).First(&token).Error)
			assert.Empty(t, token.AutoGroups)
			assert.True(t, token.CrossGroupRetry)
		})
	}
}

func TestAddTokenPersistsOrderedAutoGroupsSnapshot(t *testing.T) {
	configureTokenAutoGroupsTest(t, "5", `["default","vip"]`)
	user := setupTokenAutoGroupsControllerTest(t)
	request := baseAutoTokenRequest("ordered-snapshot")
	request["auto_groups"] = []string{"vip", "default"}

	ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
	identity.AddToken(ctx)
	require.True(t, decodeAPIResponse(t, recorder).Success)

	var token identity.Token
	require.NoError(t, dbx.DB.Where("name = ?", "ordered-snapshot").First(&token).Error)
	assert.JSONEq(t, `["vip","default"]`, token.AutoGroups)

	getRecorder := httptest.NewRecorder()
	getEngine := gin.New()
	getEngine.Use(func(c *gin.Context) {
		c.Set("id", user.Id)
		c.Set(string(constant.ContextKeyUserGroup), "default")
		c.Next()
	})
	getEngine.GET("/api/token/:id", ginadapter.Handler(identity.GetToken))
	getEngine.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/api/token/"+stringInt(token.Id), nil))
	getResponse := decodeAPIResponse(t, getRecorder)
	require.True(t, getResponse.Success)
	var data struct {
		AutoGroups []string `json:"auto_groups"`
	}
	require.NoError(t, common.Unmarshal(getResponse.Data, &data))
	assert.Equal(t, []string{"vip", "default"}, data.AutoGroups)
}

func TestUpdateTokenAutoGroupsTriStateAndNonAutoCleanup(t *testing.T) {
	tests := []struct {
		name               string
		includeField       bool
		value              any
		group              string
		expectedAutoGroups string
		expectedRetry      bool
	}{
		{name: "omitted preserves", group: "auto", expectedAutoGroups: `["vip","default"]`, expectedRetry: true},
		{name: "null inherits", includeField: true, value: nil, group: "auto", expectedRetry: true},
		{name: "empty inherits", includeField: true, value: []string{}, group: "auto", expectedRetry: true},
		{name: "non auto clears and disables retry", includeField: true, value: []string{"vip"}, group: "default"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenAutoGroupsTest(t, "5", `["default","vip"]`)
			user := setupTokenAutoGroupsControllerTest(t)
			token := seedToken(t, dbx.DB, user.Id, "update-auto", "update-auto-key")
			token.Group = "auto"
			token.CrossGroupRetry = true
			require.NoError(t, token.SetAutoGroups([]string{"vip", "default"}))
			require.NoError(t, dbx.DB.Save(token).Error)

			request := baseAutoTokenRequest("updated-auto")
			request["id"] = token.Id
			request["status"] = common.TokenStatusEnabled
			request["group"] = test.group
			if test.includeField {
				request["auto_groups"] = test.value
			}
			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPut, "/api/token/", request, user.Id)
			identity.UpdateToken(ctx)
			response := decodeAPIResponse(t, recorder)
			require.True(t, response.Success, response.Message)

			var updated identity.Token
			require.NoError(t, dbx.DB.First(&updated, token.Id).Error)
			if test.expectedAutoGroups == "" {
				assert.Empty(t, updated.AutoGroups)
			} else {
				assert.JSONEq(t, test.expectedAutoGroups, updated.AutoGroups)
			}
			assert.Equal(t, test.expectedRetry, updated.CrossGroupRetry)
		})
	}
}

func TestAddTokenRejectsInvalidAutoGroups(t *testing.T) {
	tests := []struct {
		name     string
		maxCount string
		groups   []string
	}{
		{name: "over limit", maxCount: "1", groups: []string{"default", "vip"}},
		{name: "duplicate", maxCount: "5", groups: []string{"default", "default"}},
		{name: "auto pseudo group", maxCount: "5", groups: []string{"auto"}},
		{name: "unavailable", maxCount: "5", groups: []string{"missing"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configureTokenAutoGroupsTest(t, test.maxCount, `["default","vip"]`)
			user := setupTokenAutoGroupsControllerTest(t)
			request := baseAutoTokenRequest("invalid-" + test.name)
			request["auto_groups"] = test.groups

			ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodPost, "/api/token/", request, user.Id)
			identity.AddToken(ctx)

			response := decodeAPIResponse(t, recorder)
			assert.False(t, response.Success)
			var count int64
			require.NoError(t, dbx.DB.Model(&identity.Token{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestGetTokenAutoGroupsReturnsFullFilteredGlobalOrderAndLimit(t *testing.T) {
	configureTokenAutoGroupsTest(t, "1", `["vip","missing","default"]`)
	user := setupTokenAutoGroupsControllerTest(t)

	ctx, recorder := newTokenAutoGroupsAuthenticatedContext(t, http.MethodGet, "/api/token/auto-groups", nil, user.Id)
	identity.GetTokenAutoGroups(ctx)

	response := decodeAPIResponse(t, recorder)
	require.True(t, response.Success, response.Message)
	var data struct {
		Groups   []string `json:"groups"`
		MaxCount int      `json:"max_count"`
	}
	require.NoError(t, common.Unmarshal(response.Data, &data))
	assert.Equal(t, []string{"vip", "default"}, data.Groups)
	assert.Equal(t, 1, data.MaxCount)
}
