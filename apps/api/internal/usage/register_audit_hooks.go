package usage

import (
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/transport/contract"
)

// This domain owns the Log table and resolves usernames from user records, so it
// depends on identity. Identity therefore cannot import it back; instead it
// exposes hooks that this init fills in.
func init() {
	identity.RegisterAuditHooks(
		func(userID int, content string) {
			RecordLog(userID, LogTypeSystem, content)
		},
		func(userID int, username, content, ip, action string, params, extra map[string]any) {
			RecordLoginLog(userID, username, content, ip, action, params, extra)
		},
		func(c contract.Context, targetUserID int, action string, params map[string]any) {
			RecordManageAuditFor(c, targetUserID, action, params)
		},
		func(c contract.Context, userID int, action string, params map[string]any) {
			RecordUserSecurityAudit(c, userID, action, params)
		},
	)

	// This domain owns the auto-ban mark, so it clears the mark when an admin
	// lifts a ban; otherwise the user is exempt from re-evaluation for the rest
	// of the process lifetime.
	identity.RegisterUserUnbanHook(ResetInsightAutoBanMark)
}
