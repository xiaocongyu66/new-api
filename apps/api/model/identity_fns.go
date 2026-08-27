package model

import (
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"gorm.io/gorm"
)

// Identity-domain function wrappers.
//
// The implementations moved to internal/identity. Files still in model/
// (checkin.go, log.go, subscription.go, topup.go, etc.) call these functions.
// To avoid a model → identity import cycle (identity imports security/oauth
// which imports model), these are function variables wired at startup.
//
// Until wired, calls return zero values. main.go wires them via
// model.SetIdentityFunctions() during init, before serving requests.

type UserQuotaRow = identity.UserQuotaRow

var (
	UserQueryFn         func(tx *gorm.DB) *gorm.DB
	TokenQueryFn        func(tx *gorm.DB) *gorm.DB
	LockUserRowFn       func(tx *gorm.DB, userID int) (UserQuotaRow, error)
	ReadUserQuotaFn     func(tx *gorm.DB, userID int) (UserQuotaRow, error)
	GetUsernameByIdFn   func(id int, fromDB bool) (string, error)
	GetUserSettingFn    func(id int, fromDB bool) (dto.UserSetting, error)
	IncreaseUserQuotaFn func(id int, quota int, db bool) error
	DecreaseUserQuotaFn func(id int, quota int, db bool) error
	RootUserExistsFn    func() bool
)

// UserQuery delegates to the identity implementation.
func UserQuery(tx *gorm.DB) *gorm.DB {
	if UserQueryFn != nil {
		return UserQueryFn(tx)
	}
	return tx
}

// TokenQuery delegates to the identity implementation.
func TokenQuery(tx *gorm.DB) *gorm.DB {
	if TokenQueryFn != nil {
		return TokenQueryFn(tx)
	}
	return tx
}

// LockUserRow delegates to the identity implementation.
func LockUserRow(tx *gorm.DB, userID int) (UserQuotaRow, error) {
	if LockUserRowFn != nil {
		return LockUserRowFn(tx, userID)
	}
	return UserQuotaRow{}, nil
}

// ReadUserQuota delegates to the identity implementation.
func ReadUserQuota(tx *gorm.DB, userID int) (UserQuotaRow, error) {
	if ReadUserQuotaFn != nil {
		return ReadUserQuotaFn(tx, userID)
	}
	return UserQuotaRow{}, nil
}

// GetUsernameById delegates to the identity implementation.
func GetUsernameById(id int, fromDB bool) (string, error) {
	if GetUsernameByIdFn != nil {
		return GetUsernameByIdFn(id, fromDB)
	}
	return "", nil
}

// GetUserSetting delegates to the identity implementation.
func GetUserSetting(id int, fromDB bool) (dto.UserSetting, error) {
	if GetUserSettingFn != nil {
		return GetUserSettingFn(id, fromDB)
	}
	return dto.UserSetting{}, nil
}

// IncreaseUserQuota delegates to the identity implementation.
func IncreaseUserQuota(id int, quota int, db bool) error {
	if IncreaseUserQuotaFn != nil {
		return IncreaseUserQuotaFn(id, quota, db)
	}
	return nil
}

// DecreaseUserQuota delegates to the identity implementation.
func DecreaseUserQuota(id int, quota int, db bool) error {
	if DecreaseUserQuotaFn != nil {
		return DecreaseUserQuotaFn(id, quota, db)
	}
	return nil
}

// RootUserExists delegates to the identity implementation.
func RootUserExists() bool {
	if RootUserExistsFn != nil {
		return RootUserExistsFn()
	}
	return false
}

// SetIdentityFunctions wires the identity-domain function variables.
// Called from main.go during startup.
func SetIdentityFunctions(
	uq func(*gorm.DB) *gorm.DB,
	tq func(*gorm.DB) *gorm.DB,
	lur func(*gorm.DB, int) (UserQuotaRow, error),
	rur func(*gorm.DB, int) (UserQuotaRow, error),
	gub func(int, bool) (string, error),
	gus func(int, bool) (dto.UserSetting, error),
	iuq func(int, int, bool) error,
	duq func(int, int, bool) error,
	rue func() bool,
) {
	UserQueryFn = uq
	TokenQueryFn = tq
	LockUserRowFn = lur
	ReadUserQuotaFn = rur
	GetUsernameByIdFn = gub
	GetUserSettingFn = gus
	IncreaseUserQuotaFn = iuq
	DecreaseUserQuotaFn = duq
	RootUserExistsFn = rue
}

// TokenNameRow is a token projection for callers that only need the name.
type TokenNameRow struct {
	Id   int
	Name string
}

func (TokenNameRow) TableName() string {
	return "tokens"
}

var (
	GetTokenByIdFn            func(id int) (*Token, error)
	GetTokenByKeyFn           func(key string, fromDB bool) (*Token, error)
	GetUserCacheFn            func(userId int) (*UserBase, error)
	DecreaseTokenQuotaFn      func(id int, key string, quota int) error
	RefreshUserGroupCacheFn   func(userId int) error
	MigrateTokenModelLimitsFn func() error
)

func GetTokenById(id int) (*Token, error) {
	if GetTokenByIdFn != nil {
		return GetTokenByIdFn(id)
	}
	var row Token
	err := dbx.DB.Select("id", "name", "key").Where("id = ?", id).First(&row).Error
	return &row, err
}

func GetTokenByKey(key string, fromDB bool) (*Token, error) {
	if GetTokenByKeyFn != nil {
		return GetTokenByKeyFn(key, fromDB)
	}
	return nil, nil
}

func GetUserCache(userId int) (*UserBase, error) {
	if GetUserCacheFn != nil {
		return GetUserCacheFn(userId)
	}
	return nil, nil
}

func DecreaseTokenQuota(id int, key string, quota int) error {
	if DecreaseTokenQuotaFn != nil {
		return DecreaseTokenQuotaFn(id, key, quota)
	}
	return nil
}

func RefreshUserGroupCache(userId int) error {
	if RefreshUserGroupCacheFn != nil {
		return RefreshUserGroupCacheFn(userId)
	}
	return nil
}

func migrateTokenModelLimitsToText() error {
	if MigrateTokenModelLimitsFn != nil {
		return MigrateTokenModelLimitsFn()
	}
	return nil
}

// Type re-exports for callers still importing model (security/oauth, etc.).
// These are aliases — model.User and identity.User are the same type.


type User = identity.User
type Token = identity.Token
type UserSession = identity.UserSession
type TwoFA = identity.TwoFA
type TwoFABackupCode = identity.TwoFABackupCode
type UserBase = identity.UserBase
type PasskeyCredential = identity.PasskeyCredential
type AuthFlow = identity.AuthFlow
type AuthFlowCreate = identity.AuthFlowCreate
type AuthFlowMatch = identity.AuthFlowMatch
type CustomOAuthProvider = identity.CustomOAuthProvider
type UserOAuthBinding = identity.UserOAuthBinding
type ExternalIdentityClaim = identity.ExternalIdentityClaim
type UserSortOptions = identity.UserSortOptions

// Function re-exports for callers still importing model (security/oauth).
var (
	IsDiscordIdAlreadyTaken    = identity.IsDiscordIdAlreadyTaken
	IsGitHubIdAlreadyTaken     = identity.IsGitHubIdAlreadyTaken
	IsLinuxDOIdAlreadyTaken    = identity.IsLinuxDOIdAlreadyTaken
	IsOidcIdAlreadyTaken       = identity.IsOidcIdAlreadyTaken
	IsProviderUserIdTaken      = identity.IsProviderUserIdTaken
	GetUserByOAuthBinding      = identity.GetUserByOAuthBinding
	GetAllCustomOAuthProviders = identity.GetAllCustomOAuthProviders
)

// Token quota functions re-exported.
var IncreaseTokenQuota = identity.IncreaseTokenQuota

var (
	GetTokenByKeyWrFn   = identity.GetTokenByKey
	GetUserCacheWrFn    = identity.GetUserCache
)
var UpdateUserSetting = identity.UpdateUserSetting
var ValidateAccessToken = identity.ValidateAccessToken

var (
	ValidateUserToken               = identity.ValidateUserToken
	IsAdmin                         = identity.IsAdmin
	UpdateUserUsedQuota             = identity.UpdateUserUsedQuota
	UpdateUserUsedQuotaAndRequestCount = identity.UpdateUserUsedQuotaAndRequestCount
	GetUserById                     = identity.GetUserById
	GetUserGroup                    = identity.GetUserGroup
	GetUserQuota                    = identity.GetUserQuota
	NormalizeEmail                  = identity.NormalizeEmail
	SearchUsers                     = identity.SearchUsers
	GetUsernameByIdRe               = identity.GetUsernameById
	GetUserSettingRe                = identity.GetUserSetting
)
var GetRootUser = identity.GetRootUser
var IsEmailAlreadyTaken = identity.IsEmailAlreadyTaken
var GetTwoFAByUserId = identity.GetTwoFAByUserId
var GetUserLanguage = identity.GetUserLanguage

// Re-exports for test compatibility
var (
	UserSessionStatusActive   = identity.UserSessionStatusActive
	UserSessionStatusRevoked  = identity.UserSessionStatusRevoked
	CreateUserSession          = identity.CreateUserSession
	GetUserSessionCached       = identity.GetUserSessionCached
	RevokeUserSession          = identity.RevokeUserSession
	RevokeAllUserSessions      = identity.RevokeAllUserSessions
	GetUserSessionBySID        = identity.GetUserSessionBySID
	CountActiveUserSessions    = identity.CountActiveUserSessions
	AdvanceUserSessionAuthVersion = identity.AdvanceUserSessionAuthVersion
	IsTwoFAEnabled             = identity.IsTwoFAEnabled
	GetPasskeyByUserID         = identity.GetPasskeyByUserID
	ValidateBackupCode         = identity.ValidateBackupCode
	DisableTwoFAWithAuthVersion = identity.DisableTwoFAWithAuthVersion
	CreateAuthFlow             = identity.CreateAuthFlow
	GetAuthFlow                = identity.GetAuthFlow
	ConsumeAuthFlow            = identity.ConsumeAuthFlow
)
var AuthFlowPurposeOAuth = identity.AuthFlowPurposeOAuth
var AuthFlowPurposePasskeyLogin = identity.AuthFlowPurposePasskeyLogin
