package billing

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
)

// 撤回策略（recall_policies）：后台配置「内容类型 → 秒数」的 JSON 映射，
// 形如 {"drop_award":30,"checkin_fail":10}。0 或缺失 = 该类型不撤回；
// 配置存在即完全接管：缺席的类型不撤回，也不再按文案回落到 failure_notice。
// 配置为空串或 {} 时走旧行为回落：bind_fail 用 DefaultBindFailRecallSeconds，
// 其余失败提示受旧开关 recall_failed_messages / recall_delay_seconds 控制。
//
// 命中撤回策略的消息必须走主动消息通道（撤回接口只支持平台下发 message_id
// 的主动消息，带 msg_id/event_id 的被动回复无法撤回），会占用主动消息配额，
// 建议只对低频提示开启。
//
// 12 种可撤回内容类型，各自固定的回复点：
//
//	RecallKindDropAward      drop_award      群聊掉落奖励文案（qqbot_drop.go HandleGroupChatForDrop）
//	RecallKindCheckinSuccess checkin_success 签到成功文案（doCheckinForOpenID 成功分支）
//	RecallKindCheckinFail    checkin_fail    签到失败/未开启文案（doCheckinForOpenID 失败分支）
//	RecallKindTransfer       transfer        /转账 结果与 /转账费率 提示（HandleGroupAtMessage）
//	RecallKindBalance        balance         /余额 查询提示（HandleGroupAtMessage）
//	RecallKindMenu           menu            /菜单 与按钮回调的菜单回复（HandleMenuCommand / HandleInteraction）
//	RecallKindRedPacket      red_packet      发红包、抢红包、红包战绩回复（HandleGroupAtMessage / HandleInteraction）
//	RecallKindBindSuccess    bind_success    绑定成功提示（HandleGroupAtMessage）
//	RecallKindBindFail       bind_fail       绑定失败提示（HandleGroupAtMessage）
//	RecallKindDropCommand    drop_command    /开启掉落 等掉落开关指令回复（HandleGroupAtMessage）
//	RecallKindStealSuccess   steal_success   偷奶酪成功文案（HandleStealCommand 回复，qqbot_service.go）
//	RecallKindFailureNotice  failure_notice  isFailureReply 按内容识别的失败提示（仅未配置 policies 时兜底）
const (
	RecallKindDropAward      = "drop_award"
	RecallKindCheckinSuccess = "checkin_success"
	RecallKindCheckinFail    = "checkin_fail"
	RecallKindTransfer       = "transfer"
	RecallKindBalance        = "balance"
	RecallKindMenu           = "menu"
	RecallKindRedPacket      = "red_packet"
	RecallKindBindSuccess    = "bind_success"
	RecallKindBindFail       = "bind_fail"
	RecallKindDropCommand    = "drop_command"
	RecallKindStealSuccess   = "steal_success"
	RecallKindFailureNotice  = "failure_notice"
)

// DefaultBindFailRecallSeconds 未配置 recall_policies 时「绑定失败」提示的默认撤回延迟。
//
// 这条提示是用户唯一能看到绑定失败原因的地方（验证码错误 / 已过期 / 该 QQ 已被占用），
// 10 秒在手机上往往读不完就被撤回，用户只能反复重试，所以单独给 bind_fail 更长的保留时间。
// recall_policies 显式配置了 bind_fail 时（含 0）完全按配置走，本默认值不生效。
const DefaultBindFailRecallSeconds = 30

// recallDelayFor 返回某条回复发出后自动撤回的秒数，0 表示不撤回。
//
// content 用于旧行为的失败提示识别（isFailureReply），仅在 recall_policies
// 未配置时参与判断；配置存在即完全接管，不再按文案兜底。
//
// 优先级：
//  1. recall_policies 解析出非空映射 → 完全接管：policies[kind]>0 用该值，
//     否则一律 0（含显式 0、负数、缺键），缺席的类型不会被 failure_notice 救回。
//  2. 配置非空但解析失败 → 一律 0，宁可不撤，不可误撤。
//  3. 配置为空串或 {} → 旧行为回落，全部受 recall_failed_messages 总开关约束：
//     bind_fail 用 DefaultBindFailRecallSeconds（固定 30 秒，不受 recall_delay_seconds 影响），
//     failure_notice 与文案命中 isFailureReply 的回复走 recall_delay_seconds。
func recallDelayFor(kind, content string) int {
	if raw := strings.TrimSpace(GetQQBotSetting().RecallPolicies); raw != "" {
		var policies map[string]int
		if err := common.UnmarshalJsonStr(raw, &policies); err != nil {
			common.SysError("recall_policies 配置解析失败，已停用全部自动撤回: " + err.Error())
			return 0
		}
		if len(policies) > 0 {
			if delay := policies[kind]; delay > 0 {
				return delay
			}
			return 0
		}
	}
	// 以下为 recall_policies 未配置（空串或 {}）时的旧行为回落。
	enabled, legacyDelay := AutoRecallFailed()
	if !enabled {
		return 0
	}
	// bind_fail 是用户看到绑定失败原因的唯一入口，10 秒在手机上读不完，单独给更长保留时间。
	if kind == RecallKindBindFail {
		return DefaultBindFailRecallSeconds
	}
	if kind == RecallKindFailureNotice || isFailureReply(content) {
		return legacyDelay
	}
	return 0
}
