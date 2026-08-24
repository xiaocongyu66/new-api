package task

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/internal/gateway/port"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/model"
)
// ResolveOriginTask 处理基于已有任务的提交（remix / continuation）：
// 查找原始任务、从中提取模型名称、将渠道锁定到原始任务的渠道
// （通过 info.LockedChannel，重试时复用同一渠道并轮换 key），
// 以及提取 OtherRatios（时长、分辨率）。
// 该函数在控制器的重试循环之前调用一次，其结果通过 info 字段和上下文持久化。
func ResolveOriginTask(c contract.Context, info *port.SubmitInfo) *dto.TaskError {
	// 检测 remix action - from request path
	path := c.Path()
	if strings.Contains(path, "/v1/videos/") && strings.HasSuffix(path, "/remix") {
		info.Action = constant.TaskActionRemix
	}

	// 提取 remix 任务的 video_id
	if info.Action == constant.TaskActionRemix {
		videoID := c.Param("video_id")
		if strings.TrimSpace(videoID) == "" {
			return &dto.TaskError{
				Code:       "invalid_request",
				Message:    "video_id is required",
				StatusCode: http.StatusBadRequest,
			}
		}
		info.OriginTaskID = videoID
	}

	if info.OriginTaskID == "" {
		return nil
	}

	originTask, exist, err := GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil {
		return &dto.TaskError{
			Code:       "get_origin_task_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}
	if !exist {
		return &dto.TaskError{
			Code:       "task_not_exist",
			Message:    "task_origin_not_exist",
			StatusCode: http.StatusBadRequest,
		}
	}

	// 从原始任务推导模型名称
	if info.OriginModelName == "" {
		if originTask.Properties.OriginModelName != "" {
			info.OriginModelName = originTask.Properties.OriginModelName
		} else if originTask.Properties.UpstreamModelName != "" {
			info.OriginModelName = originTask.Properties.UpstreamModelName
		} else {
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			if m, ok := taskData["model"].(string); ok && m != "" {
				info.OriginModelName = m
			}
		}
	}

	// 锁定到原始任务的渠道（重试时复用同一渠道，轮换 key）
	ch, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		return &dto.TaskError{
			Code:       "channel_not_found",
			Message:    err.Error(),
			StatusCode: http.StatusBadRequest,
		}
	}
	if ch.Status != common.ChannelStatusEnabled {
		return &dto.TaskError{
			Code:       "task_channel_disable",
			Message:    "the channel of the origin task is disabled",
			StatusCode: http.StatusBadRequest,
		}
	}
	info.LockedChannel = ch

	if originTask.ChannelId != info.ChannelId {
		key, _, newAPIError := ch.GetNextEnabledKey()
		if newAPIError != nil {
			return &dto.TaskError{
				Code:       "channel_no_available_key",
				Message:    newAPIError.Error(),
				StatusCode: newAPIError.StatusCode,
			}
		}
		// Set context values for downstream use
		c.SetContextValue(constant.ContextKeyChannelKey, key)
		c.SetContextValue(constant.ContextKeyChannelType, ch.Type)
		c.SetContextValue(constant.ContextKeyChannelBaseUrl, ch.GetBaseURL())
		c.SetContextValue(constant.ContextKeyChannelId, originTask.ChannelId)

		info.ChannelBaseUrl = ch.GetBaseURL()
		info.ChannelId = originTask.ChannelId
		info.ChannelType = ch.Type
		info.ApiKey = key
	}

	// 提取 remix 参数（时长、分辨率 → OtherRatios）
	if info.Action == constant.TaskActionRemix {
		if info.PriceData == nil {
			info.PriceData = &port.PriceData{}
		}
		if originTask.PrivateData.BillingContext != nil {
			// 新的 remix 逻辑：直接从原始任务的 BillingContext 中提取 OtherRatios（如果存在）
			for s, f := range originTask.PrivateData.BillingContext.OtherRatios {
				info.PriceData.AddOtherRatio(s, f)
			}
		} else {
			// 旧的 remix 逻辑：直接从 task data 解析 seconds 和 size（如果存在）
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			secondsStr, _ := taskData["seconds"].(string)
			seconds, _ := strconv.Atoi(secondsStr)
			if seconds <= 0 {
				seconds = 4
			}
			// 历史任务数据可能包含未经校验的时长，作为计费乘数前必须钳制
			if seconds > 60 { // relaycommon.MaxTaskDurationSeconds
				seconds = 60
			}
			sizeStr, _ := taskData["size"].(string)
			info.PriceData.AddOtherRatio("seconds", float64(seconds))
			info.PriceData.AddOtherRatio("size", 1)
			if sizeStr == "1792x1024" || sizeStr == "1024x1792" {
				info.PriceData.AddOtherRatio("size", 1.666667)
			}
		}
	}

	return nil
}

// SubmitTask orchestrates the task submission flow:
// platform detection → validation → model mapping → price calc → billing →
// request build/send/response → billing adjustment.
func SubmitTask(c contract.Context, info port.SubmitInfo) (*port.TaskSubmitResult, *dto.TaskError) {
	// 1. 确定 platform → 创建适配器 → 验证请求
	platform := c.GetString("platform")
	if platform == "" {
		// Try to get from provider
		provider := port.GetTaskSubmitProviderFunc("")
		if provider != nil {
			p, a := provider.GetPlatform(c)
			if p != "" {
				platform = p
				info.Action = a
			}
		}
	}
	provider := port.GetTaskSubmitProviderFunc(platform)
	if provider == nil {
		return nil, &dto.TaskError{
			Code:       "invalid_api_platform",
			Message:    fmt.Sprintf("invalid api platform: %s", platform),
			StatusCode: http.StatusBadRequest,
		}
	}
	provider.Init(info.ChannelBaseUrl, info.ApiKey)

	if taskErr := provider.ValidateRequest(c, &info); taskErr != nil {
		return nil, taskErr
	}

	// 2. 确定模型名称
	modelName := info.OriginModelName
	if modelName == "" {
		// This is covered by ValidateRequest in the provider
		modelName = info.UpstreamModelName
	}

	// 2.5 应用渠道的模型映射
	info.OriginModelName = modelName
	info.UpstreamModelName = modelName
	if err := provider.ModelMappedHelper(c, &info); err != nil {
		return nil, &dto.TaskError{
			Code:       "model_mapping_failed",
			Message:    err.Error(),
			StatusCode: http.StatusBadRequest,
		}
	}

	// 3. 预生成公开 task ID（仅首次）
	if info.PublicTaskID == "" {
		info.PublicTaskID = model.GenerateTaskID()
	}

	// 4. 价格计算：基础模型价格
	info.OriginModelName = modelName
	priceData, err := provider.ModelPriceHelper(c, &info)
	if err != nil {
		return nil, &dto.TaskError{
			Code:       "model_price_error",
			Message:    err.Error(),
			StatusCode: http.StatusBadRequest,
		}
	}
	info.PriceData = priceData

	// 5. 计费估算：让适配器根据用户请求提供 OtherRatios（时长、分辨率等）
	//    必须在 ModelPriceHelper 之后调用（它会重建 PriceData）。
	//    ResolveOriginTask 可能已在 remix 路径中预设了 OtherRatios，此处合并。
	if estimatedRatios := provider.EstimateBilling(c, &info); len(estimatedRatios) > 0 {
		for k, v := range estimatedRatios {
			info.PriceData.AddOtherRatio(k, v)
		}
	}

	// 6. 将 OtherRatios 应用到基础额度（饱和转换，防止溢出成负数）
	if !common.StringsContains(constant.TaskPricePatches, modelName) {
		quotaWithRatios := info.PriceData.ApplyOtherRatiosToFloat(float64(info.PriceData.Quota))
		quota, clamp := common.QuotaFromFloatChecked(quotaWithRatios)
		info.PriceData.Quota = quota
		noteTaskQuotaClamp(&info, clamp)
	}

	// 7. 预扣费（仅首次 — 重试时 info.Billing 已存在，跳过）
	if info.Billing == nil && !info.PriceData.FreeModel {
		info.ForcePreConsume = true
		if taskErr := provider.PreConsumeBilling(c, &info, info.PriceData.Quota); taskErr != nil {
			return nil, taskErr
		}
	}

	// 8. 构建请求体
	requestBody, err := provider.BuildRequestBody(c, &info)
	if err != nil {
		return nil, &dto.TaskError{
			Code:       "build_request_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		}
	}

	// 9. 发送请求
	resp, err := provider.DoRequest(c, &info, requestBody)
	if err != nil {
		return nil, &dto.TaskError{
			Code:       "do_request_failed",
			Message:    err.Error(),
			StatusCode: http.StatusBadGateway,
		}
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		return nil, &dto.TaskError{
			Code:       "fail_to_fetch_task",
			Message:    string(responseBody),
			StatusCode: resp.StatusCode,
		}
	}

	// 10. 返回 OtherRatios 给下游（header 必须在 DoResponse 写 body 之前设置）
	otherRatios := info.PriceData.OtherRatios()
	if otherRatios == nil {
		otherRatios = map[string]float64{}
	}
	ratiosJSON, _ := common.Marshal(otherRatios)
	c.SetHeader("X-New-Api-Other-Ratios", string(ratiosJSON))

	// 11. 解析响应
	upstreamTaskID, taskData, taskErr := provider.DoResponse(c, resp, &info)
	if taskErr != nil {
		return nil, taskErr
	}

	// 12. 提交后计费调整：让适配器根据上游实际返回调整 OtherRatios
	finalQuota := info.PriceData.Quota
	if adjustedRatios := provider.AdjustBillingOnSubmit(&info, taskData); len(adjustedRatios) > 0 {
		if adjustedQuota, ok := recalcQuotaFromRatios(&info, adjustedRatios); ok {
			// 基于调整后的 ratios 重新计算 quota
			finalQuota = adjustedQuota
			info.PriceData.ReplaceOtherRatios(adjustedRatios)
			info.PriceData.Quota = finalQuota
		}
	}

	return &port.TaskSubmitResult{
		UpstreamTaskID: upstreamTaskID,
		TaskData:       taskData,
		Platform:       platform,
		Quota:          finalQuota,
	}, nil
}

// noteTaskQuotaClamp records quota clamp info on the submit info for logging.
func noteTaskQuotaClamp(info *port.SubmitInfo, clamp *common.QuotaClamp) {
	if clamp != nil && info.Billing != nil {
		// The billing session will carry this for logging
		// Actual logging happens in the controller via billing.SettleBilling
	}
}

// recalcQuotaFromRatios 根据 adjustedRatios 重新计算 quota。
// 公式: baseQuota × ∏(ratio) — 其中 baseQuota 是不含 OtherRatios 的基础额度。
func recalcQuotaFromRatios(info *port.SubmitInfo, adjustedRatios map[string]float64) (int, bool) {
	baseQuota := info.PriceData.Quota
	// Undo current OtherRatios to get base
	for _, v := range info.PriceData.OtherRatios() {
		if v != 0 {
			baseQuota = int(float64(baseQuota) / v)
		}
	}
	// Apply new ratios
	for _, v := range adjustedRatios {
		if v != 0 {
			baseQuota = int(float64(baseQuota) * v)
		}
	}
	if baseQuota <= 0 {
		return 0, false
	}
	return baseQuota, true
}