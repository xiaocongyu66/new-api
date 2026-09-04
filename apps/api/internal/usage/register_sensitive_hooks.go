package usage

import "github.com/QuantumNous/new-api/internal/sensitive"

// This domain owns the Log table that holds the sensitive audit rows
// (type=LogTypeSensitive). The sensitive domain must not import it back:
// internal/settings imports sensitive, and this package imports settings, so a
// sensitive -> usage edge would close a cycle. It therefore declares the two
// persistence hooks and this init fills them in.
//
// Without this registration both hooks stay nil: audit_block.go drops every
// intercepted request silently and gc_audit.go returns before deleting
// anything, so SensitiveAuditRetentionDays never takes effect.
func init() {
	sensitive.RecordAuditLogFn = RecordSensitiveAuditLog
	sensitive.DeleteOldAuditLogBatchFn = DeleteOldSensitiveLogBatch
}
