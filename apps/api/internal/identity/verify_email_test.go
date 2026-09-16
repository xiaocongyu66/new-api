package identity

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// performEmailVerification serves SendEmailVerification through a real route so
// the query parsing matches production.
func performEmailVerification(t *testing.T, email string) string {
	t.Helper()
	target := "/api/verification?email=" + url.QueryEscape(email)
	response := testutil.ServeBufferedRoute(t, http.MethodGet, "/api/verification",
		nil, SendEmailVerification, httptest.NewRequest(http.MethodGet, target, nil))
	payload, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	return string(payload)
}

// seedPendingCode pre-registers a known verification code so the test can tell
// whether SendEmailVerification reached the code-issuance step (which overwrites
// it) or was rejected before that.
func seedPendingCode(t *testing.T, email string) string {
	t.Helper()
	code := common.GenerateVerificationCode(6)
	common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)
	t.Cleanup(func() { common.DeleteKey(email, common.EmailVerificationPurpose) })
	return code
}

// EmailFormatRegex 必须在不匹配的邮箱进入发码流程之前拒绝它；
// 匹配的邮箱则要走到发码步骤（覆盖预置的旧验证码）。
func TestSendEmailVerificationFormatRegex(t *testing.T) {
	truncateTables(t)

	oldRegex, oldCompiled := common.EmailFormatRegex, common.EmailFormatRegexCompiled
	t.Cleanup(func() {
		common.EmailFormatRegex, common.EmailFormatRegexCompiled = oldRegex, oldCompiled
	})
	common.EmailFormatRegex = "^[0-9]+@qq\\.com$"
	re, err := regexp.Compile(common.EmailFormatRegex)
	require.NoError(t, err)
	common.EmailFormatRegexCompiled = re

	rejected := "admin@qq.com"
	accepted := "123456@qq.com"
	rejectedCode := seedPendingCode(t, rejected)
	acceptedCode := seedPendingCode(t, accepted)

	body := performEmailVerification(t, rejected)
	require.Contains(t, body, `"success":false`, "不匹配的邮箱必须被拒绝")
	assert.True(t, common.VerifyCodeWithKey(rejected, rejectedCode, common.EmailVerificationPurpose),
		"被拒绝的邮箱不应签发新验证码")

	_ = performEmailVerification(t, accepted)
	assert.False(t, common.VerifyCodeWithKey(accepted, acceptedCode, common.EmailVerificationPurpose),
		"匹配的邮箱应走到发码步骤并覆盖预置验证码")
}

// 未配置 EmailFormatRegex（编译结果为 nil）时不能拦截任何邮箱。
func TestSendEmailVerificationFormatRegexDisabled(t *testing.T) {
	truncateTables(t)

	oldRegex, oldCompiled := common.EmailFormatRegex, common.EmailFormatRegexCompiled
	t.Cleanup(func() {
		common.EmailFormatRegex, common.EmailFormatRegexCompiled = oldRegex, oldCompiled
	})
	common.EmailFormatRegex = ""
	common.EmailFormatRegexCompiled = nil

	email := "someone@gmail.com"
	code := seedPendingCode(t, email)

	_ = performEmailVerification(t, email)
	assert.False(t, common.VerifyCodeWithKey(email, code, common.EmailVerificationPurpose),
		"未启用格式限制时应正常发码")
}
