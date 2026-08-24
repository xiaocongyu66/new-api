package integration

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/QuantumNous/new-api/internal/security/authtoken"
)

const (
	KarmadaDashboardSessionTTL = 5 * time.Minute
	karmadaDashboardTokenUse   = "karmada_dashboard"
	karmadaDashboardAudience   = "new-api-karmada-dashboard"
)

var ErrKarmadaDashboardSessionInvalid = errors.New("karmada dashboard session is invalid")

type karmadaDashboardClaims struct {
	TokenUse        string `json:"token_use"`
	SessionID       string `json:"sid"`
	UserAuthVersion int64  `json:"uv"`
	SessionVersion  int64  `json:"sv"`
	jwt.RegisteredClaims
}

func IssueKarmadaDashboardSession(ident AuthIdentity) (string, int64, error) {
	if ident.UserID <= 0 || ident.SessionID == "" || ident.UserAuthVersion <= 0 || ident.SessionVersion <= 0 {
		return "", 0, ErrKarmadaDashboardSessionInvalid
	}

	now := time.Now()
	expiresAt := now.Add(KarmadaDashboardSessionTTL)
	claims := karmadaDashboardClaims{
		TokenUse:        karmadaDashboardTokenUse,
		SessionID:       ident.SessionID,
		UserAuthVersion: ident.UserAuthVersion,
		SessionVersion:  ident.SessionVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    authtoken.TokenIssuer,
			Subject:   strconv.Itoa(ident.UserID),
			Audience:  jwt.ClaimStrings{karmadaDashboardAudience},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        uuid.NewString(),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(authtoken.SigningKey(karmadaDashboardTokenUse))
	if err != nil {
		return "", 0, err
	}
	return signed, expiresAt.Unix(), nil
}

func ValidateKarmadaDashboardSession(raw string) (AuthIdentity, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AuthIdentity{}, ErrKarmadaDashboardSessionInvalid
	}

	claims := &karmadaDashboardClaims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("%w: unexpected signing method", ErrKarmadaDashboardSessionInvalid)
		}
		return authtoken.SigningKey(karmadaDashboardTokenUse), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(authtoken.TokenIssuer), jwt.WithAudience(karmadaDashboardAudience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithLeeway(5*time.Second))
	if err != nil || !parsed.Valid || claims.TokenUse != karmadaDashboardTokenUse || claims.ID == "" {
		return AuthIdentity{}, ErrKarmadaDashboardSessionInvalid
	}

	userID, err := strconv.Atoi(claims.Subject)
	if err != nil || userID <= 0 || claims.SessionID == "" || claims.UserAuthVersion <= 0 || claims.SessionVersion <= 0 {
		return AuthIdentity{}, ErrKarmadaDashboardSessionInvalid
	}

	return AuthIdentity{
		UserID:          userID,
		SessionID:       claims.SessionID,
		UserAuthVersion: claims.UserAuthVersion,
		SessionVersion:  claims.SessionVersion,
	}, nil
}