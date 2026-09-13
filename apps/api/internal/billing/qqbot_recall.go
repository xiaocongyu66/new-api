package billing

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
)

// 撤回策略（recall_policies）：后台配置「内容类型 → 秒数」的 JSON 映射，
// 形如 {"drop_award":30,"checkin_fail":10}。0 或缺失 = 该类型不撤回；
// 配置为空串或 {} 时沿用旧行为：只有失败提示受 recall_failed_messages /
// recall_delay_seconds 这组旧开关控制。
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
//	RecallKindFailureNotice  failure_notice  isFailureReply 按内容识别的失败提示（旧行为，兜底）
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

// recallPolicyFor 返回某内容类型发出后自动撤回的秒数，0 表示不撤回。
//
// 优先级：recall_policies 解析出非空映射时完全接管（含 failure_notice，
// 旧开关被忽略）；配置为空串或 {} 时 failure_notice 走旧开关回落。
// 配置非空但解析失败时一律不撤回——宁可不撤，不可误撤。
func recallPolicyFor(kind string) int {
	setting := GetQQBotSetting()
	if raw := strings.TrimSpace(setting.RecallPolicies); raw != "" {
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
	if kind == RecallKindFailureNotice {
		if enabled, delay := AutoRecallFailed(); enabled {
			return delay
		}
	}
	return 0
}
