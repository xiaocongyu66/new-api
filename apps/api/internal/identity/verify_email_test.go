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
	common.EmailFormatRegex = "^[0-9]+@qq\\.com$\n^[a-z]+@company\\.com$"
	var compiled []*regexp.Regexp
	for _, rule := range []string{"^[0-9]+@qq\\.com$", "^[a-z]+@company\\.com$"} {
		re, err := regexp.Compile(rule)
		require.NoError(t, err)
		compiled = append(compiled, re)
	}
	common.EmailFormatRegexCompiled = compiled

	tests := []struct {
		email      string
		shouldPass bool
	}{
		{email: "admin@qq.com", shouldPass: false},
		{email: "123456@qq.com", shouldPass: true},
		{email: "abc@company.com", shouldPass: true},
		{email: "paco0822@qq.com", shouldPass: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.email, func(t *testing.T) {
			code := seedPendingCode(t, tc.email)
			_ = performEmailVerification(t, tc.email)
			if tc.shouldPass {
				assert.False(t, common.VerifyCodeWithKey(tc.email, code, common.EmailVerificationPurpose),
					"%s 应走到发码步骤并覆盖预置验证码", tc.email)
			} else {
				// response body check still useful for first case
				_ = performEmailVerification(t, tc.email)
				assert.True(t, common.VerifyCodeWithKey(tc.email, code, common.EmailVerificationPurpose),
					"%s 不应签发新验证码", tc.email)
			}
		})
	}
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
