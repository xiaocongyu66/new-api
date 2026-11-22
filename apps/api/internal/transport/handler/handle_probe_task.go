package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/ops"
	usagedomain "github.com/QuantumNous/new-api/internal/usage"
)

const probeErrorCodeMaxLen = 64

const (
	// probeOutcomeWindowSeconds bounds how far back a unit's own real traffic
	// still counts as its last known outcome. It matches the 7-day retention
	// of channel_probe_results: a model that failed this morning must show
	// that failure, not a sibling model's success from an hour ago.
	probeOutcomeWindowSeconds = 7 * 24 * 3600
	// probeAggregateWindowSeconds bounds the recent window the channel
	// aggregate is computed over. Only units with no own traffic in the
	// outcome window inherit it.
	probeAggregateWindowSeconds = 24 * 3600
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
type probeLogRow struct {
	ChannelId int
	ModelName string
	Type      int
	UseTime   int
	Content   string
	CreatedAt int64
}

type probeUnitKey struct {
	ChannelId int
	Model     string
}

type probeOutcome struct {
	Success   bool
	LatencyMs int64
	ErrorCode string
	// HasLatency is true only for consume-derived outcomes: an error row
	// never carries a usable duration, and averaging its zero would fake a
	// fast channel out of pure failures.
	HasLatency bool
	CreatedAt  int64
	// Source labels where the outcome came from: "request" (own traffic in
	// the local logs), "channel" (sibling aggregate), or "upstream" (the
	// upstream site's own perf API). Only "request" is a measurement of this
	// very unit, so only it may enter the auto-disable evidence table.
	Source string
	// Rate carries a graded 0-100 availability when the source provides one
	// (upstream perf APIs report a graded rate; log-derived outcomes are
	// binary 100/0).
	Rate float64
}

// probeErrorCode clips a free-form error message to the column width.
//
// The slice must be rune-aware: cutting a multi-byte character in half leaves
// invalid UTF-8, which PostgreSQL rejects outright (SQLSTATE 22021) and the
// whole probe row is lost. Non-UTF-8 bytes are dropped for the same reason.
func probeErrorCode(content string) string {
	s := strings.ToValidUTF8(content, "")
	if r := []rune(s); len(r) > probeErrorCodeMaxLen {
		s = string(r[:probeErrorCodeMaxLen])
	}
	return s
}

// latestProbeOutcomes collapses log rows (newest first) into one outcome per
// (channel, model). The newest row decides: a request that failed and was then
// retried successfully ends with a consume row, so the unit reads as usable —
// which is the question a probe is supposed to answer. EWMA averages are not
// used here on purpose: a long history of successes must not mask a model that
// has just started failing.
func latestProbeOutcomes(rows []probeLogRow) map[probeUnitKey]probeOutcome {
	out := make(map[probeUnitKey]probeOutcome, len(rows))
	for _, row := range rows {
		model := strings.TrimSpace(row.ModelName)
		if row.ChannelId <= 0 || model == "" {
			continue
		}
		key := probeUnitKey{ChannelId: row.ChannelId, Model: model}
		if _, ok := out[key]; ok {
			continue // rows arrive newest-first; the first one wins
		}
		if row.Type == usagedomain.LogTypeConsume {
			out[key] = probeOutcome{
				Success:    true,
				LatencyMs:  int64(row.UseTime) * 1000,
				HasLatency: true,
				CreatedAt:  row.CreatedAt,
			}
			continue
		}
		out[key] = probeOutcome{
			Success:   false,
			ErrorCode: probeErrorCode(row.Content),
			CreatedAt: row.CreatedAt,
		}
	}
	return out
}

// runProbeTask turns real request outcomes into probe snapshots.
//
// It sends nothing upstream. Every request already writes a consume log on
// success or an error log on failure, so the newest row per (channel, model)
// is direct evidence that the model does or does not run — with no extra
// tokens spent and no upstream charge. Only 24h of traffic is considered:
// older evidence no longer describes the model's current state.
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
	outcomeCutoff := now - probeOutcomeWindowSeconds
	var rows []probeLogRow
	if err := dbx.LogDB.Table("logs").
		Select("channel_id, model_name, type, use_time, content, created_at").
		Where("created_at >= ? AND channel_id > 0 AND model_name <> '' AND type IN ?",
			outcomeCutoff, []int{usagedomain.LogTypeConsume, usagedomain.LogTypeError}).
		Order("created_at DESC, id DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	outcomes := latestProbeOutcomes(rows)

	// 首选上游站自己的性能接口（new-api /api/perf-metrics/* 等）：这是上游
	// 真实流量统计出的成功率与延迟，零推理消耗、也不是本地日志的回声。
	// 上游没有该接口或取数失败时，退回日志推导。
	for _, channel := range channels {
		snap, err := ops.FetchUpstreamPerf(ctx, channel)
		if err != nil {
			common.SysLog(fmt.Sprintf("channel %d upstream perf fetch skipped: %v", channel.Id, err))
			continue
		}
		for modelName, entry := range snap.Models {
			outcomes[probeUnitKey{ChannelId: channel.Id, Model: modelName}] = probeOutcome{
				Success:    entry.SuccessRate >= 50,
				LatencyMs:  entry.AvgLatency,
				HasLatency: entry.AvgLatency > 0,
				CreatedAt:  now,
				Source:     "upstream",
				Rate:       entry.SuccessRate,
			}
		}
	}

	byChannel := make(map[int][]probeUnitKey, len(channels))
	for key := range outcomes {
		byChannel[key.ChannelId] = append(byChannel[key.ChannelId], key)
	}

	tested := 0
	snapshots := make([]ops.ModelAvailabilitySnapshot, 0, len(outcomes))
	for index, channel := range channels {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		report(index, total)

		// Channel aggregate over the units that real traffic measured this
		// window, restricted to models the channel still declares: removed
		// units must not skew the rate inherited by served models. Models on
		// this channel without their own traffic inherit it, so every model on
		// a serving channel stays charted.
		declared := make(map[string]struct{}, len(channel.GetModels()))
		for _, rawModel := range channel.GetModels() {
			if m := strings.TrimSpace(rawModel); m != "" {
				declared[m] = struct{}{}
			}
		}
		// The aggregate is a recent-window signal: a channel that served
		// traffic within the day is alive, regardless of how stale each
		// unit's own last outcome is.
		aggCutoff := now - probeAggregateWindowSeconds
		var aggRateSum float64
		aggN := 0
		for _, key := range byChannel[channel.Id] {
			if _, ok := declared[key.Model]; !ok {
				continue
			}
			o := outcomes[key]
			if o.CreatedAt < aggCutoff {
				continue
			}
			rate := 0.0
			if o.Success {
				rate = 100
			}
			aggRateSum += rate
			aggN++
		}
		var chRate float64
		if aggN > 0 {
			chRate = aggRateSum / float64(aggN)
		}

		seen := make(map[string]struct{}, len(channel.GetModels()))
		for _, rawModel := range channel.GetModels() {
			if ctx != nil && ctx.Err() != nil {
				break
			}
			model := strings.TrimSpace(rawModel)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}

			var success bool
			var latencyMs int64
			var errorCode string
			var rate float64
			source := "channel"
			outcome, ok := outcomes[probeUnitKey{ChannelId: channel.Id, Model: model}]
			if ok {
				success = outcome.Success
				latencyMs = outcome.LatencyMs
				errorCode = outcome.ErrorCode
				source = outcome.Source
				// Graded rate when the source provides one (upstream perf API);
				// log-derived outcomes are binary and fall back to 100/0.
				if outcome.Source == "upstream" {
					rate = outcome.Rate
				} else if success {
					rate = 100
				}
			} else if aggN > 0 {
				success = chRate >= 50
				// Keep the channel's aggregate rate, but NOT its latency: this
				// model never served a request, so a sibling's latency is not
				// its own measurement. Zero latency means "no evidence".
				latencyMs = 0
				rate = chRate
			} else {
				// The channel carried no traffic in the window at all: no
				// honest availability claim exists for any of its models.
				continue
			}

			// ChannelProbeResult is the evidence table the auto disable /
			// restore thresholds act on. A channel-aggregate outcome is an
			// estimate, not a measurement: writing it here would let sibling
			// models' results disable a model that never ran. Inherited units
			// are recorded in the snapshot table only.
			if source == "request" {
				// Stamp the row with the time the outcome was actually observed.
				// The outcome window is 7 days; writing "now" would make a
				// week-old failure look fresh to the 24h auto-disable evaluator.
				observedAt := now
				if o, ok := outcomes[probeUnitKey{ChannelId: channel.Id, Model: model}]; ok && o.CreatedAt > 0 {
					observedAt = o.CreatedAt
				}
				if err := dbx.DB.Create(&ops.ChannelProbeResult{
					ChannelId: channel.Id,
					Model:     model,
					Success:   success,
					LatencyMs: latencyMs,
					ErrorCode: errorCode,
					CreatedAt: observedAt,
				}).Error; err != nil {
					common.SysError("probe result persist failed: " + err.Error())
				}
			}
			snapshots = append(snapshots, ops.ModelAvailabilitySnapshot{
				Model:     model,
				ChannelId: channel.Id,
				Success:   success,
				Rate:      rate,
				LatencyMs: latencyMs,
				Source:    source,
				CreatedAt: now,
			})
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

	// Durable copy survives restarts; the transient table keeps only a week.
	ops.PersistModelAvailability(snapshots)
	if err := dbx.DB.Where("created_at < ?", now-7*24*3600).
		Delete(&ops.ChannelProbeResult{}).Error; err != nil {
		common.SysError("probe result cleanup failed: " + err.Error())
	}
	ops.EvaluateAndNotifyProbeThresholds(ctx)
	return result, nil
}
