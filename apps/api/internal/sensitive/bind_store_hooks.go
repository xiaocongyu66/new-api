package sensitive

import "context"

// Persistence hooks for the sensitive audit log. They are wired by the usage
// capability (see internal/usage), which owns the log store; this package must
// not import the model layer directly because the model layer reaches
// internal/settings, which configures this package — importing it here would
// close a cycle (sensitive -> model -> settings -> sensitive).
//
// Callers may assume the hooks are set before the first audit write or sweep:
// both are triggered after application startup.
var (
	// RecordAuditLogFn persists one sensitive-audit row.
	RecordAuditLogFn func(userId int, content string, ip string, params map[string]interface{})
	// DeleteOldAuditLogBatchFn deletes up to limit rows older than
	// targetTimestamp and reports how many it removed.
	DeleteOldAuditLogBatchFn func(ctx context.Context, targetTimestamp int64, limit int) (int64, error)
)
