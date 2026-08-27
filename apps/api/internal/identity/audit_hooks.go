package identity

import "github.com/QuantumNous/new-api/internal/transport/contract"

// Audit and activity-log writes are injected rather than imported.
//
// The usage domain owns the Log table and reads user records to resolve
// usernames, so it depends on this domain. Calling usage directly from here would
// close that loop. Registration keeps the dependency one-way: usage installs
// these during startup, and this domain calls them through the indirection.
//
// Unset hooks are no-ops so a partially wired process (a unit test that never
// starts the usage domain) records nothing instead of panicking.
type (
	// SystemLogRecorder writes a system-activity entry for a user.
	SystemLogRecorder func(userID int, content string)
	// LoginLogRecorder writes an authentication-event entry.
	LoginLogRecorder func(userID int, username, content, ip, action string, params, extra map[string]any)
	// ManageAuditRecorder writes an administrative audit entry attributed to the
	// operator, with targetUserID naming the user acted upon.
	ManageAuditRecorder func(c contract.Context, targetUserID int, action string, params map[string]any)
	// UserSecurityAuditRecorder writes a security-sensitive entry for the user's
	// own action, which has no administrative operator.
	UserSecurityAuditRecorder func(c contract.Context, userID int, action string, params map[string]any)
)

var (
	recordSystemLog         SystemLogRecorder
	recordLoginLog          LoginLogRecorder
	recordManageAudit       ManageAuditRecorder
	recordUserSecurityAudit UserSecurityAuditRecorder
)

// RegisterAuditHooks installs the log and audit writers. Called once during
// startup by the usage domain.
func RegisterAuditHooks(
	systemLog SystemLogRecorder,
	loginLog LoginLogRecorder,
	manageAudit ManageAuditRecorder,
	userSecurityAudit UserSecurityAuditRecorder,
) {
	recordSystemLog = systemLog
	recordLoginLog = loginLog
	recordManageAudit = manageAudit
	recordUserSecurityAudit = userSecurityAudit
}

func writeSystemLog(userID int, content string) {
	if recordSystemLog != nil {
		recordSystemLog(userID, content)
	}
}

func writeLoginLog(userID int, username, content, ip, action string, params, extra map[string]any) {
	if recordLoginLog != nil {
		recordLoginLog(userID, username, content, ip, action, params, extra)
	}
}

func writeManageAudit(c contract.Context, targetUserID int, action string, params map[string]any) {
	if recordManageAudit != nil {
		recordManageAudit(c, targetUserID, action, params)
	}
}

func writeUserSecurityAudit(c contract.Context, userID int, action string, params map[string]any) {
	if recordUserSecurityAudit != nil {
		recordUserSecurityAudit(c, userID, action, params)
	}
}

// GroupModelsResolver returns the model names enabled for the given user groups.
//
// Catalog data lives outside this domain and its lookup reads channel records, so
// it is injected for the same reason the audit writers are.
type GroupModelsResolver func(groups []string) []string

var resolveGroupModels GroupModelsResolver

// RegisterGroupModelsResolver installs the catalog lookup.
func RegisterGroupModelsResolver(resolver GroupModelsResolver) {
	resolveGroupModels = resolver
}

func groupModels(groups []string) []string {
	if resolveGroupModels == nil {
		return []string{}
	}
	return resolveGroupModels(groups)
}

// RedemptionKeyRedeemer redeems a top-up key for a user.
type RedemptionKeyRedeemer func(key string, userID int) (quota int, err error)

var redeemKey RedemptionKeyRedeemer

func RegisterRedemptionRedeemer(redeemer RedemptionKeyRedeemer) {
	redeemKey = redeemer
}

// CustomOAuthRegistrar manages custom OAuth provider registration.
type CustomOAuthRegistrar interface {
	IsProviderRegistered(slug string) bool
	IsCustomProvider(slug string) bool
	RegisterOrUpdateCustomProvider(config *CustomOAuthProvider)
	UnregisterCustomProvider(slug string)
}

var oauthRegistrar CustomOAuthRegistrar

func RegisterCustomOAuthRegistrar(registrar CustomOAuthRegistrar) {
	oauthRegistrar = registrar
}
