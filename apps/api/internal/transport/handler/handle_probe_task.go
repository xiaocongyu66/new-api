package handler

import (
	"context"
	"strings"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/ops"
)

const probeErrorCodeMaxLen = 64

// probeHealthWindowSeconds bounds how old a health observation may be before it
// stops describing the model's current state.
const probeHealthWindowSeconds = 24 * 3600

type probeTaskPayload struct {
	ChannelIds []int `json:"channel_ids,omitempty"`
}

type probeTaskResult struct {
	Tested    int `json:"tested"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type probeHandler struct{}

func (probeHandler) Type() string { return ops.SystemTaskTypeChannelProbe }

func (probeHandler) Enabled() bool {
	return ops.GetProbeSetting().AutoProbeEnabled
}

func (probeHandler) Interval() time.Duration {
	interval := ops.GetProbeSetting().ProbeIntervalMinutes
	if interval <= 0 {
		interval = 30
	}
	return time.Duration(interval) * time.Minute
}

func (probeHandler) NewPayload() any { return &probeTaskPayload{} }

func (probeHandler) Run(ctx context.Context, task *ops.SystemTask, runnerID string) {
	payload := probeTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, ops.SystemTaskStatusFailed, nil, err)
		return
	}
	result, err := runProbeTask(ctx, payload.ChannelIds, task, runnerID)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, ops.SystemTaskStatusFailed, nil, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, ops.SystemTaskStatusSucceeded, result, nil)
}

// probeResultForHealth 把一条 request-derived 健康记录翻译成一次探测快照。
//
// success 取决于最近一次真实请求的结果，而不是 EWMA 均值：EWMA 会被历史
// 成功稀释，无法回答“这个模型现在还能不能跑”。没有成功也从未报错的行按
// 成功处理（暖机期不惩罚）。
func probeResultForHealth(h channelpkg.ChannelModelHealth) (bool, string) {
	if h.LastSuccessAt != nil && (h.LastErrorAt == nil || *h.LastSuccessAt >= *h.LastErrorAt) {
		return true, ""
	}
	if h.LastErrorAt != nil {
		code := h.LastErrorCode
		if len(code) > probeErrorCodeMaxLen {
			code = code[:probeErrorCodeMaxLen]
		}
		return false, code
	}
	return true, ""
}

// runProbeTask 用真实请求累积的健康状态生成探测快照。
//
// 它不向上游发送任何请求：每个 (channel, model) 的成败已经由真实流量在
// channel_model_health 里记录（handle_relay 在每次请求结束时调用
// RecordRequestAttempts 更新 EWMA、最近成功/失败时间与延迟）。因此这里
// 既不会消耗 prompt/completion token，也不会让上游多收一分钱，却能回答
// “这个模型最近到底跑通没有”——而不是仅仅“渠道密钥有效”。
//
// 只输出最近 24h 内有观测更新的单元：更早的记录已经不能代表模型当前状态。
func runProbeTask(ctx context.Context, channelIds []int, task *ops.SystemTask, runnerID string) (*probeTaskResult, error) {
	allChannels, err := channelpkg.GetAllChannels(0, 0, true, false)
	if err != nil {
		return nil, err
	}
	channels := make([]*channelpkg.Channel, 0, len(allChannels))
	for _, ch := range allChannels {
		if ch.Status != common.ChannelStatusEnabled {
			continue
		}
		channels = append(channels, ch)
	}
	if len(channelIds) > 0 {
		wanted := make(map[int]struct{}, len(channelIds))
		for _, id := range channelIds {
			wanted[id] = struct{}{}
		}
		filtered := make([]*channelpkg.Channel, 0, len(channels))
		for _, ch := range channels {
			if _, ok := wanted[ch.Id]; ok {
				filtered = append(filtered, ch)
			}
		}
		channels = filtered
	}

	report := ops.NewSystemTaskProgressReporter(task, runnerID)
	total := len(channels)
	result := &probeTaskResult{}

	// One query for every unit; the table is keyed by (channel, key_index, model)
	// and only carries rows that real traffic has observed.
	var health []channelpkg.ChannelModelHealth
	if err := dbx.DB.Find(&health).Error; err != nil {
		return nil, err
	}
	byChannel := make(map[int][]channelpkg.ChannelModelHealth, len(health))
	cutoffMs := (common.GetTimestamp() - probeHealthWindowSeconds) * 1000
	for _, row := range health {
		if row.RequestCount <= 0 || row.ScoreUpdatedAtMs < cutoffMs {
			continue
		}
		byChannel[row.ChannelId] = append(byChannel[row.ChannelId], row)
	}

	now := common.GetTimestamp()
	tested := 0
	for index, channel := range channels {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		report(index, total)

		for _, row := range byChannel[channel.Id] {
			if ctx != nil && ctx.Err() != nil {
				break
			}
			model := strings.TrimSpace(row.Model)
			if model == "" {
				continue
			}
			success, errorCode := probeResultForHealth(row)
			if err := dbx.DB.Create(&ops.ChannelProbeResult{
				ChannelId: channel.Id,
				Model:     model,
				Success:   success,
				LatencyMs: int64(row.LatencyEwmaMs),
				ErrorCode: errorCode,
				CreatedAt: now,
			}).Error; err != nil {
				common.SysError("probe result persist failed: " + err.Error())
			}
			tested++
			if success {
				result.Succeeded++
			} else {
				result.Failed++
			}
		}
	}
	if ctx == nil || ctx.Err() == nil {
		report(total, total)
	}
	result.Tested = tested

	cutoff := now - 7*24*3600
	if err := dbx.DB.Where("created_at < ?", cutoff).Delete(&ops.ChannelProbeResult{}).Error; err != nil {
		common.SysError("probe result cleanup failed: " + err.Error())
	}
	ops.EvaluateAndNotifyProbeThresholds(ctx)
	return result, nil
}
