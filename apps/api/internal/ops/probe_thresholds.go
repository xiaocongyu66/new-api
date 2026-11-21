package ops

import (
	"context"
	"strconv"
	"strings"
	"sync"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// ProbeGrade 渠道 24h 探测可用率档位。
type ProbeGrade int

const (
	ProbeGradeUnknown   ProbeGrade = iota // 无数据，不通知
	ProbeGradeOK                          // >= Warning
	ProbeGradeWarning                     // >= Critical && < Warning
	ProbeGradeCritical                    // >= Emergency && < Critical
	ProbeGradeEmergency                   // < Emergency
)

// ClassifyProbeGrade 按三阈值把可用率（0-100）分级；阈值由 GetProbeSetting 归一化后传入。
func ClassifyProbeGrade(availability float64, s *ProbeSetting) ProbeGrade {
	switch {
	case availability >= s.WarningThreshold:
		return ProbeGradeOK
	case availability >= s.CriticalThreshold:
		return ProbeGradeWarning
	case availability >= s.EmergencyThreshold:
		return ProbeGradeCritical
	default:
		return ProbeGradeEmergency
	}
}

// probeZone 是单（渠道, 模型）在某轮评估后的区间：迟滞带内不动作不通知。
type probeZone int

const (
	probeZoneUnknown probeZone = iota
	probeZoneAbove             // >= enableAbove：可用
	probeZoneBand              // [disableBelow, enableAbove)：迟滞带，保持现状
	probeZoneBelow             // < disableBelow：异常
)

// probeUnitKey 标识一个探测单元（渠道 + 模型）。
type probeUnitKey struct {
	ChannelID int
	Model     string
}

// probeControlZoneState is driven only by raw 24h probe rates and gates
// automatic disable/restore. Keeping it separate from the webhook zone stops
// a blended crossing from changing automatic control. Losing the map on
// restart only replays one control transition.
var probeControlZoneState = struct {
	mu sync.Mutex
	m  map[probeUnitKey]probeZone
}{m: make(map[probeUnitKey]probeZone)}

// probeNotifyZoneState tracks the zone computed from the blended probe and
// upstream rate; it only deduplicates webhook notifications.
var probeNotifyZoneState = struct {
	mu sync.Mutex
	m  map[probeUnitKey]probeZone
}{m: make(map[probeUnitKey]probeZone)}

// SendProbeWebhook 是可被测试注入的 webhook 发送入口，默认复用 ops.SendWebhookNotify。
var SendProbeWebhook = SendWebhookNotify

// loadProbeWebhookHealth supplies the optional request-derived signal used for
// webhook decisions; tests inject deterministic points without health storage.
var loadProbeWebhookHealth = LoadUpstreamHealth

// probeAction 是一轮评估中待执行/待提交的单个单元动作（两阶段的中间产物）。
type probeAction struct {
	key          probeUnitKey
	stat         ProbeModelStat
	zone         probeZone
	disableBelow float64
	enableAbove  float64
	autoControl  bool
}

// probeNotifyAction is one pending webhook decision, judged on the blended
// probe plus upstream rate rather than the raw probe count rate.
type probeNotifyAction struct {
	key          probeUnitKey
	channel      *channelpkg.Channel
	probeStat    ProbeModelStat
	blendedRate  float64
	source       string
	zone         probeZone
	disableBelow float64
	enableAbove  float64
}

// probeNotifyEvent 是一次待发送的通知。
type probeNotifyEvent struct {
	title   string
	content string
}

// autoDisableModel 与 autoRestoreModel 是可注入的自动控制入口（测试替换）。
var (
	autoDisableModel = channelpkg.AutoDisableChannelModel
	autoRestoreModel = channelpkg.RestoreAutoDisabledChannelModel
)

// EvaluateAndNotifyProbeThresholds evaluates one probe round:
//  1. raw 24h probe counts classify automatic disable/restore for channels
//     whose policy enables auto control; the webhook switch never gates this;
//  2. when a webhook is configured, a trailing-24h observed upstream rate is
//     loaded once and averaged with the probe rate for notification decisions;
//  3. control and notification keep separate per-unit zones, so only their
//     own crossings trigger an action or a message.
func EvaluateAndNotifyProbeThresholds(ctx context.Context) {
	s := GetProbeSetting()
	webhookConfigured := s.WebhookEnabled && s.WebhookUrl != ""

	rows, err := LoadAllProbeResults24h(ctx)
	if err != nil {
		common.SysError("probe threshold stats load failed: " + err.Error())
		return
	}
	channels, err := channelpkg.GetAllChannels(0, 0, true, false)
	if err != nil {
		common.SysError("probe threshold channel list failed: " + err.Error())
		return
	}

	type channelRule struct {
		disableBelow float64
		enableAbove  float64
		autoControl  bool
	}
	rules := make(map[int]channelRule, len(channels))
	channelByID := make(map[int]*channelpkg.Channel, len(channels))
	for _, ch := range channels {
		channelByID[ch.Id] = ch
		rule := channelRule{
			disableBelow: s.WarningThreshold,
			enableAbove:  s.WarningThreshold + probeNotifyRecoveryMargin,
		}
		if otherSettings := ch.GetOtherSettings(); otherSettings.ProbePolicy != nil {
			policy := otherSettings.ProbePolicy
			policy.Normalize()
			rule.disableBelow, rule.enableAbove = policy.DisableBelow, policy.EnableAbove
			rule.autoControl = policy.AutoControlEnabled
		}
		rules[ch.Id] = rule
	}

	// The upstream load is optional for this evaluation: it happens once,
	// before any zone lock, and only when a webhook can actually be sent.
	var upstreamPoints []usage.UpstreamHealthPoint
	if webhookConfigured {
		endTs := common.GetTimestamp()
		var loadErr error
		upstreamPoints, loadErr = loadProbeWebhookHealth(ctx, endTs-probeResultWindowSeconds, endTs, nil)
		if loadErr != nil {
			common.SysError("probe webhook upstream health load failed: " + loadErr.Error())
			upstreamPoints = nil
		}
	}
	upstreamByUnit := make(map[probeUnitKey][]usage.UpstreamHealthPoint)
	upstreamCount := make(map[probeUnitKey]int)
	for _, point := range upstreamPoints {
		if point.ChannelID <= 0 || point.Model == "" {
			continue
		}
		key := probeUnitKey{ChannelID: point.ChannelID, Model: point.Model}
		upstreamByUnit[key] = append(upstreamByUnit[key], point)
		upstreamCount[key]++
	}

	byChannel := make(map[int][]ChannelProbeResult)
	for _, row := range rows {
		byChannel[row.ChannelId] = append(byChannel[row.ChannelId], row)
	}

	// Both zone maps are read and written in one short critical section; the
	// database work below stays outside the lock.
	var controlActions []probeAction
	var notifyActions []probeNotifyAction
	liveControlKeys := make(map[probeUnitKey]struct{})
	liveNotifyKeys := make(map[probeUnitKey]struct{})
	notifyHandled := make(map[probeUnitKey]struct{})

	probeControlZoneState.mu.Lock()
	probeNotifyZoneState.mu.Lock()
	for channelID, channelRows := range byChannel {
		ch := channelByID[channelID]
		if ch == nil {
			continue
		}
		rule := rules[channelID]
		for _, stat := range AggregateProbeModels(channelRows) {
			if stat.TotalCount == 0 || stat.Availability == nil {
				continue
			}
			key := probeUnitKey{ChannelID: channelID, Model: stat.Model}
			probeRate := *stat.Availability

			if rule.autoControl {
				liveControlKeys[key] = struct{}{}
				zone := probeZoneForRate(probeRate, rule.disableBelow, rule.enableAbove)
				if last, seen := probeControlZoneState.m[key]; !seen || last != zone {
					controlActions = append(controlActions, probeAction{
						key: key, stat: stat, zone: zone,
						disableBelow: rule.disableBelow, enableAbove: rule.enableAbove,
						autoControl: true,
					})
				}
			}

			if !webhookConfigured || ch.Status != common.ChannelStatusEnabled {
				continue
			}
			var upstreamRate *float64
			if upstreamCount[key] > 0 {
				rate := usage.UpstreamRateWeighted(upstreamByUnit[key])
				upstreamRate = &rate
			}
			blended := usage.BlendRates(&probeRate, upstreamRate)
			source := "probe"
			if upstreamRate != nil {
				source = "probe+upstream"
			}
			notifyHandled[key] = struct{}{}
			liveNotifyKeys[key] = struct{}{}
			zone := probeZoneForRate(*blended, rule.disableBelow, rule.enableAbove)
			if last, seen := probeNotifyZoneState.m[key]; !seen || last != zone {
				notifyActions = append(notifyActions, probeNotifyAction{
					key: key, channel: ch, probeStat: stat,
					blendedRate: *blended, source: source, zone: zone,
					disableBelow: rule.disableBelow, enableAbove: rule.enableAbove,
				})
			}
		}
	}

	// A unit with upstream evidence but no probe rows is still a valid signal.
	if webhookConfigured {
		for key := range upstreamCount {
			if _, handled := notifyHandled[key]; handled {
				continue
			}
			ch := channelByID[key.ChannelID]
			if ch == nil || ch.Status != common.ChannelStatusEnabled {
				continue
			}
			rate := usage.UpstreamRateWeighted(upstreamByUnit[key])
			blended := usage.BlendRates(&rate)
			rule := rules[key.ChannelID]
			liveNotifyKeys[key] = struct{}{}
			zone := probeZoneForRate(*blended, rule.disableBelow, rule.enableAbove)
			if last, seen := probeNotifyZoneState.m[key]; !seen || last != zone {
				notifyActions = append(notifyActions, probeNotifyAction{
					key: key, channel: ch, probeStat: ProbeModelStat{Model: key.Model},
					blendedRate: *blended, source: "upstream", zone: zone,
					disableBelow: rule.disableBelow, enableAbove: rule.enableAbove,
				})
			}
		}
	}

	for key := range probeControlZoneState.m {
		if _, ok := liveControlKeys[key]; !ok {
			delete(probeControlZoneState.m, key)
		}
	}
	if webhookConfigured {
		for key := range probeNotifyZoneState.m {
			if _, ok := liveNotifyKeys[key]; !ok {
				delete(probeNotifyZoneState.m, key)
			}
		}
	}
	probeNotifyZoneState.mu.Unlock()
	probeControlZoneState.mu.Unlock()

	// Control actions are driven by the raw probe zone; webhook messages are
	// judged on the blended notify zone. Keep both so a webhook only reuses
	// control wording when the two zones actually agree.
	type controlContext struct {
		zone probeZone
		text string
	}
	controlContextByKey := make(map[probeUnitKey]controlContext)
	abilityCache := make(map[int]map[string]channelpkg.ChannelModelAbilityState)
	for _, action := range controlActions {
		states, ok := abilityCache[action.key.ChannelID]
		if !ok {
			loaded, abilityErr := channelpkg.LoadChannelModelAbilityStates(action.key.ChannelID)
			if abilityErr != nil {
				common.SysError("probe ability states load failed: " + abilityErr.Error())
			}
			states = loaded
			abilityCache[action.key.ChannelID] = states
		}
		state := states[action.stat.Model]

		committed := true
		var statusText string
		switch action.zone {
		case probeZoneBelow:
			switch {
			case action.autoControl && state.Enabled:
				if err := autoDisableModel(action.key.ChannelID, action.stat.Model); err != nil {
					common.SysError("probe auto disable failed: " + err.Error())
					committed = false
					break
				}
				// 动作已改库：同步缓存，后续同渠道模型判定看到最新状态。
				state.Enabled = false
				state.AutoDisabled = true
				states[action.stat.Model] = state
				statusText = "低于 " + fmtThr(action.disableBelow) + "% 阈值，已自动禁用该模型路由"
			case action.autoControl:
				statusText = "低于 " + fmtThr(action.disableBelow) + "% 阈值（模型已被禁用，未重复操作）"
			default:
				statusText = "低于 " + fmtThr(action.disableBelow) + "% 阈值（自动禁用未开启，仅告警）"
			}
		case probeZoneAbove:
			switch {
			case action.autoControl && state.AutoDisabled:
				if err := autoRestoreModel(action.key.ChannelID, action.stat.Model); err != nil {
					common.SysError("probe auto restore failed: " + err.Error())
					committed = false
					break
				}
				state.Enabled = true
				state.AutoDisabled = false
				states[action.stat.Model] = state
				statusText = "回升至 " + fmtThr(action.enableAbove) + "% 阈值以上，已自动恢复该模型路由"
			case !state.Enabled && !state.AutoDisabled:
				statusText = "回升至 " + fmtThr(action.enableAbove) + "% 阈值以上（该模型为手动禁用，未自动恢复）"
			default:
				statusText = "回升至 " + fmtThr(action.enableAbove) + "% 阈值以上"
			}
		}

		// 动作成功才提交新区间；失败保留旧区间，下一轮重试。
		if committed {
			probeControlZoneState.mu.Lock()
			probeControlZoneState.m[action.key] = action.zone
			probeControlZoneState.mu.Unlock()
		}
		if statusText != "" {
			controlContextByKey[action.key] = controlContext{zone: action.zone, text: statusText}
		}
	}

	var events []probeNotifyEvent
	for _, action := range notifyActions {
		probeNotifyZoneState.mu.Lock()
		probeNotifyZoneState.m[action.key] = action.zone
		probeNotifyZoneState.mu.Unlock()
		// 迟滞带内不通知，但提交区间，避免后续同带值重复判定。
		if action.zone == probeZoneBand {
			continue
		}
		statusText := ""
		if ctx, ok := controlContextByKey[action.key]; ok && ctx.zone == action.zone {
			statusText = ctx.text
		}
		if statusText == "" {
			// No matching control transition this round; never claim one.
			if action.zone == probeZoneAbove {
				statusText = "回升至 " + fmtThr(action.enableAbove) + "% 阈值以上（通知判定）"
			} else {
				statusText = "低于 " + fmtThr(action.disableBelow) + "% 阈值（通知判定）"
			}
		}
		events = append(events, probeEvent(action.channel, action.probeStat,
			action.blendedRate, action.source, probeTitleForZone(action.zone), statusText))
	}

	if !webhookConfigured || len(events) == 0 {
		return
	}
	for _, ev := range events {
		if err := SendProbeWebhook(s.WebhookUrl, s.WebhookSecret,
			dto.NewNotify("channel_probe", ev.title, ev.content, nil)); err != nil {
			common.SysError("probe webhook notify failed: " + err.Error())
		}
	}
}

func probeZoneForRate(rate, disableBelow, enableAbove float64) probeZone {
	switch {
	case rate >= enableAbove:
		return probeZoneAbove
	case rate < disableBelow:
		return probeZoneBelow
	default:
		return probeZoneBand
	}
}

// probeNotifyRecoveryMargin 是无渠道策略时通知恢复带的宽度（百分点），
// 避免可用率贴着全局告警阈值来回抖动。
const probeNotifyRecoveryMargin = 5.0

func fmtThr(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// probeTitleForZone 按区间给出通知标题：异常→告警，恢复→恢复。
func probeTitleForZone(zone probeZone) string {
	if zone == probeZoneAbove {
		return "路由健康探测恢复"
	}
	return "路由健康探测告警"
}

// probeEvent assembles one model notification. The blended percentage is
// labelled separately from the raw probe counts so they are not mistaken for
// the same measurement.
func probeEvent(ch *channelpkg.Channel, stat ProbeModelStat, blendedRate float64, source, title, action string) probeNotifyEvent {
	var b strings.Builder
	b.WriteString("渠道「" + ch.Name + "」（#" + strconv.Itoa(ch.Id) + "）模型 " + stat.Model + "\n")
	b.WriteString("通知判定可用率 " + strconv.FormatFloat(blendedRate, 'f', 1, 64) + "%")
	counts := strconv.FormatInt(stat.SuccessCount, 10) + "/" + strconv.FormatInt(stat.TotalCount, 10) + " 次成功"
	switch source {
	case "probe":
		b.WriteString("（24h 探测 " + counts + "）")
	case "probe+upstream":
		b.WriteString("（24h 探测与上游请求观测均值；24h 探测 " + counts + "）")
	default:
		b.WriteString("（上游请求观测均值；无 24h 探测样本）")
	}
	b.WriteString("\n状态：" + action)
	if stat.LastErrorCode != "" {
		b.WriteString("，最近错误 " + stat.LastErrorCode)
	}
	return probeNotifyEvent{title: title, content: b.String()}
}
