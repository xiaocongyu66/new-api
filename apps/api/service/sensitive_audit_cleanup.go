package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/internal/sensitive"
)

const sensitiveAuditCleanupInterval = time.Hour

// StartSensitiveAuditCleanup 周期清理超过保留期的敏感审计日志。
// 仅主节点执行；保留天数 <=0 视为永久保留，直接跳过。
func StartSensitiveAuditCleanup() {
	if !common.IsMasterNode {
		return
	}
	go func() {
		runSensitiveAuditCleanup()
		ticker := time.NewTicker(sensitiveAuditCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			runSensitiveAuditCleanup()
		}
	}()
}

func runSensitiveAuditCleanup() {
	days := sensitive.SensitiveAuditRetentionDays
	if days <= 0 {
		return
	}
	ctx := context.Background()
	target := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	for {
		deleted, err := model.DeleteOldSensitiveLogBatch(ctx, target, 500)
		if err != nil {
			common.SysError("failed to cleanup sensitive audit logs: " + err.Error())
			return
		}
		if deleted == 0 {
			return
		}
	}
}
