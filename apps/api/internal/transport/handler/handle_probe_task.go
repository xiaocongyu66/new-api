package handler

import (
	"context"
	"strconv"
	"strings"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/ops"
)

const probeErrorCodeMaxLen = 64

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

// runProbeTask 零消耗探测每个启用渠道（payload 限定时仅限给定 id）。
//
// 探测只请求上游模型列表（GET /v1/models），不发送任何对话消息，因此不会
// 产生 prompt/completion token，也不写消费日志；可用性与耗时落到
// ChannelProbeResult 供「上游情况」与模型广场图表聚合，随后清理 7 天前旧行
// 并评估阈值通知。
func runProbeTask(ctx context.Context, channelIds []int, task *ops.SystemTask, runnerID string) (*probeTaskResult, error) {
	allChannels, err := channelpkg.GetAllChannels(0, 0, true, false)
	if err != nil {
		return nil, err
	}
	channels := make([]*channelpkg.Channel, 0, len(allChannels))
	skipped := 0
	for _, ch := range allChannels {
		if ch.Status != common.ChannelStatusEnabled {
			continue
		}
		// 异步/任务型渠道没有同步探测入口：跳过而不是记为失败，
		// 否则会拉低可用率并可能触发误自动禁用。
		if isChannelTypeTestUnsupported(ch.Type) {
			skipped++
			continue
		}
		channels = append(channels, ch)
	}
	if skipped > 0 {
		common.SysLog("channel probe: skipped unsupported channel types = " + strconv.Itoa(skipped))
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
	tested := 0

	for index, channel := range channels {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		report(index, total)

		// Zero-cost probe: only GET the upstream model list. No chat request is
		// sent, so no prompt/completion tokens are consumed and nothing is
		// billed. One request covers every model declared by the channel, so a
		// channel with 20 models still costs a single metadata call.
		tik := time.Now()
		_, probeErr := fetchChannelUpstreamModelIDs(channel)
		ms := time.Since(tik).Milliseconds()
		success := probeErr == nil
		errorCode := ""
		if probeErr != nil {
			errorCode = probeErr.Error()
			if len(errorCode) > probeErrorCodeMaxLen {
				errorCode = errorCode[:probeErrorCodeMaxLen]
			}
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

			if err := dbx.DB.Create(&ops.ChannelProbeResult{
				ChannelId: channel.Id,
				Model:     model,
				Success:   success,
				LatencyMs: ms,
				ErrorCode: errorCode,
				CreatedAt: common.GetTimestamp(),
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
		if common.RequestInterval > 0 {
			if ctx == nil {
				time.Sleep(common.RequestInterval)
			} else {
				select {
				case <-ctx.Done():
					goto done
				case <-time.After(common.RequestInterval):
				}
			}
		}
	}
done:
	if ctx == nil || ctx.Err() == nil {
		report(total, total)
	}
	result.Tested = tested

	cutoff := common.GetTimestamp() - 7*24*3600
	if err := dbx.DB.Where("created_at < ?", cutoff).Delete(&ops.ChannelProbeResult{}).Error; err != nil {
		common.SysError("probe result cleanup failed: " + err.Error())
	}
	ops.EvaluateAndNotifyProbeThresholds(ctx)
	return result, nil
}
