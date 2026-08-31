package usage

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/sensitive"
)

// The sensitive domain declares its two persistence hooks but cannot fill them
// (importing usage would close sensitive -> usage -> settings -> sensitive).
// If register_sensitive_hooks.go's init() is dropped, both hooks stay nil:
// every intercepted request is silently discarded and the retention sweep
// deletes nothing, so #409's audit table grows without bound.
func TestSensitiveAuditStoreHooksRegistered(t *testing.T) {
	if sensitive.RecordAuditLogFn == nil {
		t.Fatal("sensitive.RecordAuditLogFn is nil: audit writes are dropped silently")
	}
	if sensitive.DeleteOldAuditLogBatchFn == nil {
		t.Fatal("sensitive.DeleteOldAuditLogBatchFn is nil: SensitiveAuditRetentionDays never takes effect")
	}
}
