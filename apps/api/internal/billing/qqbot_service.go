package billing

import (
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/usage"
)

// 按钮 ID 与回调数据
const (
	ButtonIDCheckin   = "nailao_checkin"
	ButtonDataCheckin = "/签到"

	// 指令按钮：点击后在输入框插入 @bot /签到 由用户自己发送
	ButtonIDJoinCheckin = "nailao_join_checkin"
)

// 签到指令关键字，去掉 @机器人 前缀后进行匹配
var checkinCommands = []string{"/签到", "签到"}

var (
	clientMu     sync.RWMutex
	cachedClient *apiClient
	cachedTM     *tokenManager
)

// getClient 返回与当前后台配置匹配的 API 客户端
// 配置变更后会自动重建，避免使用旧的 AppID/Secret
func getClient() (*apiClient, error) {
	setting := GetQQBotSetting()
	if setting.AppID == "" || setting.AppSecret == "" {
		return nil, fmt.Errorf("QQ 机器人未配置 AppID/AppSecret")
	}

	clientMu.RLock()
	if cachedClient != nil && cachedTM != nil &&
		!cachedTM.credentialsChanged(setting.AppID, setting.AppSecret) {
		c := cachedClient
		clientMu.RUnlock()
		return c, nil
	}
	clientMu.RUnlock()

	clientMu.Lock()
	defer clientMu.Unlock()
	// 双检
	if cachedClient != nil && cachedTM != nil &&
		!cachedTM.credentialsChanged(setting.AppID, setting.AppSecret) {
		return cachedClient, nil
	}

	cachedTM = newTokenManager(setting.AppID, setting.AppSecret)
	cachedClient = newAPIClient(cachedTM)
	return cachedClient, nil
}

// GroupAtMessageEvent 群 @机器人 消息事件体
type GroupAtMessageEvent struct {
	ID     string `json:"id"`
	Author struct {
		ID           string `json:"id"`
		MemberOpenID string `json:"member_openid"`
		UnionOpenID  string `json:"union_openid"`
		Username     string `json:"username"`
		Bot          bool   `json:"bot"`
	} `json:"author"`
	Content     string `json:"content"`
	GroupOpenID string `json:"group_openid"`
	Timestamp   string `json:"timestamp"`

	// Mentions 为消息中被 @ 的对象，含机器人自己。
	// 转账靠它确定收款人，而不是去解析 content 里的昵称文本
	//（昵称可重复、可含空格，不可靠）。
	Mentions []Mention `json:"mentions"`
}

// Mention 消息中被 @ 的对象
type Mention struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Bot      bool   `json:"bot"`
}

// InteractionEvent 互动事件体
type InteractionEvent struct {
	ID                string `json:"id"` // d.id，即 interaction_id，用于回应互动 PUT /interactions/{id}
	Type              int    `json:"type"`
	Scene             string `json:"scene"`
	ChatType          int    `json:"chat_type"`
	Timestamp         string `json:"timestamp"`
	UserOpenID        string `json:"user_openid"`
	GroupOpenID       string `json:"group_openid"`
	GroupMemberOpenID string `json:"group_member_openid"`
	Data              struct {
		Type     int `json:"type"`
		Resolved struct {
			ButtonData string `json:"button_data"`
			ButtonID   string `json:"button_id"`
		} `json:"resolved"`
	} `json:"data"`

	// PayloadEventID 为 webhook payload 最外层的 id（由 controller 赋值，不参与 JSON 解析）
	// 发送被动消息时的 event_id 必须使用此字段，而非 d.id
	PayloadEventID string `json:"-"`
}

// GroupJoinRequestEvent 入群申请事件体
type GroupJoinRequestEvent struct {
	GroupOpenID   string `json:"group_openid"`
	JoinRequestID string `json:"join_request_id"`
	MemberOpenID  string `json:"member_openid"`
	UnionOpenID   string `json:"union_openid"`
	Username      string `json:"username"`
	ApplySource   string `json:"apply_source"`
	Bot           bool   `json:"bot"`
	VerifyInfo    struct {
		Method        string `json:"method"`
		VerifyMessage string `json:"verify_message"`
		ReviewQAList  []struct {
			Question string `json:"question"`
			Answer   string `json:"answer"`
		} `json:"review_qa_list"`
	} `json:"verify_info"`
	AutoApproved map[string]any `json:"auto_approved"`
}

// isCheckinCommand 判断消息内容是否为签到指令
func isCheckinCommand(content string) bool {
	text := strings.TrimSpace(content)
	// 客户端可能把 @机器人 的文本一并带上，去掉所有 <...> 标签后再匹配
	text = stripTags(text)
	text = strings.TrimSpace(text)
	for _, cmd := range checkinCommands {
		if text == cmd {
			return true
		}
	}
	return false
}

// stripTags 去掉消息里的 <qqbot-at-user .../> 之类的标签文本
func stripTags(content string) string {
	var sb strings.Builder
	depth := 0
	for _, r := range content {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				sb.WriteRune(r)
			}
		}
	}
	return sb.String()
}

// looksLikeBindCode 判断内容是否形似六位绑定验证码
func looksLikeBindCode(content string) bool {
	return extractBindCode(content) != ""
}

// extractBindCode 从消息内容中提取六位绑定验证码
// 兼容用户把验证码和其他文字一起发送、或客户端保留了 @机器人 文本的情况
func extractBindCode(content string) string {
	// 先按空白切分，逐个 token 判断；再退化为整体判断
	fields := strings.Fields(content)
	for _, f := range fields {
		if isBindCodeToken(f) {
			return f
		}
	}
	return ""
}

// isBindCodeToken 判断单个词是否是六位字母数字验证码
func isBindCodeToken(text string) bool {
	if len(text) != 6 {
		return false
	}
	for _, r := range text {
		isDigit := r >= '0' && r <= '9'
		isUpper := r >= 'A' && r <= 'Z'
		isLower := r >= 'a' && r <= 'z'
		if !isDigit && !isUpper && !isLower {
			return false
		}
	}
	return true
}

// atUser 生成 markdown 中的 @用户 语法
func atUser(openID string) string {
	if openID == "" {
		return ""
	}
	return fmt.Sprintf("<qqbot-at-user id=\"%s\" />", openID)
}

// checkinKeyboard 构造签到消息下方的按钮
// 「我也要签到」为指令按钮，点击后在输入框插入 @bot /签到
// 「一键签到」为回调按钮，点击后直接触发后台签到
func checkinKeyboard() *Keyboard {
	return &Keyboard{
		Content: &KeyboardContent{
			Rows: []Row{
				{
					Buttons: []Button{
						{
							ID: ButtonIDJoinCheckin,
							RenderData: &RenderData{
								Label: "我也要签到",
								Style: 0,
							},
							Action: &Action{
								Type:          2, // 指令按钮
								Permission:    &Permission{Type: 2},
								Data:          ButtonDataCheckin,
								UnsupportTips: "请升级 QQ 版本后使用",
								Reply:         true,
							},
						},
						{
							ID: ButtonIDCheckin,
							RenderData: &RenderData{
								Label: "一键签到",
								Style: 1,
							},
							Action: &Action{
								Type:          1, // 回调按钮
								Permission:    &Permission{Type: 2},
								Data:          ButtonDataCheckin,
								UnsupportTips: "请升级 QQ 版本后使用",
							},
						},
					},
				},
			},
		},
	}
}

// renderNotify 按后台配置的通知样式渲染文案
// 支持占位符 {货币} 与 {金额}
func renderNotify(quota int) string {
	tpl := GetNotifyTemplate()
	symbol, amount := formatQuotaParts(quota)
	out := strings.ReplaceAll(tpl, "{货币}", symbol)
	out = strings.ReplaceAll(out, "{金额}", amount)
	return out
}

// formatQuotaParts 将额度拆分为货币符号与金额字符串
func formatQuotaParts(quota int) (symbol string, amount string) {
	displayType := GetQuotaDisplayType()
	if displayType == QuotaDisplayTypeTokens {
		return "点额度", fmt.Sprintf("%d", quota)
	}

	symbol = GetCurrencySymbol()
	usd := float64(quota) / common.QuotaPerUnit
	rate := GetUsdToCurrencyRate(USDExchangeRate)
	value := usd * rate
	return symbol, fmt.Sprintf("%.6f", value)
}

// formatQuotaText 返回「符号+金额」形式的完整文本
func formatQuotaText(quota int) string {
	symbol, amount := formatQuotaParts(quota)
	if symbol == "点额度" {
		return amount + " 点额度"
	}
	return symbol + amount
}

// buildCheckinSuccessMarkdown 构造签到成功的 markdown 文案
// QQ 群 markdown 单个 \n 会被折叠为空格，必须用 \n\n 才能换行
func buildCheckinSuccessMarkdown(openID string, awarded int, balance int) string {
	var sb strings.Builder
	sb.WriteString(atUser(openID))
	sb.WriteString(" **签到成功！**\n\n")
	sb.WriteString(fmt.Sprintf("您已获得 %s！\n\n", formatQuotaText(awarded)))
	sb.WriteString(fmt.Sprintf("当前余额 %s", formatQuotaText(balance)))
	return sb.String()
}

// buildCheckinFailMarkdown 构造未绑定时的提示文案
func buildCheckinFailMarkdown(openID string) string {
	var sb strings.Builder
	sb.WriteString(atUser(openID))
	sb.WriteString(" **签到失败！**\n\n")
	sb.WriteString("请登陆后在 个人资料→每日签到→QQ签到 获取验证码进行绑定")
	return sb.String()
}

// buildPlainMarkdown 构造带 @ 的普通提示文案
// text 内部若含换行请使用 \n\n（QQ 群 markdown 要求）
func buildPlainMarkdown(openID, text string) string {
	return atUser(openID) + " " + text
}

// msgSeqMu 保护 msgSeqCounter
var (
	msgSeqMu      sync.Mutex
	msgSeqCounter = make(map[string]int)
	msgSeqOrder   []string
)

// nextMsgSeq 为同一个被动消息凭证分配递增的 msg_seq
//
// 平台要求：同一 msg_id / event_id 下发送多条消息时 msg_seq 必须不同，
// 全部写死 1 的话第二条起会被静默丢弃（接口仍返回成功）。
func nextMsgSeq(token string) int {
	if token == "" {
		return 1
	}
	msgSeqMu.Lock()
	defer msgSeqMu.Unlock()
	if _, ok := msgSeqCounter[token]; !ok {
		msgSeqOrder = append(msgSeqOrder, token)
		// 只保留最近 500 个凭证的计数，防止长期运行内存堆积
		if len(msgSeqOrder) > 500 {
			delete(msgSeqCounter, msgSeqOrder[0])
			msgSeqOrder = msgSeqOrder[1:]
		}
	}
	msgSeqCounter[token]++
	return msgSeqCounter[token]
}

// replyGroupMarkdown 以被动消息形式回复群聊 markdown + 键盘
//
// seq 参数保留兼容旧调用，实际发送时按 msg_id/event_id 自增，
// 避免同一凭证下多条消息因 msg_seq 相同被平台丢弃。
func replyGroupMarkdown(groupOpenID, msgID, eventID, content string, keyboard *Keyboard, seq int) error {
	client, err := getClient()
	if err != nil {
		return err
	}
	token := msgID
	if token == "" {
		token = eventID
	}
	if s := nextMsgSeq(token); s > seq {
		seq = s
	}
	req := &GroupMessageRequest{
		MsgType:  2, // Markdown
		Markdown: &MessageMarkdown{Content: content},
		Keyboard: keyboard,
		MsgID:    msgID,
		EventID:  eventID,
		MsgSeq:   seq,
	}
	_, err = client.SendGroupMessage(groupOpenID, req)
	if err == nil {
		return nil
	}

	// 被动消息失败（msg_id/event_id 过期或无效）时退化为主动消息
	// 保证签到结果无论如何都能发到群里
	common.SysError("被动回复失败，改用主动消息重试: " + err.Error())
	activeReq := &GroupMessageRequest{
		MsgType:  2,
		Markdown: &MessageMarkdown{Content: content},
		Keyboard: keyboard,
		MsgSeq:   seq + 1,
	}
	if _, activeErr := client.SendGroupMessage(groupOpenID, activeReq); activeErr != nil {
		return fmt.Errorf("被动回复失败(%v)，主动消息也失败(%v)", err, activeErr)
	}
	return nil
}

// doCheckinForOpenID 执行签到主流程
// 返回需要回复的 markdown 文案与是否附带按钮
func doCheckinForOpenID(openID, groupOpenID string) (content string, withKeyboard bool) {
	setting := GetQQBotSetting()
	if !setting.QQCheckinEnabled {
		return buildPlainMarkdown(openID, "**签到失败！**\nQQ 签到功能当前未开启"), false
	}

	userId, bound := identity.IsQQBound(openID)
	if !bound {
		return buildCheckinFailMarkdown(openID), false
	}

	checkin, err := UserQQCheckin(userId, openID, groupOpenID)
	if err != nil {
		return buildPlainMarkdown(openID, "**签到失败！**\n\n"+err.Error()), true
	}

	// 读取签到后的最新余额
	balance, qErr := identity.GetUserQuota(userId, true)
	if qErr != nil {
		common.SysError("QQ 签到后查询余额失败: " + qErr.Error())
	}

	usage.RecordLog(userId, usage.LogTypeSystem,
		fmt.Sprintf("QQ 签到，获得额度 %s", logger.LogQuota(checkin.QuotaAwarded)))

	// 首行加粗标题采用后台可配的通知样式，正文两行固定
	// QQ 群 markdown 单个 \n 会被折叠为空格，段落必须用 \n\n 分隔
	notify := renderNotify(checkin.QuotaAwarded)
	var sb strings.Builder
	sb.WriteString(atUser(openID))
	sb.WriteString(" **")
	sb.WriteString(notify)
	sb.WriteString("**\n\n")
	sb.WriteString(fmt.Sprintf("您已获得 %s！\n\n", formatQuotaText(checkin.QuotaAwarded)))
	sb.WriteString(fmt.Sprintf("当前余额 %s", formatQuotaText(balance)))
	return sb.String(), true
}

// HandleGroupAtMessage 处理群 @机器人 消息
func HandleGroupAtMessage(event *GroupAtMessageEvent) {
	if event == nil || event.Author.Bot {
		return
	}
	// 同一条消息可能通过 GROUP_AT_MESSAGE_CREATE 与 GROUP_MESSAGE_CREATE
	// 两个事件各推一次，去重后再处理，否则掉落计数会翻倍
	if markMessageSeen(event.ID) {
		return
	}

	openID := event.Author.MemberOpenID
	if openID == "" {
		openID = event.Author.ID
	}
	content := strings.TrimSpace(event.Content)

	if cmd, ok := isDropCommand(content); ok {
		reply := HandleDropCommand(cmd, openID, event.GroupOpenID)
		if err := replyGroupMarkdown(event.GroupOpenID, event.ID, "", reply, nil, 1); err != nil {
			common.SysError("回复掉落指令失败: " + err.Error())
		}
		return
	}

	if cmd, ok := isCheckinSwitchCommand(content); ok {
		reply := HandleCheckinSwitchCommand(cmd, openID, event.GroupOpenID)
		if err := replyGroupMarkdown(event.GroupOpenID, event.ID, "", reply, nil, 1); err != nil {
			common.SysError("回复签到开关指令失败: " + err.Error())
		}
		return
	}

	if cmd, ok := isTransferSwitchCommand(content); ok {
		reply := HandleTransferSwitchCommand(cmd, openID, event.GroupOpenID)
		if err := replyGroupMarkdown(event.GroupOpenID, event.ID, "", reply, nil, 1); err != nil {
			common.SysError("回复转账开关指令失败: " + err.Error())
		}
		return
	}

	// /转账费率 必须排在 /转账 之前判断，否则会被前缀匹配吃掉
	if isTransferInfoCommand(content) {
		reply := HandleTransferInfo(openID)
		if err := replyGroupMarkdown(event.GroupOpenID, event.ID, "", reply, nil, 1); err != nil {
			common.SysError("回复转账费率失败: " + err.Error())
		}
		return
	}

	if isTransferCommand(content) {
		reply := HandleTransferCommand(event, openID)
		if err := replyGroupMarkdown(event.GroupOpenID, event.ID, "", reply, nil, 1); err != nil {
			common.SysError("回复转账指令失败: " + err.Error())
		}
		return
	}

	if isBalanceCommand(content) {
		reply := HandleMyBalance(openID)
		if err := replyGroupMarkdown(event.GroupOpenID, event.ID, "", reply, nil, 1); err != nil {
			common.SysError("回复余额查询失败: " + err.Error())
		}
		return
	}

	if isMenuCommand(content) {
		reply, kb := HandleMenuCommand(openID)
		if err := replyGroupMarkdown(event.GroupOpenID, event.ID, "", reply, kb, 1); err != nil {
			common.SysError("回复菜单失败: " + err.Error())
		}
		return
	}

	if isRedPacketCommand(content) {
		reply, kb := HandleRedPacketCommand(event, openID)
		if err := replyGroupMarkdown(event.GroupOpenID, event.ID, "", reply, kb, 1); err != nil {
			common.SysError("回复红包指令失败: " + err.Error())
		}
		return
	}

	switch {
	case isCheckinCommand(content):
		// 本群签到被单独关闭时直接回绝，不影响其他群与网页签到
		if IsCheckinDisabledGroup(event.GroupOpenID) {
			if err := replyGroupMarkdown(event.GroupOpenID, event.ID, "",
				checkinDisabledReply(openID), nil, 1); err != nil {
				common.SysError("回复本群签到已关闭失败: " + err.Error())
			}
			return
		}
		reply, withKeyboard := doCheckinForOpenID(openID, event.GroupOpenID)
		var kb *Keyboard
		if withKeyboard {
			kb = checkinKeyboard()
		}
		if err := replyGroupMarkdown(event.GroupOpenID, event.ID, "", reply, kb, 1); err != nil {
			common.SysError("回复群签到消息失败: " + err.Error())
		}

	case looksLikeBindCode(content):
		// 群内发送六位验证码即完成绑定
		code := extractBindCode(content)
		userId, err := identity.ConsumeQQBindCode(
			code, openID, event.Author.UnionOpenID, event.Author.Username)
		if err != nil {
			reply := buildPlainMarkdown(openID, "**绑定失败！**\n\n"+err.Error())
			if sendErr := replyGroupMarkdown(event.GroupOpenID, event.ID, "", reply, nil, 1); sendErr != nil {
				common.SysError("回复绑定失败消息失败: " + sendErr.Error())
			}
			return
		}

		usage.RecordLog(userId, usage.LogTypeSystem, "已绑定 QQ 账号，可使用 QQ 签到")
		reply := buildPlainMarkdown(openID,
			"**绑定成功！**\n\n现在可以直接发送 /签到 领取每日额度")
		if sendErr := replyGroupMarkdown(
			event.GroupOpenID, event.ID, "", reply, checkinKeyboard(), 1); sendErr != nil {
			common.SysError("回复绑定成功消息失败: " + sendErr.Error())
		}

	default:
		// 普通水群消息：累计掉落进度，命中阈值时发放奖励
		HandleGroupChatForDrop(event)
	}
}

// HandleInteraction 处理按钮回调事件
func HandleInteraction(event *InteractionEvent) {
	if event == nil {
		return
	}

	// 仅 type=11 消息按钮 / type=12 快捷菜单需要回应
	// 必须「先回应、后处理业务」：回应慢了客户端会直接提示「请求第三方失败」
	if event.Type == 11 || event.Type == 12 {
		if client, err := getClient(); err != nil {
			common.SysError("回应互动事件失败: " + err.Error())
		} else if err := client.AnswerInteraction(event.ID, 0); err != nil {
			common.SysError("回应互动事件失败: " + err.Error())
		} else {
			common.SysLog("已回应互动事件 interaction_id=" + event.ID)
		}
	}

	// 仅处理群聊场景下的签到按钮
	if event.Scene != "group" || event.GroupOpenID == "" {
		common.SysLog(fmt.Sprintf("互动事件非群聊场景，忽略 scene=%s", event.Scene))
		return
	}
	buttonData := event.Data.Resolved.ButtonData
	buttonID := event.Data.Resolved.ButtonID
	openID := event.GroupMemberOpenID

	// 被动消息回复时 event_id 取 payload 最外层 id（非 d.id）
	replyEventID := event.PayloadEventID
	if replyEventID == "" {
		replyEventID = event.ID
	}

	// 菜单系统按钮
	if isMenuCallback(buttonData) {
		reply, kb := HandleMenuCallback(buttonData, openID, event.GroupOpenID)
		if err := replyGroupMarkdown(
			event.GroupOpenID, "", replyEventID, reply, kb, 1); err != nil {
			common.SysError("回复菜单回调失败: " + err.Error())
		}
		return
	}

	// 抢红包按钮，data 形如 nailao_rp_grab:<id>
	if strings.HasPrefix(buttonData, ButtonDataRedPacketGrab) {
		packetID := strings.TrimPrefix(buttonData, ButtonDataRedPacketGrab)
		reply := HandleRedPacketGrab(packetID, openID)
		if err := replyGroupMarkdown(
			event.GroupOpenID, "", replyEventID, reply, nil, 1); err != nil {
			common.SysError("回复抢红包失败: " + err.Error())
		}
		return
	}

	// 红包战绩按钮
	if strings.HasPrefix(buttonData, ButtonDataRedPacketDetail) {
		packetID := strings.TrimPrefix(buttonData, ButtonDataRedPacketDetail)
		reply := HandleRedPacketDetail(packetID, openID)
		if err := replyGroupMarkdown(
			event.GroupOpenID, "", replyEventID, reply, nil, 1); err != nil {
			common.SysError("回复红包战绩失败: " + err.Error())
		}
		return
	}

	if buttonData != ButtonDataCheckin && buttonID != ButtonIDCheckin {
		common.SysLog(fmt.Sprintf("互动事件按钮不匹配 data=%s id=%s", buttonData, buttonID))
		return
	}

	// 按钮走的是另一条入口，同样要受群级开关约束。
	// 否则用户翻出历史消息点「一键签到」，仍能在已关闭的群里签到。
	if IsCheckinDisabledGroup(event.GroupOpenID) {
		replyEventID := event.PayloadEventID
		if replyEventID == "" {
			replyEventID = event.ID
		}
		if err := replyGroupMarkdown(event.GroupOpenID, "", replyEventID,
			checkinDisabledReply(openID), nil, 1); err != nil {
			common.SysError("回复本群签到已关闭失败: " + err.Error())
		}
		return
	}

	// 无论签到成功还是失败，都要在群里回复结果
	reply, withKeyboard := doCheckinForOpenID(openID, event.GroupOpenID)
	var kb *Keyboard
	if withKeyboard {
		kb = checkinKeyboard()
	}
	if err := replyGroupMarkdown(event.GroupOpenID, "", replyEventID, reply, kb, 1); err != nil {
		common.SysError("回复按钮签到消息失败: " + err.Error())
		return
	}
	common.SysLog("按钮签到已回复群消息 openid=" + openID)
}

// HandleGroupJoinRequest 处理入群申请事件，命中关键词时自动同意
func HandleGroupJoinRequest(event *GroupJoinRequestEvent) {
	if event == nil {
		return
	}
	setting := GetQQBotSetting()
	if !setting.AutoApproveEnabled {
		return
	}
	keyword := strings.TrimSpace(setting.AutoApproveKeyword)
	if keyword == "" {
		return
	}
	// 已被平台自动审批策略处理过的申请不再重复操作
	if len(event.AutoApproved) > 0 {
		return
	}

	texts := []string{event.Username, event.VerifyInfo.VerifyMessage}
	for _, qa := range event.VerifyInfo.ReviewQAList {
		texts = append(texts, qa.Question, qa.Answer)
	}
	full := strings.ToLower(strings.Join(texts, "\n"))
	if !strings.Contains(full, strings.ToLower(keyword)) {
		return
	}

	client, err := getClient()
	if err != nil {
		common.SysError("自动审批入群申请失败: " + err.Error())
		return
	}
	if err := client.ApproveJoinRequest(
		event.GroupOpenID, event.MemberOpenID, event.JoinRequestID); err != nil {
		common.SysError("自动审批入群申请失败: " + err.Error())
		return
	}
	common.SysLog(fmt.Sprintf("已自动同意 %s 的入群申请", event.Username))
}

// SyncCommandPanel 将「签到」指令面板同步到 QQ 平台（group 全局生效）
// 幂等：先删除已有的 group 场景面板，再重建，避免重复堆积（上限 20 个）
func SyncCommandPanel() error {
	client, err := getClient()
	if err != nil {
		return err
	}

	// 清理已存在的 group 面板
	panels, err := client.ListPanels("group")
	if err != nil {
		common.SysError("查询指令面板列表失败: " + err.Error())
	} else {
		for _, p := range panels {
			if p.Scope == "group" {
				if delErr := client.DeletePanel(p.PanelID); delErr != nil {
					common.SysError("删除旧指令面板失败: " + delErr.Error())
				}
			}
		}
	}

	req := &CreatePanelRequest{
		Scope:      "group",
		TargetType: "all",
		Panel: Panel{
			Items: []PanelItem{
				{
					Type: "command",
					Name: "签到",
					Desc: "领取每日额度",
				},
			},
			Remark: "奶酪签到指令面板",
		},
	}
	panelID, err := client.CreatePanel(req)
	if err != nil {
		return err
	}
	common.SysLog("已同步 QQ 指令面板，panel_id=" + panelID)
	return nil
}
