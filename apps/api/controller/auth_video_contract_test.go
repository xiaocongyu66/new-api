package controller

import (
	"bytes"
	"encoding/json"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/egress/fetch_url"
	"github.com/QuantumNous/new-api/internal/gateway/port"
	"github.com/QuantumNous/new-api/internal/i18n"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/task"
	"github.com/QuantumNous/new-api/internal/transport/ginadapter"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupAuthVideoContractTestDB(t *testing.T) {
	t.Helper()
	previousDB := dbx.DB
	previousLogDB := dbx.LogDB
	previousRedis := common.RedisEnabled
	previousPasswordLogin := common.PasswordLoginEnabled
	previousRegisterEnabled := common.RegisterEnabled
	previousPasswordRegister := common.PasswordRegisterEnabled
	previousEmailVerification := common.EmailVerificationEnabled
	previousTaskProviderFunc := port.GetTaskProviderFunc
	previousFetchSetting := *fetch_url.GetFetchSetting()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserSession{},
		&model.AuthFlow{},
		&model.TwoFA{},
		&model.TwoFABackupCode{},
		&model.Task{},
		&model.Channel{},
		&model.Log{},
	))
	require.NoError(t, i18n.Init())
	dbx.DB = db
	dbx.LogDB = db
	common.RedisEnabled = false
	common.PasswordLoginEnabled = true
	common.RegisterEnabled = true
	common.PasswordRegisterEnabled = true
	common.EmailVerificationEnabled = false

	// Stub GetTaskProviderFunc to avoid nil panic
	port.GetTaskProviderFunc = func(platform string) port.TaskProviderExec {
		return nil
	}

	// Disable SSRF protection for test upstream URLs (http://localhost)
	fetch_url.GetFetchSetting().EnableSSRFProtection = false

	t.Cleanup(func() {
		dbx.DB = previousDB
		dbx.LogDB = previousLogDB
		common.RedisEnabled = previousRedis
		common.PasswordLoginEnabled = previousPasswordLogin
		common.RegisterEnabled = previousRegisterEnabled
		common.PasswordRegisterEnabled = previousPasswordRegister
		common.EmailVerificationEnabled = previousEmailVerification
		port.GetTaskProviderFunc = previousTaskProviderFunc
		*fetch_url.GetFetchSetting() = previousFetchSetting
	})
}

func createTestUser(t *testing.T, username, password string) *model.User {
	t.Helper()
	user := &model.User{
		Username:    username,
		Password:    password,
		DisplayName: username,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Email:       username + "@test.com",
	}
	err := user.Insert(0)
	require.NoError(t, err)
	return user
}

func createTestUserWith2FA(t *testing.T, username, password string) (*model.User, string) {
	t.Helper()
	user := createTestUser(t, username, password)

	twoFA := &model.TwoFA{
		UserId:    user.Id,
		Secret:    "JBSWY3DPEHPK3PXP", // base32 for "testsecret"
		IsEnabled: false,              // pending setup
	}
	err := twoFA.CreatePendingTwoFASetup()
	require.NoError(t, err)
	err = twoFA.EnableWithAuthVersion()
	require.NoError(t, err)

	return user, "123456" // TOTP code for "testsecret" at time 0
}

func createTestTask(t *testing.T, userID int, taskID string, channelID int) *model.Task {
	t.Helper()
	taskModel := &model.Task{
		TaskID:    taskID,
		UserId:    userID,
		Status:    model.TaskStatusSuccess,
		ChannelId: channelID,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-" + taskID,
			ResultURL:      "https://example.com/video.mp4",
		},
	}
	err := dbx.DB.Create(taskModel).Error
	require.NoError(t, err)
	return taskModel
}

func createTestChannel(t *testing.T, channelType int) *model.Channel {
	t.Helper()
	baseURL := "https://api.example.com"
	channel := &model.Channel{
		Name:    "Test Channel",
		Type:    channelType,
		Key:     "test-key",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}
	err := dbx.DB.Create(channel).Error
	require.NoError(t, err)
	return channel
}

func assertSuccess(t *testing.T, body []byte, msg common.H) common.H {
	t.Helper()
	require.NoError(t, common.Unmarshal(body, &msg))
	assert.True(t, msg["success"].(bool))
	return msg
}

func assertFailure(t *testing.T, body []byte, msg common.H) common.H {
	t.Helper()
	require.NoError(t, common.Unmarshal(body, &msg))
	assert.False(t, msg["success"].(bool))
	return msg
}

func TestLoginSuccessContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAuthVideoContractTestDB(t)

	user := createTestUser(t, "testuser", "correctpassword")

	reqBody := map[string]string{"username": "testuser", "password": "correctpassword"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	c, recorder := ginadapter.NewSyntheticContext(req)
	identity.Login(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp common.H
	resp = assertSuccess(t, recorder.Body.Bytes(), resp)
	assert.Empty(t, resp["message"])
	assert.NotNil(t, resp["data"])

	data := resp["data"].(map[string]interface{})
	assert.NotEmpty(t, data["access_token"])
	userData := data["user"].(map[string]interface{})
	assert.Equal(t, float64(user.Id), userData["id"])
	assert.Equal(t, user.Username, userData["username"])
}

func TestLoginWrongPasswordContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAuthVideoContractTestDB(t)

	createTestUser(t, "testuser", "correctpassword")

	reqBody := map[string]string{"username": "testuser", "password": "wrongpassword"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	c, recorder := ginadapter.NewSyntheticContext(req)
	identity.Login(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp common.H
	resp = assertFailure(t, recorder.Body.Bytes(), resp)
	assert.NotEmpty(t, resp["message"])
	// Check for the translated error message (default is English)
	assert.Contains(t, resp["message"].(string), "Username or password is incorrect")
}

func TestRegisterSuccessContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAuthVideoContractTestDB(t)

	reqBody := map[string]string{
		"username": "newuser",
		"password": "newpassword123",
		"email":    "newuser@test.com",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	c, recorder := ginadapter.NewSyntheticContext(req)
	identity.Register(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp common.H
	resp = assertSuccess(t, recorder.Body.Bytes(), resp)
	assert.Empty(t, resp["message"])
}

func TestRegisterDuplicateContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAuthVideoContractTestDB(t)

	createTestUser(t, "existinguser", "password123")

	reqBody := map[string]string{
		"username": "existinguser",
		"password": "differentpass123",
		"email":    "existinguser@test.com",
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/user/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	c, recorder := ginadapter.NewSyntheticContext(req)
	identity.Register(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp common.H
	resp = assertFailure(t, recorder.Body.Bytes(), resp)
	assert.NotEmpty(t, resp["message"])
	// Check for the translated error message (default is English)
	assert.Contains(t, resp["message"].(string), "Username already exists")
}

func Test2FALoginRequiresOTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAuthVideoContractTestDB(t)

	createTestUserWith2FA(t, "2fauser", "password123")

	// Step 1: Login with password -> should return require_2fa
	loginBody := map[string]string{"username": "2fauser", "password": "password123"}
	loginJSON, _ := json.Marshal(loginBody)
	req := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(loginJSON))
	req.Header.Set("Content-Type", "application/json")

	c, recorder := ginadapter.NewSyntheticContext(req)
	identity.Login(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var loginResp common.H
	loginResp = assertSuccess(t, recorder.Body.Bytes(), loginResp)
	data := loginResp["data"].(map[string]interface{})
	assert.True(t, data["require_2fa"].(bool))
	flowToken := data["flow_token"].(string)
	assert.NotEmpty(t, flowToken)
	assert.NotNil(t, data["expires_at"])

	// Step 2: Verify 2FA with invalid code -> should fail with proper error
	verifyBody := map[string]string{"flow_token": flowToken, "code": "000000"}
	verifyJSON, _ := json.Marshal(verifyBody)
	req2 := httptest.NewRequest(http.MethodPost, "/api/user/login/2fa", bytes.NewReader(verifyJSON))
	req2.Header.Set("Content-Type", "application/json")

	c2, recorder2 := ginadapter.NewSyntheticContext(req2)
	identity.Verify2FALogin(c2)

	require.Equal(t, http.StatusOK, recorder2.Code)

	var verifyResp common.H
	verifyResp = assertFailure(t, recorder2.Body.Bytes(), verifyResp)
	assert.NotEmpty(t, verifyResp["message"])
	assert.Contains(t, verifyResp["message"].(string), "验证码")
}

func TestPasswordResetSendAndConfirmContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAuthVideoContractTestDB(t)

	user := createTestUser(t, "resetuser", "oldpassword123")
	// Step 1: Send password reset email
	req := httptest.NewRequest(http.MethodGet, "/api/reset_password?email="+user.Email, nil)
	c, recorder := ginadapter.NewSyntheticContext(req)
	identity.SendPasswordResetEmail(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var sendResp common.H
	sendResp = assertSuccess(t, recorder.Body.Bytes(), sendResp)
	assert.Empty(t, sendResp["message"])

	// Step 2: Register a known code and confirm password reset
	testCode := "TESTCODE123"
	common.RegisterVerificationCodeWithKey(user.Email, testCode, common.PasswordResetPurpose)

	resetBody := map[string]string{"email": user.Email, "token": testCode}
	resetJSON, _ := json.Marshal(resetBody)
	req2 := httptest.NewRequest(http.MethodPost, "/api/user/reset", bytes.NewReader(resetJSON))
	req2.Header.Set("Content-Type", "application/json")
	c2, recorder2 := ginadapter.NewSyntheticContext(req2)
	identity.ResetPassword(c2)

	require.Equal(t, http.StatusOK, recorder2.Code)

	var resetResp common.H
	resetResp = assertSuccess(t, recorder2.Body.Bytes(), resetResp)
	assert.Empty(t, resetResp["message"])
	assert.NotNil(t, resetResp["data"])
	newPassword := resetResp["data"].(string)
	assert.Len(t, newPassword, 12)

	// Step 3: Verify the new password works
	loginBody := map[string]string{"username": "resetuser", "password": newPassword}
	loginJSON, _ := json.Marshal(loginBody)
	req3 := httptest.NewRequest(http.MethodPost, "/api/user/login", bytes.NewReader(loginJSON))
	req3.Header.Set("Content-Type", "application/json")

	c3, recorder3 := ginadapter.NewSyntheticContext(req3)
	identity.Login(c3)

	require.Equal(t, http.StatusOK, recorder3.Code)
	var loginResp common.H
	loginResp = assertSuccess(t, recorder3.Body.Bytes(), loginResp)
}

func TestPasswordResetInvalidTokenContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAuthVideoContractTestDB(t)

	user := createTestUser(t, "resetuser2", "oldpassword123")

	resetBody := map[string]string{"email": user.Email, "token": "INVALID_TOKEN"}
	resetJSON, _ := json.Marshal(resetBody)
	req := httptest.NewRequest(http.MethodPost, "/api/user/reset", bytes.NewReader(resetJSON))
	req.Header.Set("Content-Type", "application/json")

	c, recorder := ginadapter.NewSyntheticContext(req)
	identity.ResetPassword(c)
	require.Equal(t, http.StatusOK, recorder.Code)

	var resp common.H
	resp = assertFailure(t, recorder.Body.Bytes(), resp)
	assert.NotEmpty(t, resp["message"])
	// Check for the translated error message (default is English)
	assert.Contains(t, resp["message"].(string), "Password reset link is invalid")
}

func TestVideoProxySuccessPathContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAuthVideoContractTestDB(t)

	// Create user and channel
	user := createTestUser(t, "videouser", "password123")
	channel := createTestChannel(t, constant.ChannelTypeOpenAI)
	createTestTask(t, user.Id, "task-video-1", channel.Id)

	// Create an httptest server as the upstream
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/videos/upstream-task-video-1/content", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		// Return a fake video response
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "12")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("fake video"))
	}))
	defer upstream.Close()

	// Override channel base URL to point to our test server
	newBaseURL := upstream.URL
	dbx.DB.Model(channel).Update("base_url", &newBaseURL)

	// Make the request through the video proxy
	req := httptest.NewRequest(http.MethodGet, "/v1/videos/task-video-1/content", nil)
	c, recorder := ginadapter.NewSyntheticContext(req)
	// Manually set the user ID in context (simulating auth middleware)
	ginCtx, _ := ginadapter.Unwrap(c)
	ginCtx.Set("id", user.Id)
	// Set route parameter for task_id
	ginCtx.Params = gin.Params{{Key: "task_id", Value: "task-video-1"}}

	task.VideoProxy(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=86400", recorder.Header().Get("Cache-Control"))
	assert.Equal(t, "fake video", recorder.Body.String())
}

func TestVideoProxySuccessPathOpenAIContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAuthVideoContractTestDB(t)

	user := createTestUser(t, "openaiuser", "password123")
	channel := createTestChannel(t, constant.ChannelTypeOpenAI)
	taskModel := createTestTask(t, user.Id, "task-openai-1", channel.Id)
	// Store API key in private data for OpenAI (though OpenAI uses channel key)
	dbx.DB.Model(taskModel).Update("private_data", model.TaskPrivateData{
		Key:            "",
		UpstreamTaskID: "upstream-task-openai-1",
		ResultURL:      "https://example.com/openai-video.mp4",
	})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/v1/")
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("openai video"))
	}))
	defer upstream.Close()

	newBaseURL := upstream.URL
	dbx.DB.Model(channel).Update("base_url", &newBaseURL)

	req := httptest.NewRequest(http.MethodGet, "/v1/videos/task-openai-1/content", nil)
	c, recorder := ginadapter.NewSyntheticContext(req)
	ginCtx, _ := ginadapter.Unwrap(c)
	ginCtx.Set("id", user.Id)
	ginCtx.Params = gin.Params{{Key: "task_id", Value: "task-openai-1"}}

	task.VideoProxy(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "openai video", recorder.Body.String())
}

// TestVideoProxyTransfersUpstreamHeaders verifies that response headers from upstream
// are passed through to the client (excluding hop-by-hop headers handled by Go).
func TestVideoProxyTransfersUpstreamHeadersContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupAuthVideoContractTestDB(t)

	user := createTestUser(t, "headeruser", "password123")
	channel := createTestChannel(t, constant.ChannelTypeOpenAI)
	createTestTask(t, user.Id, "task-header-1", channel.Id)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("X-Custom-Header", "custom-value")
		w.Header().Set("X-Another-Header", "another-value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("header video"))
	}))
	defer upstream.Close()

	newBaseURL := upstream.URL
	dbx.DB.Model(channel).Update("base_url", &newBaseURL)

	req := httptest.NewRequest(http.MethodGet, "/v1/videos/task-header-1/content", nil)
	c, recorder := ginadapter.NewSyntheticContext(req)
	ginCtx, _ := ginadapter.Unwrap(c)
	ginCtx.Set("id", user.Id)
	ginCtx.Params = gin.Params{{Key: "task_id", Value: "task-header-1"}}

	task.VideoProxy(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "custom-value", recorder.Header().Get("X-Custom-Header"))
	assert.Equal(t, "another-value", recorder.Header().Get("X-Another-Header"))
}
