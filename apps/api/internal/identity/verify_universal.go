package identity

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/internal/authtoken"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// UniversalVerifyRequest is the body of POST /api/verify. The scope of the
// security proof must be one of the allowlisted scopes.
type UniversalVerifyRequest struct {
	Method string `json:"method"`
	Code   string `json:"code,omitempty"`
	Scope  string `json:"scope"`
}

// UniversalVerify implements POST /api/verify. It exchanges a 2FA code for a
// short-lived security proof token. The previous 466-c placement under
// controller used usage.RecordLog / model.LogTypeSystem; the writeSystemLog
// hook installed by 466-a already records system activity. authID comes from
// the authtoken leaf rather than the security package, because security
// imports identity (cycle) but authtoken is the upstream leaf for the JWT
// auth chain.
func UniversalVerify(c contract.Context) {
	authID, ok := authtoken.ReadSessionAuthIdentity(c)
	if !ok {
		_ = c.JSON(http.StatusUnauthorized, common.H{"success": false, "message": "当前认证方式不支持安全验证"})
		return
	}
	var request UniversalVerifyRequest
	if err := c.BindJSON(&request); err != nil {
		common.CtxApiError(c, fmt.Errorf("参数错误: %v", err))
		return
	}
	if request.Method != secureVerificationMethod2FA {
		common.CtxApiError(c, errors.New("Passkey 验证必须使用 Passkey verify 流程"))
		return
	}
	if !isAllowedSecurityProofScope(request.Scope) {
		common.CtxApiError(c, errors.New("不支持的安全验证范围"))
		return
	}
	if strings.TrimSpace(request.Code) == "" {
		common.CtxApiError(c, errors.New("验证码不能为空"))
		return
	}
	twoFA, err := GetTwoFAByUserId(authID.UserID)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if twoFA == nil || !twoFA.IsEnabled {
		common.CtxApiError(c, errors.New("用户未启用2FA"))
		return
	}
	if !validateTwoFactorAuth(twoFA, request.Code) {
		common.CtxApiError(c, errors.New("验证失败，请检查验证码"))
		return
	}
	proofToken, expiresAt, err := IssueSecurityProof(authID, request.Method, []string{request.Scope})
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	writeSystemLog(authID.UserID, fmt.Sprintf("通用安全验证成功 (验证方式: %s)", request.Method))
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "验证成功",
		"data": common.H{
			"proof_token": proofToken,
			"expires_at":  expiresAt,
			"method":      request.Method,
			"scope":       request.Scope,
		},
	})
}

// validateTwoFactorAuth is shared with the channel key export flow. It lives
// in the identity package because both identity-domain flows call it; the
// channel controller can continue to call this exported function.
func validateTwoFactorAuth(twoFA *TwoFA, code string) bool {
	if cleanCode, err := common.ValidateNumericCode(code); err == nil {
		if isValid, _ := twoFA.ValidateTOTPAndUpdateUsage(cleanCode); isValid {
			return true
		}
	}
	if isValid, err := twoFA.ValidateBackupCodeAndUpdateUsage(code); err == nil && isValid {
		return true
	}
	return false
}
