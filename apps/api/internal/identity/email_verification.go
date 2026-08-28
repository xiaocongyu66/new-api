package identity

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/i18n"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

type PasswordResetRequest struct {
	Email string `json:"email"`
	Token string `json:"token"`
}

// SendEmailVerification handles GET /api/verification. It sends a 6-digit code
// to the given email if it is not already registered and passes the configured
// domain/alias restrictions.
func SendEmailVerification(c contract.Context) {
	email := NormalizeEmail(c.Query("email"))
	if err := common.Validate.Var(email, "required,email"); err != nil {
		common.CtxApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		_ = c.JSON(200, common.H{
			"success": false,
			"message": "无效的邮箱地址",
		})
		return
	}
	localPart := parts[0]
	domainPart := parts[1]
	if common.EmailDomainRestrictionEnabled {
		allowed := false
		for _, domain := range common.EmailDomainWhitelist {
			if domainPart == domain {
				allowed = true
				break
			}
		}
		if !allowed {
			_ = c.JSON(200, common.H{
				"success": false,
				"message": "The administrator has enabled the email domain name whitelist, and your email address is not allowed due to special symbols or it's not in the whitelist.",
			})
			return
		}
	}
	if common.EmailAliasRestrictionEnabled {
		containsSpecialSymbols := strings.Contains(localPart, "+") || strings.Contains(localPart, ".")
		if containsSpecialSymbols {
			_ = c.JSON(200, common.H{
				"success": false,
				"message": "管理员已启用邮箱地址别名限制，您的邮箱地址由于包含特殊符号而被拒绝。",
			})
			return
		}
	}

	if IsEmailAlreadyTaken(email) {
		common.CtxApiErrorI18n(c, i18n.MsgUserEmailAlreadyTaken)
		return
	}
	code := common.GenerateVerificationCode(6)
	common.RegisterVerificationCodeWithKey(email, code, common.EmailVerificationPurpose)
	subject := fmt.Sprintf("%s邮箱验证邮件", common.SystemName)
	content := fmt.Sprintf("<p>您好，你正在进行%s邮箱验证。</p>"+
		"<p>您的验证码为: <strong>%s</strong></p>"+
		"<p>验证码 %d 分钟内有效，如果不是本人操作，请忽略。</p>", common.SystemName, code, common.VerificationValidMinutes)
	err := common.SendEmail(subject, email, content)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(200, common.H{
		"success": true,
		"message": "",
	})
}

// SendPasswordResetEmail handles GET /api/reset_password. The presence of the
// user is not revealed: the same success response is returned whether or not
// the email exists, but a real reset link is only generated when a user
// matches the email.
func SendPasswordResetEmail(c contract.Context) {
	email := NormalizeEmail(c.Query("email"))
	if err := common.Validate.Var(email, "required,email"); err != nil {
		common.CtxApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if _, err := GetUniqueUserByEmail(email); err == nil {
		code := common.GenerateVerificationCode(0)
		common.RegisterVerificationCodeWithKey(email, code, common.PasswordResetPurpose)
		link := fmt.Sprintf("%s/user/reset?email=%s&token=%s", system_setting.ServerAddress, email, code)
		subject := fmt.Sprintf("%s密码重置", common.SystemName)
		content := fmt.Sprintf("<p>您好，你正在进行%s密码重置。</p>"+
			"<p>点击 <a href='%s'>此处</a> 进行密码重置。</p>"+
			"<p>如果链接无法点击，请尝试点击下面的链接或将其复制到浏览器中打开：<br> %s </p>"+
			"<p>重置链接 %d 分钟内有效，如果不是本人操作，请忽略。</p>", common.SystemName, link, link, common.VerificationValidMinutes)
		err := common.SendEmail(subject, email, content)
		if err != nil {
			logger.LogError(c.Context(), fmt.Sprintf("failed to send password reset email to %s: %s", email, err.Error()))
		}
	} else if err != nil && !errors.Is(err, ErrEmailNotFound) {
		logger.LogWarn(c.Context(), fmt.Sprintf("skip password reset email for %s: %s", email, err.Error()))
	}
	_ = c.JSON(200, common.H{
		"success": true,
		"message": "",
	})
}

// ResetPassword handles POST /api/user/reset. It validates the emailed token
// and rotates the user password to a fresh random value.
func ResetPassword(c contract.Context) {
	var req PasswordResetRequest
	err := c.BindJSON(&req)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	req.Email = NormalizeEmail(req.Email)
	if req.Email == "" || req.Token == "" {
		common.CtxApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if !common.VerifyCodeWithKey(req.Email, req.Token, common.PasswordResetPurpose) {
		common.CtxApiErrorI18n(c, i18n.MsgUserPasswordResetLinkInvalid)
		return
	}
	password := common.GenerateVerificationCode(12)
	err = ResetUserPasswordByEmail(req.Email, password)
	if err != nil {
		if errors.Is(err, ErrEmailNotFound) || errors.Is(err, ErrEmailAmbiguous) {
			common.CtxApiErrorI18n(c, i18n.MsgUserPasswordResetLinkInvalid)
			return
		}
		common.CtxApiError(c, err)
		return
	}
	common.DeleteKey(req.Email, common.PasswordResetPurpose)
	_ = c.JSON(200, common.H{
		"success": true,
		"message": "",
		"data":    password,
	})
}
