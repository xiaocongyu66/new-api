package billing

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage"
)

// QuotaCompensationRequest credits a user's wallet for an amount an admin
// decided to give back. Unlike the plain add_quota action it also writes a
// top-up row owned by the user, so the credit shows up in the user's own log
// list instead of only in the operator's audit trail.
type QuotaCompensationRequest struct {
	UserId    int    `json:"user_id"`
	Quota     int    `json:"quota"`
	Reason    string `json:"reason"`
	RequestId string `json:"request_id"`
}

// QuotaCompensationResult reports what the call did. Applied is false when the
// request id was already recorded, which makes retries safe.
type QuotaCompensationResult struct {
	UserId    int    `json:"user_id"`
	Quota     int    `json:"quota"`
	RequestId string `json:"request_id"`
	Applied   bool   `json:"applied"`
}

// quotaCompensationRequestIdMaxLen matches logs.request_id.
const quotaCompensationRequestIdMaxLen = 64

// quotaCompensationMu serializes the check-then-credit window: two concurrent
// retries carrying the same request id would otherwise both pass the lookup.
var quotaCompensationMu sync.Mutex

// ApplyQuotaCompensation credits quota and records the user-visible top-up row.
// requestId is the idempotency key; an empty id always applies.
func ApplyQuotaCompensation(userId int, quota int, reason string, requestId string) (*QuotaCompensationResult, error) {
	if userId <= 0 {
		return nil, errors.New("user_id 无效")
	}
	if quota <= 0 {
		return nil, errors.New("quota 必须大于 0")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, errors.New("reason 不能为空")
	}
	requestId = strings.TrimSpace(requestId)
	if len(requestId) > quotaCompensationRequestIdMaxLen {
		return nil, fmt.Errorf("request_id 长度不能超过 %d", quotaCompensationRequestIdMaxLen)
	}

	quotaCompensationMu.Lock()
	defer quotaCompensationMu.Unlock()

	result := &QuotaCompensationResult{UserId: userId, Quota: quota, RequestId: requestId}
	if requestId != "" {
		recorded, err := usage.LogRequestIdExists(usage.LogTypeTopup, requestId)
		if err != nil {
			return nil, err
		}
		if recorded {
			return result, nil
		}
	}
	if err := identity.IncreaseUserQuota(userId, quota, true); err != nil {
		return nil, err
	}
	content := fmt.Sprintf("额度补偿：返还 %s，原因：%s", logger.LogQuota(quota), reason)
	if err := usage.RecordLogWithRequestId(userId, usage.LogTypeTopup, requestId, content); err != nil {
		return nil, err
	}
	result.Applied = true
	return result, nil
}

// AdminCompensateQuota is the admin entry point: POST
// /api/subscription/admin/quota-compensate.
func AdminCompensateQuota(c contract.Context) {
	var req QuotaCompensationRequest
	if err := c.BindJSON(&req); err != nil {
		common.CtxApiErrorMsg(c, "参数错误")
		return
	}
	result, err := ApplyQuotaCompensation(req.UserId, req.Quota, req.Reason, req.RequestId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	usage.RecordManageAuditFor(c, req.UserId, "user.quota_compensate", map[string]interface{}{
		"quota":      logger.LogQuota(req.Quota),
		"reason":     strings.TrimSpace(req.Reason),
		"request_id": strings.TrimSpace(req.RequestId),
		"applied":    result.Applied,
	})
	common.CtxApiSuccess(c, result)
}
