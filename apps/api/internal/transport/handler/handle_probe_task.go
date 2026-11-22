package handler

import (
	"context"
	"fmt"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/ops"
)

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

// probeLogRow is the slice of a consume/error log row the probe needs.
// runProbeTask 调用上游站的性能接口，把上游统计的逐小时模型表现写入快照表。
//
// 它不向上游发任何推理请求，也不读本地日志：本地日志是探活自己的回声——
// 同一个陈旧值会被每轮重复写入，图表因此永远是一条平线。数据只能来自
// 上游自己的 /api/perf-metrics/*（或其等价接口）。
//
// 上游取数失败（无该接口 / 鉴权失败 / 不可解析）时该渠道本轮跳过，写不出
// 数据就是没有数据——不猜测、不继承、不造值。
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
	now := common.GetTimestamp()

	tested := 0
	snapshots := make([]ops.ModelAvailabilitySnapshot, 0, len(channels)*8)
	for index, channel := range channels {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		report(index, total)

		snap, err := ops.FetchUpstreamPerf(ctx, channel)
		if err != nil {
			common.SysLog(fmt.Sprintf("channel %d upstream perf unavailable, skipped: %v", channel.Id, err))
			continue
		}
		for alias, points := range snap.Models {
			for _, point := range points {
				bucketTs := point.Ts
				if bucketTs <= 0 {
					// 上游只给了窗口汇总值、没给小时桶：落成当前小时，
					// 但仍是一小时一个点，不会被填满 24 小时。
					bucketTs = now - now%3600
				}
				var ttft *int64
				if point.TtftMs > 0 {
					v := point.TtftMs
					ttft = &v
				}
				snapshots = append(snapshots, ops.ModelAvailabilitySnapshot{
					Model:     alias,
					ChannelId: channel.Id,
					BucketTs:  bucketTs,
					Rate:      point.Rate,
					LatencyMs: point.LatencyMs,
					TtftMs:    ttft,
					Source:    "upstream",
					CreatedAt: now,
				})
				tested++
				if point.Rate >= 50 {
					result.Succeeded++
				} else {
					result.Failed++
				}
			}
		}
	}
	if ctx == nil || ctx.Err() == nil {
		report(total, total)
	}
	result.Tested = tested

	// 快照按 (model, channel, hour) upsert：重复探活刷新同一小时，不产生重复行。
	ops.PersistModelAvailability(snapshots)
	ops.EvaluateAndNotifyProbeThresholds(ctx)
	return result, nil
}
