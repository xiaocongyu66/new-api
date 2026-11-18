package ops

import (
	"context"
	"strconv"
	"strings"
	"sync"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
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

// probeZoneState 记录每个单元上次的区间，用于方向性通知（只有穿越才通知）。
// 重启丢失可接受：会把当前区间重放一次。
var probeZoneState = struct {
	mu sync.Mutex
	m  map[probeUnitKey]probeZone
}{m: make(map[probeUnitKey]probeZone)}

// SendProbeWebhook 是可被测试注入的 webhook 发送入口，默认复用 ops.SendWebhookNotify。
var SendProbeWebhook = SendWebhookNotify

// probeAction 是一轮评估中待执行/待提交的单个单元动作（两阶段的中间产物）。
type probeAction struct {
	key          probeUnitKey
	channel      *channelpkg.Channel
	stat         ProbeModelStat
	availability float64
	zone         probeZone
	last         probeZone
	seen         bool
	disableBelow float64
	enableAbove  float64
	autoControl  bool
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

// EvaluateAndNotifyProbeThresholds 是每轮探测后的评估入口：
//  1. 按模型计算 24h 可用率；
//  2. 有策略且开启自动控制时，跌破 DisableBelow 自动禁用模型路由，
//     回升到 EnableAbove 及以上自动恢复（ability.auto_disabled 标记，可审计可恢复）；
//  3. 只有「穿越」区间才产生通知（方向性），webhook 未开启时仍执行自动控制，只是不发通知。
func EvaluateAndNotifyProbeThresholds(ctx context.Context) {
	s := GetProbeSetting()

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
	channelByID := make(map[int]*channelpkg.Channel, len(channels))
	for _, ch := range channels {
		channelByID[ch.Id] = ch
	}

	byChannel := make(map[int][]ChannelProbeResult)
	for _, row := range rows {
		byChannel[row.ChannelId] = append(byChannel[row.ChannelId], row)
	}

	// 两阶段：先在短临界区内读取/判定区间（纯内存），DB 动作在锁外执行，
	// 避免进程内互斥锁覆盖整个网关路由事务（MutateGatewayRouting）。

	var actions []probeAction
	var liveKeys = make(map[probeUnitKey]struct{}, len(byChannel))

	probeZoneState.mu.Lock()
	for channelID, channelRows := range byChannel {
		ch := channelByID[channelID]
		if ch == nil {
			continue
		}
		var policy *dto.ChannelProbePolicy
		if otherSettings := ch.GetOtherSettings(); otherSettings.ProbePolicy != nil {
			policy = otherSettings.ProbePolicy
			policy.Normalize()
		}
		for _, stat := range AggregateProbeModels(channelRows) {
			if stat.TotalCount == 0 || stat.Availability == nil {
				continue
			}
			availability := *stat.Availability
			key := probeUnitKey{ChannelID: channelID, Model: stat.Model}
			liveKeys[key] = struct{}{}

			disableBelow, enableAbove := s.WarningThreshold, s.WarningThreshold+probeNotifyRecoveryMargin
			autoControl := policy != nil && policy.AutoControlEnabled
			if policy != nil {
				disableBelow, enableAbove = policy.DisableBelow, policy.EnableAbove
			}

			zone := probeZoneBand
			switch {
			case availability >= enableAbove:
				zone = probeZoneAbove
			case availability < disableBelow:
				zone = probeZoneBelow
			}
			last, seen := probeZoneState.m[key]
			if seen && last == zone {
				continue // 未穿越，不动作不通知
			}
			actions = append(actions, probeAction{
				key: key, channel: ch, stat: stat, availability: availability,
				zone: zone, last: last, seen: seen,
				disableBelow: disableBelow, enableAbove: enableAbove,
				autoControl: autoControl,
			})
		}
	}
	// 清理已消失渠道/模型的僵尸条目，避免长期运行的内存累积。
	for key := range probeZoneState.m {
		if _, ok := liveKeys[key]; !ok {
			delete(probeZoneState.m, key)
		}
	}
	probeZoneState.mu.Unlock()

	// 第二阶段：锁外查 ability 状态并执行 DB 动作。
	var events []probeNotifyEvent
	abilityCache := make(map[int]map[string]channelpkg.ChannelModelAbilityState)
	for _, action := range actions {
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
		var event *probeNotifyEvent
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
				if action.seen {
					ev := probeEvent(action.channel, action.stat, action.availability,
						probeTitleForZone(action.zone),
						"低于 "+fmtThr(action.disableBelow)+"% 阈值，已自动禁用该模型路由")
					event = &ev
				}
			case action.autoControl:
				if action.seen {
					ev := probeEvent(action.channel, action.stat, action.availability,
						probeTitleForZone(action.zone),
						"低于 "+fmtThr(action.disableBelow)+"% 阈值（模型已被禁用，未重复操作）")
					event = &ev
				}
			default:
				if action.seen {
					ev := probeEvent(action.channel, action.stat, action.availability,
						probeTitleForZone(action.zone),
						"低于 "+fmtThr(action.disableBelow)+"% 阈值（自动禁用未开启，仅告警）")
					event = &ev
				}
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
				if action.seen {
					ev := probeEvent(action.channel, action.stat, action.availability,
						probeTitleForZone(action.zone),
						"回升至 "+fmtThr(action.enableAbove)+"% 阈值以上，已自动恢复该模型路由")
					event = &ev
				}
			case !state.Enabled && !state.AutoDisabled:
				if action.seen {
					ev := probeEvent(action.channel, action.stat, action.availability,
						probeTitleForZone(action.zone),
						"回升至 "+fmtThr(action.enableAbove)+"% 阈值以上（该模型为手动禁用，未自动恢复）")
					event = &ev
				}
			default:
				if action.seen {
					ev := probeEvent(action.channel, action.stat, action.availability,
						probeTitleForZone(action.zone),
						"回升至 "+fmtThr(action.enableAbove)+"% 阈值以上")
					event = &ev
				}
			}
		}

		// 动作成功才提交新区间；失败保留旧区间，下一轮重试。
		if committed {
			probeZoneState.mu.Lock()
			probeZoneState.m[action.key] = action.zone
			probeZoneState.mu.Unlock()
		}
		if event != nil {
			events = append(events, *event)
		}
	}

	if !s.WebhookEnabled || s.WebhookUrl == "" || len(events) == 0 {
		return
	}
	for _, ev := range events {
		if err := SendProbeWebhook(s.WebhookUrl, s.WebhookSecret,
			dto.NewNotify("channel_probe", ev.title, ev.content, nil)); err != nil {
			common.SysError("probe webhook notify failed: " + err.Error())
		}
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

// probeEvent 组装单模型通知：渠道名 + 编号、模型名、可用率、动作。
// title 由调用方按区间显式给出，不靠文案字符串反推。
func probeEvent(ch *channelpkg.Channel, stat ProbeModelStat, availability float64, title, action string) probeNotifyEvent {
	var b strings.Builder
	b.WriteString("渠道「" + ch.Name + "」（#" + strconv.Itoa(ch.Id) + "）模型 " + stat.Model + "\n")
	b.WriteString("24h 探测可用率 " + strconv.FormatFloat(availability, 'f', 1, 64) + "%（成功 " +
		strconv.FormatInt(stat.SuccessCount, 10) + "/" + strconv.FormatInt(stat.TotalCount, 10) + " 次）\n")
	b.WriteString("状态：" + action)
	if stat.LastErrorCode != "" {
		b.WriteString("，最近错误 " + stat.LastErrorCode)
	}
	return probeNotifyEvent{title: title, content: b.String()}
}
