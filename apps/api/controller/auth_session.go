package controller

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/security"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"gorm.io/gorm"
)

func RefreshAuth(c contract.Context) {
	setAuthNoStore(c)
	rawRefreshToken, err := c.Cookie(service.RefreshCookieName)
	if err != nil || rawRefreshToken == "" {
		service.ClearRefreshCookie(c)
		writeAuthSessionError(c, service.ErrRefreshTokenInvalid)
		return
	}
	bundle, user, err := service.RefreshLoginSession(rawRefreshToken, c.Header("X-Auth-Session"), c.ClientIP(), c.UserAgent())
	if err != nil {
		if errors.Is(err, service.ErrRefreshTokenInvalid) || errors.Is(err, service.ErrLoginSessionRevoked) {
			service.ClearRefreshCookie(c)
		}
		writeAuthSessionError(c, err)
		return
	}
	service.WriteRefreshCookie(c, bundle.RefreshToken)
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data": common.H{
			"access_token":      bundle.AccessToken,
			"token_type":        bundle.TokenType,
			"access_expires_at": bundle.AccessExpiresAt,
			"user":              buildSelfUserData(user),
			"session":           bundle.Session,
		},
	})
}

func AuthLogout(c contract.Context) {
	setAuthNoStore(c)
	expectedSID := strings.TrimSpace(c.Header("X-Auth-Session"))
	rawRefreshToken, cookieErr := c.Cookie(service.RefreshCookieName)
	cookieSID, hasCookieSID := service.RefreshTokenSID(rawRefreshToken)
	if expectedSID != "" && cookieErr == nil && hasCookieSID && cookieSID != expectedSID {
		writeAuthSessionError(c, service.ErrLoginSessionMismatch)
		return
	}

	if rawAccessToken, ok := dashboardBearer(c.Header("Authorization")); ok {
		if identity, err := service.ParseAccessToken(rawAccessToken); err == nil {
			if expectedSID != "" && expectedSID != identity.SessionID {
				writeAuthSessionError(c, service.ErrLoginSessionMismatch)
				return
			}
			if _, err := model.RevokeUserSession(identity.UserID, identity.SessionID, "logout"); err != nil {
				writeAuthSessionError(c, err)
				return
			}
			cookieCleared := false
			if cookieErr == nil && hasCookieSID && cookieSID == identity.SessionID {
				if err := service.RevokeByRefreshToken(rawRefreshToken, identity.SessionID, "logout"); err != nil {
					writeAuthSessionError(c, err)
					return
				}
				service.ClearRefreshCookie(c)
				cookieCleared = true
			}
			_ = c.JSON(http.StatusOK, common.H{
				"success": true,
				"message": "",
				"data":    common.H{"revoked_sid": identity.SessionID, "cookie_cleared": cookieCleared},
			})
			return
		}
	}
	if cookieErr != nil || rawRefreshToken == "" {
		service.ClearRefreshCookie(c)
		_ = c.JSON(http.StatusOK, common.H{"success": true, "message": ""})
		return
	}
	if err := service.RevokeByRefreshToken(rawRefreshToken, expectedSID, "logout"); err != nil {
		writeAuthSessionError(c, err)
		return
	}
	service.ClearRefreshCookie(c)
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": ""})
}

func GetLoginSessions(c contract.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	sessions, err := service.ListLoginSessions(identity.UserID, identity.SessionID)
	if err != nil {
		writeAuthSessionError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": "", "data": sessions})
}

func DeleteLoginSession(c contract.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	sid := strings.TrimSpace(c.Param("sid"))
	if sid == "" {
		_ = c.JSON(http.StatusBadRequest, common.H{"success": false, "code": "AUTH_SESSION_ID_REQUIRED", "message": "session id is required"})
		return
	}
	revoked, err := model.RevokeUserSession(identity.UserID, sid, "user_revoked")
	if err != nil {
		writeAuthSessionError(c, err)
		return
	}
	if !revoked {
		_ = c.JSON(http.StatusNotFound, common.H{"success": false, "code": "AUTH_SESSION_NOT_FOUND", "message": "session not found"})
		return
	}
	if rawRefreshToken, cookieErr := c.Cookie(service.RefreshCookieName); cookieErr == nil {
		cookieSID, ok := service.RefreshTokenSID(rawRefreshToken)
		if ok && cookieSID == sid {
			service.ClearRefreshCookie(c)
		}
	}
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": "", "data": common.H{"revoked_sid": sid, "current": sid == identity.SessionID}})
}

func RevokeOtherLoginSessions(c contract.Context) {
	identity, ok := requireBrowserSession(c)
	if !ok {
		return
	}
	count, err := model.RevokeOtherUserSessions(identity.UserID, identity.SessionID, "user_revoked_others")
	if err != nil {
		writeAuthSessionError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": "", "data": common.H{"revoked_count": count}})
}

func requireBrowserSession(c contract.Context) (service.AuthIdentity, bool) {
	identity, ok := security.GetSessionAuthIdentity(c)
	if !ok {
		_ = c.JSON(http.StatusForbidden, common.H{
			"success": false,
			"code":    "AUTH_SESSION_REQUIRED",
			"message": "a dashboard login session is required",
		})
		return service.AuthIdentity{}, false
	}
	return identity, true
}

func writeAuthSessionError(c contract.Context, err error) {
	status, code := service.AuthSessionErrorCode(err)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status, code = http.StatusUnauthorized, "AUTH_UNAUTHORIZED"
	}
	if status == http.StatusInternalServerError {
		// The response body only carries the generic AUTH_INTERNAL_ERROR
		// code; without this log the underlying Redis/database/session
		// failure is indistinguishable from the client side.
		logger.LogError(c.Context(), fmt.Sprintf("auth session internal error (%s %s): %v", c.Method(), c.Path(), err))
	}
	_ = c.JSON(status, common.H{"success": false, "code": code, "message": http.StatusText(status)})
}

func setAuthNoStore(c contract.Context) {
	c.SetHeader("Cache-Control", "no-store")
}

func authRotationData(bundle *service.AuthBundle) common.H {
	return common.H{
		"access_token":      bundle.AccessToken,
		"token_type":        bundle.TokenType,
		"access_expires_at": bundle.AccessExpiresAt,
		"session":           bundle.Session,
	}
}

func dashboardBearer(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}
