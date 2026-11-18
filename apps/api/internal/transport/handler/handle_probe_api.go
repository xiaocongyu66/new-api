package handler

import (
	"context"
	"net/http"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/ops"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type probeOverviewChannel struct {
	ChannelID    int      `json:"channel_id"`
	Name         string   `json:"name"`
	Status       int      `json:"status"`
	Availability *float64 `json:"availability"`
	SuccessCount int64    `json:"success_count"`
	TotalCount   int64    `json:"total_count"`
	// Policy 是该渠道的探测策略；nil 表示未配置（不自动控制）。
	Policy *dto.ChannelProbePolicy `json:"policy"`
	Models []probeOverviewModel    `json:"models"`
}

type probeOverviewModel struct {
	Model         string   `json:"model"`
	SuccessCount  int64    `json:"success_count"`
	TotalCount    int64    `json:"total_count"`
	Availability  *float64 `json:"availability"`
	LastProbeAt   int64    `json:"last_probe_at"`
	LastErrorCode string   `json:"last_error_code"`
	// Enabled 表示该模型当前是否参与路由；AutoDisabled 表示是否被自动控制禁用。
	Enabled      bool `json:"enabled"`
	AutoDisabled bool `json:"auto_disabled"`
}

// GetProbeOverview 获取路由健康探测概览：设置快照 + 24h 内各启用渠道聚合。
func GetProbeOverview(c contract.Context) {
	setting := ops.GetProbeSetting()
	channels, err := channelpkg.GetAllChannels(0, 0, true, false)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	rows, err := ops.LoadAllProbeResults24h(context.Background())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	byChannel := make(map[int][]ops.ChannelProbeResult)
	for _, row := range rows {
		byChannel[row.ChannelId] = append(byChannel[row.ChannelId], row)
	}

	out := make([]probeOverviewChannel, 0, len(channels))
	for _, ch := range channels {
		if ch.Status != common.ChannelStatusEnabled {
			continue
		}
		// Models 恒为空数组而非 null：前端契约按数组消费。
		oc := probeOverviewChannel{ChannelID: ch.Id, Name: ch.Name, Status: ch.Status, Models: make([]probeOverviewModel, 0)}
		if otherSettings := ch.GetOtherSettings(); otherSettings.ProbePolicy != nil {
			policy := *otherSettings.ProbePolicy
			policy.Normalize()
			oc.Policy = &policy
		}
		rows := byChannel[ch.Id]
		if len(rows) > 0 {
			var success, total int64
			modelMap := make(map[string]*probeOverviewModel)
			order := make([]string, 0, len(rows))
			for _, row := range rows {
				total++
				if row.Success {
					success++
				}
				m, ok := modelMap[row.Model]
				if !ok {
					m = &probeOverviewModel{Model: row.Model}
					modelMap[row.Model] = m
					order = append(order, row.Model)
				}
				m.TotalCount++
				if row.Success {
					m.SuccessCount++
				}
				if row.CreatedAt > m.LastProbeAt {
					m.LastProbeAt = row.CreatedAt
					m.LastErrorCode = row.ErrorCode
				}
			}
			abilityStates, abilityErr := channelpkg.LoadChannelModelAbilityStates(ch.Id)
			if abilityErr != nil {
				common.SysError("probe ability states load failed: " + abilityErr.Error())
			}
			for _, model := range order {
				m := modelMap[model]
				availability := float64(m.SuccessCount) / float64(m.TotalCount) * 100
				m.Availability = &availability
				if state, ok := abilityStates[model]; ok {
					m.Enabled = state.Enabled
					m.AutoDisabled = state.AutoDisabled
				} else {
					// 没有 ability 行视为不可路由（模型不在该渠道的可用集合里）。
					m.Enabled = false
				}
				oc.Models = append(oc.Models, *m)
			}
			oc.SuccessCount = success
			oc.TotalCount = total
			availability := float64(success) / float64(total) * 100
			oc.Availability = &availability
		}
		out = append(out, oc)
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data": common.H{
			"settings": common.H{
				"auto_probe_enabled":        setting.AutoProbeEnabled,
				"probe_interval_minutes":    setting.ProbeIntervalMinutes,
				"warning_threshold":         setting.WarningThreshold,
				"critical_threshold":        setting.CriticalThreshold,
				"emergency_threshold":       setting.EmergencyThreshold,
				"webhook_enabled":           setting.WebhookEnabled,
				"webhook_url":               setting.WebhookUrl,
				"webhook_secret":            "",
				"webhook_secret_configured": setting.WebhookSecret != "",
			},
			"channels": out,
		},
	})
}

type probeSettingsUpdateRequest struct {
	AutoProbeEnabled     bool    `json:"auto_probe_enabled"`
	ProbeIntervalMinutes float64 `json:"probe_interval_minutes"`
	WarningThreshold     float64 `json:"warning_threshold"`
	CriticalThreshold    float64 `json:"critical_threshold"`
	EmergencyThreshold   float64 `json:"emergency_threshold"`
	WebhookEnabled       bool    `json:"webhook_enabled"`
	WebhookUrl           string  `json:"webhook_url"`
	WebhookSecret        string  `json:"webhook_secret"`
}

// UpdateProbeSettings 保存探测设置；webhook_secret 为空时保留旧值。
func UpdateProbeSettings(c contract.Context) {
	var req probeSettingsUpdateRequest
	rawBody, err := c.RawBody()
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if err := common.Unmarshal(rawBody, &req); err != nil {
		common.CtxApiError(c, err)
		return
	}
	setting := ops.ProbeSetting{
		AutoProbeEnabled:     req.AutoProbeEnabled,
		ProbeIntervalMinutes: req.ProbeIntervalMinutes,
		WarningThreshold:     req.WarningThreshold,
		CriticalThreshold:    req.CriticalThreshold,
		EmergencyThreshold:   req.EmergencyThreshold,
		WebhookEnabled:       req.WebhookEnabled,
		WebhookUrl:           req.WebhookUrl,
		WebhookSecret:        req.WebhookSecret,
	}
	if err := ops.ValidateProbeSettingValue(setting); err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{"success": false, "message": err.Error()})
		return
	}
	if setting.WebhookSecret == "" {
		setting.WebhookSecret = ops.GetProbeSetting().WebhookSecret
	}
	if err := ops.SaveProbeSetting(&setting); err != nil {
		common.CtxApiError(c, err)
		return
	}
	usage.RecordManageAudit(c, "probe_setting.update", map[string]interface{}{
		"keys": []string{
			"auto_probe_enabled", "probe_interval_minutes", "warning_threshold",
			"critical_threshold", "emergency_threshold", "webhook_enabled", "webhook_url",
		},
	})
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": ""})
}

// RunProbeNow 立即入队一次 channel_probe 探测任务；已有同类任务时返回 409。
func RunProbeNow(c contract.Context) {
	var req probeTaskPayload
	if rawBody, err := c.RawBody(); err == nil && len(rawBody) > 0 {
		if err := common.Unmarshal(rawBody, &req); err != nil {
			common.CtxApiError(c, err)
			return
		}
	}
	task, created, err := ops.EnqueueSystemTask(ops.SystemTaskTypeChannelProbe, req)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if !created {
		_ = c.JSON(http.StatusConflict, common.H{
			"success": false,
			"message": "已有探测任务正在运行或等待中",
			"data": common.H{
				"task_id": task.TaskID,
				"status":  task.Status,
			},
		})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data": common.H{
			"task_id": task.TaskID,
			"status":  task.Status,
		},
	})
}

// probePolicyUpdateRequest 是渠道探测策略的更新载荷。
type probePolicyUpdateRequest struct {
	ChannelID          int      `json:"channel_id"`
	AutoControlEnabled bool     `json:"auto_control_enabled"`
	DisableBelow       *float64 `json:"disable_below"`
	EnableAbove        *float64 `json:"enable_above"`
	// Clear 为 true 时删除策略（恢复为"不自动控制"）。
	Clear bool `json:"clear"`
}

// UpdateProbePolicy 保存单个渠道的探测策略。策略存在渠道 settings JSON 里，
// 因此与渠道编辑共用持久化与审计链路。
func UpdateProbePolicy(c contract.Context) {
	var req probePolicyUpdateRequest
	if err := c.BindJSON(&req); err != nil || req.ChannelID <= 0 {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	channel, err := channelpkg.GetChannelById(req.ChannelID, true)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}

	otherSettings := channel.GetOtherSettings()
	if req.Clear {
		otherSettings.ProbePolicy = nil
	} else {
		policy := &dto.ChannelProbePolicy{AutoControlEnabled: req.AutoControlEnabled}
		if req.DisableBelow != nil {
			policy.DisableBelow = *req.DisableBelow
		}
		if req.EnableAbove != nil {
			policy.EnableAbove = *req.EnableAbove
		}
		policy.Normalize()
		if err := policy.Validate(); err != nil {
			_ = c.JSON(http.StatusBadRequest, common.H{"success": false, "message": err.Error()})
			return
		}
		otherSettings.ProbePolicy = policy
	}

	encoded, err := common.Marshal(&otherSettings)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	channel.OtherSettings = string(encoded)
	if err := channel.Update(); err != nil {
		common.CtxApiError(c, err)
		return
	}
	usage.RecordManageAudit(c, "probe_policy.update", map[string]interface{}{
		"channel_id": req.ChannelID,
	})
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": ""})
}

// TestProbeWebhook 发送一条探测通知测试；body 可选覆盖 url/secret，空则用已保存值。
func TestProbeWebhook(c contract.Context) {
	var req struct {
		WebhookURL    string `json:"webhook_url"`
		WebhookSecret string `json:"webhook_secret"`
	}
	if rawBody, err := c.RawBody(); err == nil && len(rawBody) > 0 {
		if err := common.Unmarshal(rawBody, &req); err != nil {
			common.CtxApiError(c, err)
			return
		}
	}
	setting := ops.GetProbeSetting()
	url := req.WebhookURL
	if url == "" {
		url = setting.WebhookUrl
	}
	secret := req.WebhookSecret
	if secret == "" {
		secret = setting.WebhookSecret
	}
	if err := ops.SendProbeWebhook(url, secret, dto.NewNotify("channel_probe", "探测通知测试", "这是一条路由健康 webhook 测试通知", nil)); err != nil {
		// 发送失败必须是非 2xx，否则前端按状态码判断会误以为测试成功。
		_ = c.JSON(http.StatusBadGateway, common.H{"success": false, "message": err.Error()})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{"success": true, "message": ""})
}
