package billing

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"unicode"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/dbinfra"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/usage"
)

// 掉落功能相关的群指令。
// 平台的 webhook 事件里不含群名称，官方 API 也没有「按群名查询」的接口，
// 因此无法按「群名包含聊天群」自动判定。改为管理员在目标群内发送指令，
// 由机器人记录该群的 group_openid 到白名单。
const (
	CmdEnableDrop  = "/开启掉落"
	CmdDisableDrop = "/关闭掉落"
	CmdDropStatus  = "/掉落状态"

	// DropGroupsOptionKey 白名单在 options 表中的键，与配置结构的 json tag 对应
	DropGroupsOptionKey = "qq_bot_setting.drop_groups"

	// dedupWindow 每个群保留多少条历史消息用于重复判定
	dedupWindow = 20

	// msgIDCacheSize 已处理消息 ID 的缓存容量，防止同一条消息被
	// GROUP_AT_MESSAGE_CREATE 与 GROUP_MESSAGE_CREATE 双事件重复计数
	msgIDCacheSize = 256
)

// groupDropState 单个群的掉落计数状态（进程内内存态，重启后重新摇点）
type groupDropState struct {
	count  int      // 自上次掉落以来累计的有效消息数
	target int      // 本轮触发所需的消息数，命中后重新随机
	recent []string // 最近若干条消息的归一化文本，用于重复过滤
}

var (
	dropMu     sync.Mutex
	dropStates = make(map[string]*groupDropState)

	seenMsgIDs   = make(map[string]struct{}, msgIDCacheSize)
	seenMsgOrder []string
)

// IsDropGroup 判断该群是否已开启掉落
func IsDropGroup(groupOpenID string) bool {
	return containsGroup(dropGroupList(), groupOpenID)
}

// dropGroupList 解析白名单配置
func dropGroupList() []string {
	return parseGroupIDs(GetQQBotSetting().DropGroups)
}

// setDropGroups 持久化白名单
// 写 options 表后由 updateOptionMap → config 系统回填内存配置，无需手动赋值
func setDropGroups(groups []string) error {
	return dbinfra.UpdateOption(DropGroupsOptionKey, strings.Join(groups, ","))
}

// addDropGroup 把群加入白名单，返回是否发生变更
func addDropGroup(groupOpenID string) (bool, error) {
	if IsDropGroup(groupOpenID) {
		return false, nil
	}
	groups := append(dropGroupList(), groupOpenID)
	if err := setDropGroups(groups); err != nil {
		return false, err
	}
	return true, nil
}

// removeDropGroup 把群移出白名单，返回是否发生变更
func removeDropGroup(groupOpenID string) (bool, error) {
	current := dropGroupList()
	out := make([]string, 0, len(current))
	for _, g := range current {
		if g != groupOpenID {
			out = append(out, g)
		}
	}
	if len(out) == len(current) {
		return false, nil
	}
	if err := setDropGroups(out); err != nil {
		return false, err
	}
	return true, nil
}

// isDropAdmin 判断 openid 对应的账号是否为管理员
//
// 数据库未初始化时（单测、启动早期）一律视为非管理员：
// 鉴权失败要往「拒绝」方向倒，不能因为查不到就放行。
func isDropAdmin(openID string) bool {
	if dbx.DB == nil {
		return false
	}
	userId, bound := identity.IsQQBound(openID)
	if !bound {
		return false
	}
	user, err := identity.GetUserById(userId, false)
	if err != nil || user == nil {
		return false
	}
	return user.Role >= common.RoleAdminUser
}

// isDropCommand 判断是否为掉落管理指令
func isDropCommand(content string) (cmd string, ok bool) {
	text := strings.TrimSpace(stripTags(content))
	switch text {
	case CmdEnableDrop, CmdDisableDrop, CmdDropStatus:
		return text, true
	}
	return "", false
}

// HandleDropCommand 处理掉落管理指令，返回要回复的 markdown
func HandleDropCommand(cmd, openID, groupOpenID string) string {
	if !isDropAdmin(openID) {
		return buildPlainMarkdown(openID, "**权限不足！**\n\n只有站点管理员可以管理掉落功能")
	}

	switch cmd {
	case CmdEnableDrop:
		changed, err := addDropGroup(groupOpenID)
		if err != nil {
			return buildPlainMarkdown(openID, "**操作失败！**\n\n"+err.Error())
		}
		if !changed {
			return buildPlainMarkdown(openID, "**本群已在掉落名单中**\n\n"+dropStatusText())
		}
		common.SysLog("QQ 掉落已在群启用 group_openid=" + groupOpenID)
		return buildPlainMarkdown(openID, "**已开启本群掉落！**\n\n"+dropStatusText())

	case CmdDisableDrop:
		changed, err := removeDropGroup(groupOpenID)
		if err != nil {
			return buildPlainMarkdown(openID, "**操作失败！**\n\n"+err.Error())
		}
		if !changed {
			return buildPlainMarkdown(openID, "**本群未开启掉落**")
		}
		common.SysLog("QQ 掉落已在群关闭 group_openid=" + groupOpenID)
		return buildPlainMarkdown(openID, "**已关闭本群掉落**")

	default: // CmdDropStatus
		state := "未开启"
		if IsDropGroup(groupOpenID) {
			state = "已开启"
		}
		return buildPlainMarkdown(openID,
			fmt.Sprintf("**本群掉落：%s**\n\n%s", state, dropStatusText()))
	}
}

// dropStatusText 汇总当前掉落参数，用于指令回执
func dropStatusText() string {
	s := GetQQBotSetting()
	enabled := "开"
	if !s.DropEnabled {
		enabled = "关"
	}
	return fmt.Sprintf(
		"全局开关：%s\n\n触发间隔：%d-%d 条消息\n\n单次掉落：%s-%s\n\n每人每日上限：%d 次",
		enabled, s.DropMinMessages, s.DropMaxMessages,
		formatDropAmount(s.DropMinQuota), formatDropAmount(s.DropMaxQuota),
		s.DropDailyLimit,
	)
}

// isMessageCountable 判断一条群消息是否计入掉落进度
// 指令、纯符号、过短内容（如「1」「嗯」）一律不计；
// 但表情包、图片等 QQ 富媒体消息算数。
func isMessageCountable(content string) (normalized string, ok bool) {
	raw := strings.TrimSpace(content)
	if raw == "" {
		return "", false
	}

	// QQ 表情包 / 图片等富媒体标签：剥标签后会变空串，
	// 但它们是真实的群聊活跃度，应该计数。
	// 用固定标记归一化：同一种表情包不重复刷，但不同表情包都算数。
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "<facetype=") || strings.Contains(lower, "<img") ||
		strings.Contains(lower, "<facetype") {
		// 提取 faceId 用于区分不同表情
		if idx := strings.Index(lower, "faceid="); idx >= 0 {
			rest := raw[idx+7:]
			// 去掉引号和到下一个 > 的部分
			rest = strings.TrimLeft(rest, "\"'")
			end := strings.IndexAny(rest, "\">")
			if end > 0 {
				return "[face:" + rest[:end] + "]", true
			}
		}
		return "[sticker]", true
	}

	text := strings.TrimSpace(stripTags(raw))
	if text == "" {
		return "", false
	}
	// 任何斜杠指令都不计数（/签到 等）
	if strings.HasPrefix(text, "/") {
		return "", false
	}
	// 无斜杠的签到别名同样排除
	if isCheckinCommand(text) {
		return "", false
	}
	// 绑定验证码不计数，避免用户绑定动作被算作水群
	if looksLikeBindCode(text) {
		return "", false
	}
	// 转账相关指令不计数
	if isTransferCommand(text) || isTransferInfoCommand(text) ||
		isBalanceCommand(text) {
		return "", false
	}
	if _, ok := isTransferSwitchCommand(text); ok {
		return "", false
	}
	if _, ok := isCheckinSwitchCommand(text); ok {
		return "", false
	}
	if _, ok := isDropCommand(text); ok {
		return "", false
	}
	// 菜单与红包指令不计数
	if isMenuCommand(text) || isRedPacketCommand(text) {
		return "", false
	}

	// 归一化：去掉所有空白与标点，只保留字母数字与文字
	var sb strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
		}
	}
	normalized = sb.String()
	// 有效字符少于 2 个的消息（「1」「嗯」「。。。」）不计数
	if len([]rune(normalized)) < 2 {
		return "", false
	}
	return normalized, true
}

// randomTarget 随机本轮触发所需的消息数
func randomTarget(min, max int) int {
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	if max == min {
		return min
	}
	return min + rand.Intn(max-min+1)
}

// randomDropQuota 随机单次掉落额度
func randomDropQuota(min, max int) int {
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	if max == min {
		return min
	}
	return min + rand.Intn(max-min+1)
}

// markMessageSeen 记录消息 ID，返回 true 表示此前已处理过
func markMessageSeen(msgID string) bool {
	if msgID == "" {
		return false
	}
	if _, ok := seenMsgIDs[msgID]; ok {
		return true
	}
	seenMsgIDs[msgID] = struct{}{}
	seenMsgOrder = append(seenMsgOrder, msgID)
	if len(seenMsgOrder) > msgIDCacheSize {
		delete(seenMsgIDs, seenMsgOrder[0])
		seenMsgOrder = seenMsgOrder[1:]
	}
	return false
}

// tickDropCounter 累计一条有效消息，返回是否触发掉落
// 触发后不立即清零：只有真正发放成功才重置，
// 这样触发者未绑定/已达上限时这次掉落会顺延给下一个说话的人。
func tickDropCounter(groupOpenID, normalized string, minMsg, maxMsg int) bool {
	dropMu.Lock()
	defer dropMu.Unlock()

	st, ok := dropStates[groupOpenID]
	if !ok {
		st = &groupDropState{target: randomTarget(minMsg, maxMsg)}
		dropStates[groupOpenID] = st
	}

	// 与最近 dedupWindow 条比较，重复内容不计数（刷屏、复读）
	for _, prev := range st.recent {
		if prev == normalized {
			return false
		}
	}
	st.recent = append(st.recent, normalized)
	if len(st.recent) > dedupWindow {
		st.recent = st.recent[len(st.recent)-dedupWindow:]
	}

	st.count++
	return st.count >= st.target
}

// resetDropCounter 掉落发放成功后重置计数并重新摇点
func resetDropCounter(groupOpenID string, minMsg, maxMsg int) {
	dropMu.Lock()
	defer dropMu.Unlock()
	if st, ok := dropStates[groupOpenID]; ok {
		st.count = 0
		st.target = randomTarget(minMsg, maxMsg)
	}
}

// formatDropAmount 把额度渲染成货币数值，去掉多余的尾随零
// 例：150000 → 0.3，1500000 → 3
func formatDropAmount(quota int) string {
	if GetQuotaDisplayType() == QuotaDisplayTypeTokens {
		return fmt.Sprintf("%d", quota)
	}
	return trimFloat(quotaToUnits(quota))
}

// dropUnitName 按数额给出单位称呼：小于 1 为碎屑，大于等于 1 为碎片
func dropUnitName(quota int) string {
	if GetQuotaDisplayType() == QuotaDisplayTypeTokens {
		return ""
	}
	if quotaToUnits(quota) < 1 {
		return "奶酪碎屑"
	}
	return "奶酪碎片"
}

// renderDropMessage 渲染掉落文案
// 支持占位符：{@} {金额} {货币} {单位} {余额}
func renderDropMessage(openID string, awarded, balance int) string {
	tpl := GetDropTemplate()
	symbol := ""
	if GetQuotaDisplayType() != QuotaDisplayTypeTokens {
		symbol = GetCurrencySymbol()
	}
	out := strings.ReplaceAll(tpl, "{@}", atUser(openID))
	out = strings.ReplaceAll(out, "{金额}", formatDropAmount(awarded))
	out = strings.ReplaceAll(out, "{货币}", symbol)
	out = strings.ReplaceAll(out, "{单位}", dropUnitName(awarded))
	out = strings.ReplaceAll(out, "{余额}", formatDropAmount(balance))
	return out
}

// HandleGroupChatForDrop 处理一条普通群消息的掉落逻辑
// 该函数只在 setting.DropEnabled 且群在白名单时才会做实际工作
func HandleGroupChatForDrop(event *GroupAtMessageEvent) {
	if event == nil || event.Author.Bot {
		return
	}
	s := GetQQBotSetting()
	if !s.DropEnabled || !IsDropGroup(event.GroupOpenID) {
		return
	}

	normalized, ok := isMessageCountable(event.Content)
	if !ok {
		return
	}
	if !tickDropCounter(event.GroupOpenID, normalized, s.DropMinMessages, s.DropMaxMessages) {
		return
	}

	openID := event.Author.MemberOpenID
	if openID == "" {
		openID = event.Author.ID
	}
	userId, bound := identity.IsQQBound(openID)
	if !bound {
		// 未绑定的用户接不住奖励，计数保留，顺延给下一位
		return
	}

	quota := randomDropQuota(s.DropMinQuota, s.DropMaxQuota)
	drop, err := AwardQQDrop(userId, openID, event.GroupOpenID, quota, s.DropDailyLimit)
	if err != nil {
		if IsQQDropLimitReached(err) {
			// 今日次数用完，同样顺延，不打扰群聊
			return
		}
		common.SysError("发放 QQ 掉落奖励失败: " + err.Error())
		return
	}

	resetDropCounter(event.GroupOpenID, s.DropMinMessages, s.DropMaxMessages)

	balance, qErr := identity.GetUserQuota(userId, true)
	if qErr != nil {
		common.SysError("掉落后查询余额失败: " + qErr.Error())
	}
	usage.RecordLog(userId, usage.LogTypeSystem,
		fmt.Sprintf("QQ 群聊掉落，获得额度 %s", logger.LogQuota(drop.QuotaAwarded)))

	content := renderDropMessage(openID, drop.QuotaAwarded, balance)
	if err := replyGroupMarkdown(
		event.GroupOpenID, event.ID, "", content, nil, 1); err != nil {
		common.SysError("发送掉落消息失败: " + err.Error())
	}
}
