package controller

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	identitymodel "github.com/QuantumNous/new-api/internal/identity/model"
	identityservice "github.com/QuantumNous/new-api/internal/identity/service"
	rootmodel "github.com/QuantumNous/new-api/model"
)

const (
	// The legacy Telegram widget has no nonce. Keep its signed assertion short-lived
	// so captured callbacks cannot be reused indefinitely.
	telegramAuthorizationMaxAge     = 5 * time.Minute
	telegramAuthorizationFutureSkew = 2 * time.Minute
	telegramBindFlowTTL             = 5 * time.Minute

	telegramBindErrorDisabled       = "TELEGRAM_BIND_DISABLED"
	telegramBindErrorInvalidRequest = "TELEGRAM_BIND_INVALID_REQUEST"
	telegramBindErrorFlowInvalid    = "TELEGRAM_BIND_FLOW_INVALID"
	telegramBindErrorSessionInvalid = "TELEGRAM_BIND_SESSION_INVALID"
	telegramBindErrorAlreadyBound   = "TELEGRAM_BIND_ALREADY_BOUND"
	telegramBindErrorUserDeleted    = "TELEGRAM_BIND_USER_DELETED"
	telegramBindErrorUserDisabled   = "TELEGRAM_BIND_USER_DISABLED"
	telegramBindErrorInternal       = "TELEGRAM_BIND_INTERNAL_ERROR"
)

var (
	errTelegramAccountAlreadyBound  = errors.New("telegram account is already bound")
	errTelegramBindAssertionInvalid = errors.New("telegram bind assertion is invalid")
	errTelegramBindUserDeleted      = errors.New("telegram bind user was deleted")
	errTelegramBindUserDisabled     = errors.New("telegram bind user is disabled")
)

func TelegramBindStart(c *gin.Context) {
	if !common.TelegramOAuthEnabled {
		c.JSON(http.StatusOK, gin.H{
			"message": "管理员未开启通过 Telegram 登录以及注册",
			"success": false,
		})
		return
	}
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "未登录"})
		return
	}
	expiresAt := time.Now().Add(telegramBindFlowTTL)
	flowToken, _, err := identitymodel.CreateAuthFlow(identitymodel.AuthFlowCreate{
		Purpose:   identitymodel.AuthFlowPurposeTelegramBind,
		UserId:    identity.UserID,
		SessionId: identity.SessionID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	callbackURL := "/api/oauth/telegram/bind/" + flowToken
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"flow_token":   flowToken,
			"callback_url": callbackURL,
			"expires_at":   expiresAt.Unix(),
		},
	})
}

func TelegramBind(c *gin.Context) {
	if !common.TelegramOAuthEnabled {
		telegramBindFailure(c, telegramBindErrorDisabled)
		return
	}
	params := c.Request.URL.Query()
	telegramId, err := verifyTelegramAuthorization(params, common.TelegramBotToken, time.Now())
	if err != nil {
		common.SysLog("TelegramBind authorization failed: " + err.Error())
		telegramBindFailure(c, telegramBindErrorInvalidRequest)
		return
	}
	pendingFlow, err := identitymodel.GetAuthFlow(c.Param("flow_token"), identitymodel.AuthFlowMatch{
		Purpose: identitymodel.AuthFlowPurposeTelegramBind,
	})
	if err != nil {
		if !errors.Is(err, identitymodel.ErrAuthFlowInvalid) &&
			!errors.Is(err, identitymodel.ErrAuthFlowExpired) &&
			!errors.Is(err, identitymodel.ErrAuthFlowConsumed) {
			common.SysError("TelegramBind flow lookup failed: " + err.Error())
			telegramBindFailure(c, telegramBindErrorInternal)
			return
		}
		telegramBindFailure(c, telegramBindErrorFlowInvalid)
		return
	}
	if _, err := identityservice.ValidateSessionReference(pendingFlow.UserId, pendingFlow.SessionId); err != nil {
		if !errors.Is(err, identityservice.ErrLoginSessionInvalid) &&
			!errors.Is(err, identityservice.ErrLoginSessionRevoked) &&
			!errors.Is(err, identitymodel.ErrUserSessionInactive) &&
			!errors.Is(err, gorm.ErrRecordNotFound) {
			common.SysError("TelegramBind session validation failed: " + err.Error())
			telegramBindFailure(c, telegramBindErrorInternal)
			return
		}

		var user identitymodel.User
		userErr := rootmodel.DB.First(&user, pendingFlow.UserId).Error
		switch {
		case errors.Is(userErr, gorm.ErrRecordNotFound):
			telegramBindFailure(c, telegramBindErrorUserDeleted)
		case userErr != nil:
			common.SysError("TelegramBind user status lookup failed: " + userErr.Error())
			telegramBindFailure(c, telegramBindErrorInternal)
		case user.Status != common.UserStatusEnabled:
			telegramBindFailure(c, telegramBindErrorUserDisabled)
		default:
			telegramBindFailure(c, telegramBindErrorSessionInvalid)
		}
		return
	}
	assertion, assertionExpiresAt, err := telegramAuthorizationClaim(params, time.Now())
	if err != nil {
		common.SysLog("TelegramBind authorization claim failed: " + err.Error())
		telegramBindFailure(c, telegramBindErrorInvalidRequest)
		return
	}
	_, err = identitymodel.ConsumeAuthFlowWithAction(c.Param("flow_token"), identitymodel.AuthFlowMatch{
		Purpose:   identitymodel.AuthFlowPurposeTelegramBind,
		UserId:    pendingFlow.UserId,
		SessionId: pendingFlow.SessionId,
	}, func(tx *gorm.DB, flow *identitymodel.AuthFlow) error {
		if err := identitymodel.ClaimExternalAuthAssertionWithTx(tx, identitymodel.AuthFlowPurposeTelegramAssertion, assertion, assertionExpiresAt); err != nil {
			if errors.Is(err, identitymodel.ErrAuthFlowInvalid) || errors.Is(err, identitymodel.ErrAuthFlowConsumed) {
				return errors.Join(errTelegramBindAssertionInvalid, err)
			}
			return err
		}

		var user identitymodel.User
		if err := tx.First(&user, flow.UserId).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errTelegramBindUserDeleted
			}
			return err
		}
		if user.Status != common.UserStatusEnabled {
			return errTelegramBindUserDisabled
		}

		var session identitymodel.UserSession
		if err := tx.Where("sid = ? AND user_id = ?", flow.SessionId, flow.UserId).First(&session).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return identityservice.ErrLoginSessionRevoked
			}
			return err
		}
		if session.Status != identitymodel.UserSessionStatusActive || session.RevokedAt != 0 || session.ExpiresAt <= time.Now().Unix() {
			return identityservice.ErrLoginSessionRevoked
		}
		if session.UserAuthVersion != user.AuthVersion {
			return identityservice.ErrLoginSessionRevoked
		}
		if user.TelegramId != "" {
			return errTelegramAccountAlreadyBound
		}
		if err := identitymodel.ClaimExternalIdentityWithTx(
			tx,
			identitymodel.ExternalIdentityProviderTelegram,
			telegramId,
			user.Id,
		); err != nil {
			if errors.Is(err, identitymodel.ErrExternalIdentityAlreadyClaimed) {
				return errTelegramAccountAlreadyBound
			}
			return err
		}
		result := tx.Model(&identitymodel.User{}).
			Where("id = ? AND status = ? AND auth_version = ? AND telegram_id = ?", user.Id, common.UserStatusEnabled, user.AuthVersion, "").
			Update("telegram_id", telegramId)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errTelegramAccountAlreadyBound
		}
		return nil
	})
	if err != nil {
		switch {
		case errors.Is(err, errTelegramBindAssertionInvalid):
			telegramBindFailure(c, telegramBindErrorInvalidRequest)
		case errors.Is(err, errTelegramAccountAlreadyBound):
			telegramBindFailure(c, telegramBindErrorAlreadyBound)
		case errors.Is(err, errTelegramBindUserDeleted):
			telegramBindFailure(c, telegramBindErrorUserDeleted)
		case errors.Is(err, errTelegramBindUserDisabled):
			telegramBindFailure(c, telegramBindErrorUserDisabled)
		case errors.Is(err, identityservice.ErrLoginSessionRevoked):
			telegramBindFailure(c, telegramBindErrorSessionInvalid)
		case errors.Is(err, identitymodel.ErrAuthFlowInvalid), errors.Is(err, identitymodel.ErrAuthFlowExpired), errors.Is(err, identitymodel.ErrAuthFlowConsumed):
			telegramBindFailure(c, telegramBindErrorFlowInvalid)
		default:
			common.SysError("TelegramBind failed: " + err.Error())
			telegramBindFailure(c, telegramBindErrorInternal)
		}
		return
	}

	callback := "/oauth/telegram?telegram_bind=success&flow_token=" + url.QueryEscape(c.Param("flow_token"))
	c.Redirect(http.StatusFound, callback)
}

func telegramBindFailure(c *gin.Context, errorCode string) {
	query := url.Values{
		"telegram_bind": {"error"},
		"flow_token":    {c.Param("flow_token")},
		"error_code":    {errorCode},
	}
	c.Redirect(http.StatusFound, "/oauth/telegram?"+query.Encode())
}

func TelegramLogin(c *gin.Context) {
	if !common.TelegramOAuthEnabled {
		c.JSON(200, gin.H{
			"message": "管理员未开启通过 Telegram 登录以及注册",
			"success": false,
		})
		return
	}
	params := c.Request.URL.Query()
	telegramId, err := verifyTelegramAuthorization(params, common.TelegramBotToken, time.Now())
	if err != nil {
		common.SysLog("TelegramLogin authorization failed: " + err.Error())
		c.JSON(200, gin.H{
			"message": "无效的请求",
			"success": false,
		})
		return
	}

	user := identitymodel.User{TelegramId: telegramId}
	if err := user.FillUserByTelegramId(); err != nil {
		c.JSON(200, gin.H{
			"message": err.Error(),
			"success": false,
		})
		return
	}
	if err := claimTelegramAuthorization(params, time.Now()); err != nil {
		common.SysLog("TelegramLogin assertion replay rejected: " + err.Error())
		c.JSON(http.StatusForbidden, gin.H{
			"message": "该登录凭据已被使用",
			"success": false,
		})
		return
	}
	setupLogin(&user, c)
}

func claimTelegramAuthorization(params url.Values, now time.Time) error {
	assertion, expiresAt, err := telegramAuthorizationClaim(params, now)
	if err != nil {
		return err
	}
	return identitymodel.ClaimExternalAuthAssertion(identitymodel.AuthFlowPurposeTelegramAssertion, assertion, expiresAt)
}

func telegramAuthorizationClaim(params url.Values, now time.Time) (string, time.Time, error) {
	authDate, err := strconv.ParseInt(params.Get("auth_date"), 10, 64)
	if err != nil {
		return "", time.Time{}, errors.New("telegram authorization date is invalid")
	}
	hashBytes, err := hex.DecodeString(params.Get("hash"))
	if err != nil {
		return "", time.Time{}, errors.New("telegram authorization signature is invalid")
	}
	expiresAt := time.Unix(authDate, 0).Add(telegramAuthorizationMaxAge)
	if !expiresAt.After(now) {
		return "", time.Time{}, errors.New("telegram authorization has expired")
	}
	return hex.EncodeToString(hashBytes), expiresAt, nil
}

func verifyTelegramAuthorization(params url.Values, token string, now time.Time) (string, error) {
	if token == "" {
		return "", errors.New("telegram bot token is empty")
	}
	for _, values := range params {
		if len(values) != 1 {
			return "", errors.New("telegram authorization contains duplicate parameters")
		}
	}

	telegramID := params.Get("id")
	hash := params.Get("hash")
	authDateText := params.Get("auth_date")
	if telegramID == "" || hash == "" || authDateText == "" {
		return "", errors.New("telegram authorization is incomplete")
	}
	authDate, err := strconv.ParseInt(authDateText, 10, 64)
	if err != nil {
		return "", errors.New("telegram authorization date is invalid")
	}
	if authDate < now.Add(-telegramAuthorizationMaxAge).Unix() ||
		authDate > now.Add(telegramAuthorizationFutureSkew).Unix() {
		return "", errors.New("telegram authorization has expired")
	}

	strs := make([]string, 0, len(params)-1)
	for k, v := range params {
		if k == "hash" {
			continue
		}
		strs = append(strs, k+"="+v[0])
	}
	sort.Strings(strs)
	secret := sha256.Sum256([]byte(token))
	mac := hmac.New(sha256.New, secret[:])
	_, _ = mac.Write([]byte(strings.Join(strs, "\n")))
	providedHash, err := hex.DecodeString(hash)
	if err != nil || !hmac.Equal(providedHash, mac.Sum(nil)) {
		return "", errors.New("telegram authorization signature is invalid")
	}

	return telegramID, nil
}
