package identity

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/google/uuid"
)

const RefreshCookieName = "new_api_refresh"

var (
	ErrLoginSessionInvalid  = errors.New("login session is invalid")
	ErrLoginSessionRevoked  = errors.New("login session is revoked")
	ErrLoginSessionMismatch = errors.New("login session does not match the expected session")
	ErrRefreshTokenInvalid  = errors.New("refresh token is invalid")
	ErrRefreshRace          = errors.New("refresh token was already rotated")
)

type LoginSessionView struct {
	SID          string `json:"sid"`
	Current      bool   `json:"current"`
	LoginMethod  string `json:"login_method"`
	IP           string `json:"ip"`
	UserAgent    string `json:"user_agent"`
	CreatedAt    int64  `json:"created_at"`
	LastActiveAt int64  `json:"last_active_at"`
	ExpiresAt    int64  `json:"expires_at"`
}

type AuthBundle struct {
	AccessToken     string           `json:"access_token"`
	TokenType       string           `json:"token_type"`
	AccessExpiresAt int64            `json:"access_expires_at"`
	Session         LoginSessionView `json:"session"`
	RefreshToken    string           `json:"-"`
}

func CreateLoginSession(userID int, loginMethod, ip, userAgent string) (*AuthBundle, error) {
	return createLoginSession(userID, 0, loginMethod, ip, userAgent)
}

func CreateLoginSessionAtAuthVersion(userID int, expectedAuthVersion int64, loginMethod, ip, userAgent string) (*AuthBundle, error) {
	if expectedAuthVersion <= 0 {
		return nil, ErrLoginSessionInvalid
	}
	return createLoginSession(userID, expectedAuthVersion, loginMethod, ip, userAgent)
}

func createLoginSession(userID int, expectedAuthVersion int64, loginMethod, ip, userAgent string) (*AuthBundle, error) {
	user, err := GetUserCache(userID)
	if err != nil {
		return nil, err
	}
	if user.Status != common.UserStatusEnabled || user.AuthVersion <= 0 {
		return nil, ErrLoginSessionInvalid
	}
	if expectedAuthVersion > 0 && user.AuthVersion != expectedAuthVersion {
		return nil, ErrLoginSessionRevoked
	}
	now := time.Now().Unix()
	activeCount, err := CountActiveUserSessions(userID, now)
	if err != nil {
		return nil, err
	}
	if activeCount >= int64(common.UserSessionActiveLimit) {
		return nil, ErrUserSessionLimit
	}
	issuanceCount, err := CountUserSessionsCreatedSince(userID, now-common.UserSessionIssuanceWindowSeconds)
	if err != nil {
		return nil, err
	}
	if issuanceCount >= int64(common.UserSessionIssuanceLimit) {
		return nil, ErrUserSessionIssuanceLimit
	}
	refreshSecret, err := common.GenerateRandomCharsKey(64)
	if err != nil {
		return nil, err
	}
	session := &UserSession{
		SID:             uuid.NewString(),
		UserID:          userID,
		Version:         1,
		UserAuthVersion: user.AuthVersion,
		Status:          UserSessionStatusActive,
		RefreshHash:     hashRefreshSecret(refreshSecret),
		LoginMethod:     strings.TrimSpace(loginMethod),
		IP:              truncateAuthMetadata(ip, 64),
		UserAgent:       truncateAuthMetadata(userAgent, 512),
		CreatedAt:       now,
		LastActiveAt:    now,
		ExpiresAt:       time.Unix(now, 0).Add(LoginSessionTTL).Unix(),
	}
	if session.LoginMethod == "" {
		session.LoginMethod = "unknown"
	}
	if err := CreateUserSession(session); err != nil {
		return nil, err
	}
	bundle, err := issueAuthBundle(session, session.SID+"."+refreshSecret, true)
	if err != nil {
		_, _ = RevokeUserSession(userID, session.SID, "token_issue_failed")
		return nil, err
	}
	return bundle, nil
}

func ValidateLoginSession(identity AuthIdentity) (*UserSession, *UserBase, error) {
	session, err := GetUserSessionCached(identity.SessionID)
	if err != nil {
		if errors.Is(err, ErrUserSessionInactive) {
			return nil, nil, ErrLoginSessionRevoked
		}
		return nil, nil, err
	}
	now := time.Now().Unix()
	if session.UserID != identity.UserID || session.Status != UserSessionStatusActive || session.RevokedAt != 0 || session.ExpiresAt <= now || session.Version != identity.SessionVersion || session.UserAuthVersion != identity.UserAuthVersion {
		return nil, nil, ErrLoginSessionRevoked
	}
	user, err := GetUserCache(identity.UserID)
	if err != nil {
		return nil, nil, err
	}
	if user.Status != common.UserStatusEnabled || user.AuthVersion != identity.UserAuthVersion {
		return nil, nil, ErrLoginSessionRevoked
	}
	return session, user, nil
}

// ValidateSessionReference validates a server-side flow bound to an existing
// dashboard session without requiring an access token on the callback request.
func ValidateSessionReference(userID int, sid string) (AuthIdentity, error) {
	if userID <= 0 || strings.TrimSpace(sid) == "" {
		return AuthIdentity{}, ErrLoginSessionInvalid
	}
	session, err := GetUserSessionCached(sid)
	if err != nil {
		return AuthIdentity{}, err
	}
	identity := AuthIdentity{
		UserID:          userID,
		SessionID:       sid,
		UserAuthVersion: session.UserAuthVersion,
		SessionVersion:  session.Version,
	}
	if _, _, err := ValidateLoginSession(identity); err != nil {
		return AuthIdentity{}, err
	}
	return identity, nil
}

// AdvanceCurrentSessionSecurity increments the user's global auth version,
// preserves only the current browser session at a new session version and
// returns a replacement access token. Call after a successful 2FA/passkey
// security-setting mutation that did not already advance AuthVersion.
func AdvanceCurrentSessionSecurity(identity AuthIdentity, reason string) (*AuthBundle, error) {
	nextUserAuthVersion, err := BumpUserAuthVersion(identity.UserID)
	if err != nil {
		return nil, err
	}
	return advanceCurrentSessionToVersion(identity, nextUserAuthVersion, reason)
}

// AdvanceCurrentSessionToUserVersion is used when the security mutation and
// AuthVersion increment were committed in the same transaction (for example,
// a password change).
func AdvanceCurrentSessionToUserVersion(identity AuthIdentity, reason string) (*AuthBundle, error) {
	user, err := GetUserCache(identity.UserID)
	if err != nil {
		return nil, err
	}
	if user.Status != common.UserStatusEnabled || user.AuthVersion <= identity.UserAuthVersion {
		return nil, ErrLoginSessionRevoked
	}
	return advanceCurrentSessionToVersion(identity, user.AuthVersion, reason)
}

func advanceCurrentSessionToVersion(identity AuthIdentity, nextUserAuthVersion int64, reason string) (*AuthBundle, error) {
	session, err := AdvanceUserSessionAuthVersion(
		identity.UserID,
		identity.SessionID,
		identity.SessionVersion,
		identity.UserAuthVersion,
		nextUserAuthVersion,
	)
	if err != nil {
		return nil, err
	}
	if _, err := RevokeOtherUserSessions(identity.UserID, identity.SessionID, reason); err != nil {
		return nil, err
	}
	return issueAuthBundle(session, "", true)
}

func RefreshLoginSession(rawRefreshToken, expectedSID, ip, userAgent string) (*AuthBundle, *User, error) {
	sid, secret, ok := splitRefreshToken(rawRefreshToken)
	if !ok {
		return nil, nil, ErrRefreshTokenInvalid
	}
	if expectedSID = strings.TrimSpace(expectedSID); expectedSID != "" && expectedSID != sid {
		return nil, nil, ErrLoginSessionMismatch
	}
	session, err := GetUserSessionCached(sid)
	if err != nil {
		if errors.Is(err, ErrUserSessionInactive) {
			return nil, nil, ErrLoginSessionRevoked
		}
		return nil, nil, ErrRefreshTokenInvalid
	}
	if session.Status != UserSessionStatusActive || session.RevokedAt != 0 || session.ExpiresAt <= time.Now().Unix() {
		return nil, nil, ErrLoginSessionRevoked
	}
	userCache, err := GetUserCache(session.UserID)
	if err != nil {
		return nil, nil, err
	}
	currentUser, err := GetUserById(session.UserID, false)
	if err != nil {
		return nil, nil, err
	}
	if userCache.Status != common.UserStatusEnabled || userCache.AuthVersion != session.UserAuthVersion ||
		currentUser.Status != common.UserStatusEnabled || currentUser.AuthVersion != session.UserAuthVersion {
		_, _ = RevokeUserSession(session.UserID, session.SID, "user_security_changed")
		return nil, nil, ErrLoginSessionRevoked
	}
	nextSecret := deriveNextRefreshSecret(sid, secret)
	rotated, err := RotateUserSessionRefresh(session.UserID, sid, hashRefreshSecret(secret), hashRefreshSecret(nextSecret), time.Now().Unix(), RefreshReplayWindow)
	if err != nil {
		if errors.Is(err, ErrUserSessionRefreshRace) && rotated != nil &&
			hashRefreshSecret(nextSecret) == rotated.RefreshHash {
			bundle, issueErr := issueAuthBundle(rotated, sid+"."+nextSecret, true)
			if issueErr != nil {
				return nil, nil, issueErr
			}
			return bundle, currentUser, nil
		}
		if errors.Is(err, ErrUserSessionRefreshReuse) {
			return nil, nil, ErrLoginSessionRevoked
		}
		if errors.Is(err, ErrUserSessionRefreshInvalid) {
			return nil, nil, ErrRefreshTokenInvalid
		}
		if errors.Is(err, ErrUserSessionRefreshRace) {
			return nil, nil, ErrRefreshRace
		}
		return nil, nil, err
	}
	rotated.IP = truncateAuthMetadata(ip, 64)
	rotated.UserAgent = truncateAuthMetadata(userAgent, 512)
	bundle, err := issueAuthBundle(rotated, sid+"."+nextSecret, true)
	if err != nil {
		return nil, nil, err
	}
	return bundle, currentUser, nil
}

func RevokeByRefreshToken(rawRefreshToken, expectedSID, reason string) error {
	sid, secret, ok := splitRefreshToken(rawRefreshToken)
	if !ok {
		return nil
	}
	if expectedSID = strings.TrimSpace(expectedSID); expectedSID != "" && expectedSID != sid {
		return ErrLoginSessionMismatch
	}
	_, err := RevokeUserSessionByRefreshHash(sid, hashRefreshSecret(secret), reason)
	return err
}

func RefreshTokenSID(rawRefreshToken string) (string, bool) {
	sid, _, ok := splitRefreshToken(rawRefreshToken)
	return sid, ok
}

func ListLoginSessions(userID int, currentSID string) ([]LoginSessionView, error) {
	sessions, err := ListActiveUserSessions(userID, currentSID, time.Now().Unix())
	if err != nil {
		return nil, err
	}
	views := make([]LoginSessionView, 0, len(sessions))
	for i := range sessions {
		views = append(views, sessionView(&sessions[i], sessions[i].SID == currentSID))
	}
	return views, nil
}

func WriteRefreshCookie(c contract.Context, rawToken string) {
	expiresAt := time.Now().Add(LoginSessionTTL)
	if sid, _, ok := splitRefreshToken(rawToken); ok {
		if session, err := GetUserSessionCached(sid); err == nil && session.ExpiresAt > time.Now().Unix() {
			expiresAt = time.Unix(session.ExpiresAt, 0)
		}
	}
	maxAge := int(time.Until(expiresAt) / time.Second)
	if maxAge < 1 {
		maxAge = 1
	}
	c.SetCookie(&http.Cookie{
		Name:     RefreshCookieName,
		Value:    rawToken,
		Path:     "/api/user/auth",
		MaxAge:   maxAge,
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func ClearRefreshCookie(c contract.Context) {
	c.SetCookie(&http.Cookie{
		Name:     RefreshCookieName,
		Value:    "",
		Path:     "/api/user/auth",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   common.SessionCookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func issueAuthBundle(session *UserSession, rawRefreshToken string, current bool) (*AuthBundle, error) {
	identity := AuthIdentity{
		UserID:          session.UserID,
		SessionID:       session.SID,
		UserAuthVersion: session.UserAuthVersion,
		SessionVersion:  session.Version,
	}
	accessToken, accessExpiresAt, err := IssueAccessToken(identity)
	if err != nil {
		return nil, err
	}
	return &AuthBundle{
		AccessToken:     accessToken,
		TokenType:       "Bearer",
		AccessExpiresAt: accessExpiresAt,
		Session:         sessionView(session, current),
		RefreshToken:    rawRefreshToken,
	}, nil
}

func sessionView(session *UserSession, current bool) LoginSessionView {
	return LoginSessionView{
		SID:          session.SID,
		Current:      current,
		LoginMethod:  session.LoginMethod,
		IP:           session.IP,
		UserAgent:    session.UserAgent,
		CreatedAt:    session.CreatedAt,
		LastActiveAt: session.LastActiveAt,
		ExpiresAt:    session.ExpiresAt,
	}
}

func splitRefreshToken(raw string) (string, string, bool) {
	sid, secret, ok := strings.Cut(strings.TrimSpace(raw), ".")
	if !ok || sid == "" || secret == "" || strings.Contains(secret, ".") {
		return "", "", false
	}
	if _, err := uuid.Parse(sid); err != nil {
		return "", "", false
	}
	return sid, secret, true
}

func hashRefreshSecret(secret string) string {
	return common.GenerateHMACWithKey(authSigningKey("refresh"), secret)
}

func deriveNextRefreshSecret(sid, currentSecret string) string {
	return common.GenerateHMACWithKey(authSigningKey("refresh-rotate"), sid+"."+currentSecret)
}

func truncateAuthMetadata(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func authSessionErrorCode(err error) (int, string) {
	switch {
	case errors.Is(err, ErrUserSessionLimit):
		return http.StatusConflict, "AUTH_SESSION_LIMIT"
	case errors.Is(err, ErrUserSessionIssuanceLimit):
		return http.StatusTooManyRequests, "AUTH_SESSION_ISSUANCE_LIMIT"
	case errors.Is(err, ErrLoginSessionMismatch):
		return http.StatusConflict, "AUTH_SESSION_MISMATCH"
	case errors.Is(err, ErrRefreshRace):
		return http.StatusConflict, "AUTH_REFRESH_RACE"
	case errors.Is(err, ErrAuthTokenExpired):
		return http.StatusUnauthorized, "AUTH_TOKEN_EXPIRED"
	case errors.Is(err, ErrLoginSessionRevoked):
		return http.StatusUnauthorized, "AUTH_SESSION_REVOKED"
	case errors.Is(err, ErrRefreshTokenInvalid), errors.Is(err, ErrAuthTokenInvalid):
		return http.StatusUnauthorized, "AUTH_UNAUTHORIZED"
	default:
		return http.StatusInternalServerError, "AUTH_INTERNAL_ERROR"
	}
}

func AuthSessionErrorCode(err error) (int, string) {
	return authSessionErrorCode(err)
}
