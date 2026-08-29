package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/billing/settlecore"
	"github.com/QuantumNous/new-api/internal/constant"
	"github.com/QuantumNous/new-api/internal/logger"
	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/model"
)

func PrepareMidjourneyTaskBilling(relayInfo *relaycommon.RelayInfo, task *model.Midjourney, quota int, shouldBill bool) (bool, error) {
	if task == nil {
		return false, errors.New("Midjourney task is nil")
	}
	task.Quota = 0
	task.TokenId = 0
	task.BillingChannelId = 0
	if !shouldBill {
		return false, nil
	}
	if relayInfo == nil {
		return false, errors.New("relay info is nil")
	}
	if quota < 0 {
		return false, errors.New("quota cannot be negative")
	}
	if relayInfo.BillingSource == settlecore.BillingSourceSubscription {
		return false, errors.New("legacy Midjourney billing does not support subscriptions")
	}

	task.Quota = quota
	task.BillingChannelId = task.ChannelId
	if relayInfo.ChannelMeta != nil && relayInfo.ChannelId > 0 {
		task.BillingChannelId = relayInfo.ChannelId
	}
	return true, nil
}

// SettleMidjourneyTaskBilling charges a persisted legacy task and records the applied stages.
func SettleMidjourneyTaskBilling(relayInfo *relaycommon.RelayInfo, task *model.Midjourney, prepared bool) (bool, error) {
	if !prepared {
		return false, nil
	}
	if relayInfo == nil {
		return false, errors.New("relay info is nil")
	}
	if task == nil || task.Id == 0 {
		return false, errors.New("Midjourney task must be persisted before billing")
	}

	result, billingErr := postConsumeQuotaWithResult(relayInfo, task.Quota, 0, true)
	if !result.FundingApplied {
		task.Quota = 0
		task.TokenId = 0
		task.BillingChannelId = 0
		if updateErr := task.UpdateBillingState(); updateErr != nil {
			return false, errors.Join(billingErr, fmt.Errorf("clear Midjourney billing state: %w", updateErr))
		}
		return false, billingErr
	}

	task.TokenId = 0
	if result.TokenApplied {
		task.TokenId = relayInfo.TokenId
	}
	if updateErr := task.UpdateBillingState(); updateErr != nil {
		return true, errors.Join(billingErr, fmt.Errorf("update Midjourney billing state: %w", updateErr))
	}
	return true, billingErr
}

func RefundMidjourneyQuota(ctx context.Context, task *model.Midjourney, reason string) bool {
	quota := task.Quota
	if quota == 0 {
		return true
	}

	if err := model.IncreaseUserQuota(task.UserId, quota, false); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("退还 Midjourney 用户额度失败 task %s: %s", task.MjId, err.Error()))
		return false
	}

	if task.TokenId > 0 {
		tokenKey := settlecore.ResolveTokenKey(ctx, task.TokenId, task.MjId)
		if tokenKey != "" {
			if err := model.IncreaseTokenQuota(task.TokenId, tokenKey, quota); err != nil {
				logger.LogWarn(ctx, fmt.Sprintf("退还 Midjourney 令牌额度失败 task %s: %s", task.MjId, err.Error()))
			}
		}
	}

	billingChannelId := task.GetBillingChannelId()
	model.UpdateUserUsedQuota(task.UserId, -quota)
	model.UpdateChannelUsedQuota(billingChannelId, -quota)
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   model.LogTypeRefund,
		Content:   "",
		ChannelId: billingChannelId,
		ModelName: covertMjpActionToModelName(task.Action),
		Quota:     quota,
		TokenId:   task.TokenId,
		Other: map[string]interface{}{
			"task_id": task.MjId,
			"reason":  reason,
		},
	})

	task.Quota = 0
	if err := task.UpdateBillingState(); err != nil {
		logger.LogError(ctx, fmt.Sprintf("Midjourney 退款成功但清除 quota 失败 task %s: %s", task.MjId, err.Error()))
	}
	return true
}

// covertMjpActionToModelName maps a midjourney action to its model name (local copy to avoid import cycle).
func covertMjpActionToModelName(mjAction string) string {
	modelName := "mj_" + strings.ToLower(mjAction)
	if mjAction == constant.MjActionSwapFace {
		modelName = "swap_face"
	}
	return modelName
}
