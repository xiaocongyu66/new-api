package identity

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/internal/authtoken"
	"github.com/QuantumNous/new-api/internal/security/passkey"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/internal/common"

	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
)

const (
	SecurityProofScopeChannelKeyRead  = "channel.key.read"
	SecurityProofScopePasskeyRegister = "passkey.register"
	SecurityProofScopePasskeyDelete   = "passkey.delete"
)

type passkeyFinishRequest struct {
	FlowToken  string          `json:"flow_token"`
	Credential json.RawMessage `json:"credential"`
}

type passkeyVerifyBeginRequest struct {
	Scope string `json:"scope"`
}

func parsePasskeyFinishRequest(c contract.Context) (*passkeyFinishRequest, error) {
	var request passkeyFinishRequest
	// one-shot decode: this endpoint must not rewrite the request body
	if err := common.DecodeJson(c.HTTPRequest().Body, &request); err != nil {
		return nil, err
	}
	if request.FlowToken == "" || len(request.Credential) == 0 {
		return nil, errors.New("Passkey 流程参数不完整")
	}
	return &request, nil
}

func PasskeyRegisterBegin(c contract.Context) {
	if !passkey.GetPasskeySettings().Enabled {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "管理员未启用 Passkey 登录",
		})
		return
	}

	user, err := getAuthenticatedUser(c)
	if err != nil {
		_ = c.JSON(http.StatusUnauthorized, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	if !requirePasskeyRegistrationVerification(c, user.Id) {
		return
	}

	credential, err := GetPasskeyByUserID(user.Id)
	if err != nil && !errors.Is(err, ErrPasskeyNotFound) {
		common.CtxApiError(c, err)
		return
	}
	if errors.Is(err, ErrPasskeyNotFound) {
		credential = nil
	}

	wa, err := BuildWebAuthn(c.HTTPRequest())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	waUser := NewWebAuthnUser(user, credential)
	var options []webauthnlib.RegistrationOption
	if credential != nil {
		descriptor := credential.ToWebAuthnCredential().Descriptor()
		options = append(options, webauthnlib.WithExclusions([]protocol.CredentialDescriptor{descriptor}))
	}

	creation, sessionData, err := wa.BeginRegistration(waUser, options...)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	identity, ok := authtoken.ReadSessionAuthIdentity(c)
	if !ok {
		common.CtxApiError(c, errors.New("当前认证方式不支持安全验证"))
		return
	}
	flowToken, expiresAt, err := CreateSessionDataFlow(
		AuthFlowPurposePasskeyRegister,
		user.Id,
		identity.SessionID,
		SecurityProofScopePasskeyRegister,
		sessionData,
	)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data": common.H{
			"options":    creation,
			"flow_token": flowToken,
			"expires_at": expiresAt,
		},
	})
}

func PasskeyRegisterFinish(c contract.Context) {
	if !passkey.GetPasskeySettings().Enabled {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "管理员未启用 Passkey 登录",
		})
		return
	}

	user, err := getAuthenticatedUser(c)
	if err != nil {
		_ = c.JSON(http.StatusUnauthorized, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if !requirePasskeyRegistrationVerification(c, user.Id) {
		return
	}

	request, err := parsePasskeyFinishRequest(c)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	parsedCredential, err := protocol.ParseCredentialCreationResponseBytes(request.Credential)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	wa, err := BuildWebAuthn(c.HTTPRequest())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	credentialRecord, err := GetPasskeyByUserID(user.Id)
	if err != nil && !errors.Is(err, ErrPasskeyNotFound) {
		common.CtxApiError(c, err)
		return
	}
	if errors.Is(err, ErrPasskeyNotFound) {
		credentialRecord = nil
	}

	identity, ok := authtoken.ReadSessionAuthIdentity(c)
	if !ok {
		common.CtxApiError(c, errors.New("当前认证方式不支持安全验证"))
		return
	}
	sessionData, _, err := PopSessionDataFlow(
		request.FlowToken,
		AuthFlowPurposePasskeyRegister,
		user.Id,
		identity.SessionID,
	)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	waUser := NewWebAuthnUser(user, credentialRecord)
	credential, err := wa.CreateCredential(waUser, *sessionData, parsedCredential)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	passkeyCredential := NewPasskeyCredentialFromWebAuthn(user.Id, credential)
	if passkeyCredential == nil {
		common.CtxApiErrorMsg(c, "无法创建 Passkey 凭证")
		return
	}

	if err := UpsertPasskeyCredentialWithAuthVersion(passkeyCredential); err != nil {
		common.CtxApiError(c, err)
		return
	}
	bundle, err := AdvanceCurrentSessionToUserVersion(identity, "passkey_registered")
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	writeUserSecurityAudit(c, user.Id, "user.passkey_register", nil)
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "Passkey 注册成功",
		"data":    authRotationData(bundle),
	})
}

func PasskeyDelete(c contract.Context) {
	user, err := getAuthenticatedUser(c)
	if err != nil {
		_ = c.JSON(http.StatusUnauthorized, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	if !requirePasskeyDeleteVerification(c, user.Id) {
		return
	}

	identity, ok := authtoken.ReadSessionAuthIdentity(c)
	if !ok {
		common.CtxApiError(c, errors.New("当前认证方式不支持安全验证"))
		return
	}
	if err := DeletePasskeyByUserIDWithAuthVersion(user.Id); err != nil {
		common.CtxApiError(c, err)
		return
	}
	bundle, err := AdvanceCurrentSessionToUserVersion(identity, "passkey_deleted")
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	writeUserSecurityAudit(c, user.Id, "user.passkey_delete", nil)
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "Passkey 已解绑",
		"data":    authRotationData(bundle),
	})
}

func PasskeyStatus(c contract.Context) {
	user, err := getAuthenticatedUser(c)
	if err != nil {
		_ = c.JSON(http.StatusUnauthorized, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	credential, err := GetPasskeyByUserID(user.Id)
	if errors.Is(err, ErrPasskeyNotFound) {
		_ = c.JSON(http.StatusOK, common.H{
			"success": true,
			"message": "",
			"data": common.H{
				"enabled": false,
			},
		})
		return
	}
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	data := common.H{
		"enabled":      true,
		"last_used_at": credential.LastUsedAt,
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    data,
	})
}

func PasskeyLoginBegin(c contract.Context) {
	if !passkey.GetPasskeySettings().Enabled {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "管理员未启用 Passkey 登录",
		})
		return
	}

	wa, err := BuildWebAuthn(c.HTTPRequest())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	assertion, sessionData, err := wa.BeginDiscoverableLogin()
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	flowToken, expiresAt, err := CreateSessionDataFlow(
		AuthFlowPurposePasskeyLogin,
		0,
		"",
		"",
		sessionData,
	)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data": common.H{
			"options":    assertion,
			"flow_token": flowToken,
			"expires_at": expiresAt,
		},
	})
}

func PasskeyLoginFinish(c contract.Context) {
	if !passkey.GetPasskeySettings().Enabled {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "管理员未启用 Passkey 登录",
		})
		return
	}

	request, err := parsePasskeyFinishRequest(c)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	parsedCredential, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	wa, err := BuildWebAuthn(c.HTTPRequest())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	sessionData, _, err := PopSessionDataFlow(
		request.FlowToken,
		AuthFlowPurposePasskeyLogin,
		0,
		"",
	)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	handler := func(rawID, userHandle []byte) (webauthnlib.User, error) {
		// 首先通过凭证ID查找用户
		credential, err := GetPasskeyByCredentialID(rawID)
		if err != nil {
			return nil, fmt.Errorf("未找到 Passkey 凭证: %w", err)
		}

		// 通过凭证获取用户
		user := &User{Id: credential.UserID}
		if err := user.FillUserById(); err != nil {
			return nil, fmt.Errorf("用户信息获取失败: %w", err)
		}

		if user.Status != common.UserStatusEnabled {
			return nil, errors.New("该用户已被禁用")
		}

		if len(userHandle) > 0 {
			userID, parseErr := strconv.Atoi(string(userHandle))
			if parseErr != nil {
				// 记录异常但继续验证，因为某些客户端可能使用非数字格式
				common.SysLog(fmt.Sprintf("PasskeyLogin: userHandle parse error for credential, length: %d", len(userHandle)))
			} else if userID != user.Id {
				return nil, errors.New("用户句柄与凭证不匹配")
			}
		}

		return NewWebAuthnUser(user, credential), nil
	}

	waUser, credential, err := wa.ValidatePasskeyLogin(handler, *sessionData, parsedCredential)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	userWrapper, ok := waUser.(*WebAuthnUser)
	if !ok {
		common.CtxApiErrorMsg(c, "Passkey 登录状态异常")
		return
	}

	modelUser := userWrapper.ModelUser()
	if modelUser == nil {
		common.CtxApiErrorMsg(c, "Passkey 登录状态异常")
		return
	}

	if modelUser.Status != common.UserStatusEnabled {
		common.CtxApiErrorMsg(c, "该用户已被禁用")
		return
	}

	if err := UpdatePasskeyAssertionState(modelUser.Id, credential, time.Now()); err != nil {
		common.CtxApiError(c, err)
		return
	}

	SetupLogin(modelUser, c)
}

func AdminResetPasskey(c contract.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.CtxApiErrorMsg(c, "无效的用户 ID")
		return
	}

	user := &User{Id: id}
	if err := user.FillUserById(); err != nil {
		common.CtxApiError(c, err)
		return
	}
	myRole := c.GetInt("role")
	if !canManageTargetRole(myRole, user.Role) {
		common.CtxApiErrorMsg(c, "no permission")
		return
	}

	if _, err := GetPasskeyByUserID(user.Id); err != nil {
		if errors.Is(err, ErrPasskeyNotFound) {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "该用户尚未绑定 Passkey",
			})
			return
		}
		common.CtxApiError(c, err)
		return
	}

	if err := DeletePasskeyByUserIDWithAuthVersion(user.Id); err != nil {
		common.CtxApiError(c, err)
		return
	}
	if _, err := RevokeAllUserSessions(user.Id, "admin_passkey_reset"); err != nil {
		common.CtxApiError(c, err)
		return
	}

	writeManageAudit(c, user.Id, "user.reset_passkey", map[string]interface{}{
		"username": user.Username,
		"id":       user.Id,
	})
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "Passkey 已重置",
	})
}

func PasskeyVerifyBegin(c contract.Context) {
	if !passkey.GetPasskeySettings().Enabled {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "管理员未启用 Passkey 登录",
		})
		return
	}

	user, err := getAuthenticatedUser(c)
	if err != nil {
		_ = c.JSON(http.StatusUnauthorized, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	var request passkeyVerifyBeginRequest
	if err := c.BindJSON(&request); err != nil {
		common.CtxApiError(c, errors.New("无效的 Passkey 验证请求"))
		return
	}
	if !isAllowedSecurityProofScope(request.Scope) {
		common.CtxApiError(c, errors.New("不支持的安全验证范围"))
		return
	}

	credential, err := GetPasskeyByUserID(user.Id)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "该用户尚未绑定 Passkey",
		})
		return
	}

	wa, err := BuildWebAuthn(c.HTTPRequest())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	waUser := NewWebAuthnUser(user, credential)
	assertion, sessionData, err := wa.BeginLogin(waUser)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	identity, ok := authtoken.ReadSessionAuthIdentity(c)
	if !ok {
		common.CtxApiError(c, errors.New("当前认证方式不支持安全验证"))
		return
	}
	flowToken, expiresAt, err := CreateSessionDataFlow(
		AuthFlowPurposePasskeyStepUp,
		user.Id,
		identity.SessionID,
		request.Scope,
		sessionData,
	)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data": common.H{
			"options":    assertion,
			"flow_token": flowToken,
			"expires_at": expiresAt,
		},
	})
}

func PasskeyVerifyFinish(c contract.Context) {
	if !passkey.GetPasskeySettings().Enabled {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "管理员未启用 Passkey 登录",
		})
		return
	}

	user, err := getAuthenticatedUser(c)
	if err != nil {
		_ = c.JSON(http.StatusUnauthorized, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	request, err := parsePasskeyFinishRequest(c)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	parsedCredential, err := protocol.ParseCredentialRequestResponseBytes(request.Credential)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	wa, err := BuildWebAuthn(c.HTTPRequest())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	credential, err := GetPasskeyByUserID(user.Id)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "该用户尚未绑定 Passkey",
		})
		return
	}

	identity, ok := authtoken.ReadSessionAuthIdentity(c)
	if !ok {
		common.CtxApiError(c, errors.New("当前认证方式不支持安全验证"))
		return
	}
	sessionData, scope, err := PopSessionDataFlow(
		request.FlowToken,
		AuthFlowPurposePasskeyStepUp,
		user.Id,
		identity.SessionID,
	)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	waUser := NewWebAuthnUser(user, credential)
	validatedCredential, err := wa.ValidateLogin(waUser, *sessionData, parsedCredential)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	if err := UpdatePasskeyAssertionState(user.Id, validatedCredential, time.Now()); err != nil {
		common.CtxApiError(c, err)
		return
	}

	proofToken, proofExpiresAt, err := IssueSecurityProof(identity, secureVerificationMethodPasskey, []string{scope})
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "Passkey 验证成功",
		"data": common.H{
			"proof_token": proofToken,
			"expires_at":  proofExpiresAt,
			"method":      secureVerificationMethodPasskey,
			"scope":       scope,
		},
	})
}

func getAuthenticatedUser(c contract.Context) (*User, error) {
	id := c.GetInt("id")
	if id == 0 {
		return nil, errors.New("未登录")
	}
	user := &User{Id: id}
	if err := user.FillUserById(); err != nil {
		return nil, err
	}
	if user.Status != common.UserStatusEnabled {
		return nil, errors.New("该用户已被禁用")
	}
	return user, nil
}

func requirePasskeyRegistrationVerification(c contract.Context, userID int) bool {
	twoFA, err := GetTwoFAByUserId(userID)
	if err != nil {
		common.CtxApiError(c, err)
		return false
	}
	if twoFA == nil || !twoFA.IsEnabled {
		return true
	}
	return RequireSecurityProof(c, SecurityProofScopePasskeyRegister, []string{secureVerificationMethod2FA})
}

func requirePasskeyDeleteVerification(c contract.Context, userID int) bool {
	twoFA, err := GetTwoFAByUserId(userID)
	if err != nil {
		common.CtxApiError(c, err)
		return false
	}
	if twoFA != nil && twoFA.IsEnabled {
		return RequireSecurityProof(c, SecurityProofScopePasskeyDelete, []string{secureVerificationMethod2FA})
	}

	_, err = GetPasskeyByUserID(userID)
	if err != nil {
		if errors.Is(err, ErrPasskeyNotFound) {
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": "该用户尚未绑定 Passkey",
			})
			return false
		}
		common.CtxApiError(c, err)
		return false
	}

	return RequireSecurityProof(c, SecurityProofScopePasskeyDelete, []string{secureVerificationMethodPasskey})
}
