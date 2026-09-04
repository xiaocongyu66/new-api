// Package service provides HTTP handlers and business logic.
package task

import (
	"context"

	relaycommon "github.com/QuantumNous/new-api/internal/relay/common"
	"github.com/QuantumNous/new-api/internal/types"
)

// LogTaskConsumption 记录任务消费日志和统计信息（仅记录，不涉及实际扣费）。
// 实际扣费已由 BillingSession（PreConsumeBilling + SettleBilling）完成。

// RefundTaskQuota 统一的任务失败退款逻辑。
// 当异步任务失败时，退还资金与令牌额度，并回减用户和渠道用量。
// 返回资金来源是否已成功退还；失败时保留 quota，供显式重试或人工对账。

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (Quota) 做差额结算。
// reason 用于日志记录（例如 "token重算" 或 "adaptor调整"）。
// clamps 可选：若计算 actualQuota 时发生额度饱和，将其记入日志 admin_info（仅管理员可见）。

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
// 当任务成功且返回了 totalTokens 时，根据模型倍率和分组倍率重新计算实际扣费额度，
// 与预扣费的差额进行补扣或退还。支持钱包和订阅计费来源。

// settleTaskBillingOnComplete 任务完成时的统一计费调整（转发至 task 包）。
func settleTaskBillingOnComplete(ctx context.Context, adaptor TaskBillingAdaptor, taskModel *Task, taskResult *relaycommon.TaskInfo) {
	SettleTaskBillingOnComplete(ctx, adaptor, taskModel, taskResult)
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段（转发至 task 包）。
func taskBillingOther(taskModel *Task) map[string]interface{} {
	return TaskBillingOther(taskModel)
}

// taskBillingContextPriceData 从 BillingContext 构建 PriceData（转发至 task 包）。
func taskBillingContextPriceData(bc *TaskBillingContext) *types.PriceData {
	return TaskBillingContextPriceData(bc)
}
