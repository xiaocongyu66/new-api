package billing

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/usage"
)

// 红包指令与按钮
//
// 发红包只能通过文字指令（需要输入金额和个数），抢红包用按钮。
// 按钮 data 里带红包 ID，格式 nailao_rp_grab:<id>。
const (
	CmdRedPacket = "/红包"

	ButtonIDRedPacketGrab   = "nailao_rp_grab"
	ButtonDataRedPacketGrab = "nailao_rp_grab:"

	// 一键领取：单按钮批量领取本群所有可抢红包，只回复一条汇总
	ButtonIDRedPacketGrabAll   = "nailao_rp_grab_all"
	ButtonDataRedPacketGrabAll = "nailao_rp_grab_all"

	ButtonIDRedPacketDetail   = "nailao_rp_detail"
	ButtonDataRedPacketDetail = "nailao_rp_detail:"
)

// redPacketAliases 发红包指令的可接受写法
var redPacketAliases = []string{"/红包", "红包", "/发红包"}

// isRedPacketCommand 判断是否为发红包指令
func isRedPacketCommand(content string) bool {
	text := strings.TrimSpace(stripTags(content))
	for _, alias := range redPacketAliases {
		if text == alias || strings.HasPrefix(text, alias+" ") {
			return true
		}
	}
	return false
}

// IsRedPacketDisabledGroup 判断该群是否已关闭红包
func IsRedPacketDisabledGroup(groupOpenID string) bool {
	return containsGroup(
		parseGroupIDs(GetQQBotSetting().RedPacketDisabledGroups),
		groupOpenID)
}

// parseRedPacketArgs 解析发红包参数：金额与个数
//
// 用法 /红包 10 5 表示 10 个货币单位分 5 份。
// 只给一个数字时按「金额」处理，个数取默认值。
// 剩余的非数字文本作为祝福语。
func parseRedPacketArgs(content string) (amountUnits float64, count int, blessing string) {
	text := stripTags(content)
	for _, alias := range redPacketAliases {
		text = strings.ReplaceAll(text, alias, " ")
	}

	var numbers []float64
	var words []string
	for _, token := range strings.Fields(text) {
		cleaned := strings.TrimFunc(token, func(r rune) bool {
			return !(r >= '0' && r <= '9') && r != '.'
		})
		if cleaned != "" {
			if v, err := strconv.ParseFloat(cleaned, 64); err == nil && v > 0 {
				numbers = append(numbers, v)
				continue
			}
		}
		words = append(words, token)
	}

	if len(numbers) > 0 {
		amountUnits = numbers[0]
	}
	if len(numbers) > 1 {
		// 个数取整，避免 /红包 10 5.5 这种写法产生小数份数
		count = int(numbers[1])
	}
	blessing = strings.TrimSpace(strings.Join(words, " "))
	// Fix 5: 祝福语解析后，tag 剥离并 40 字上限，净化 HTML/马甲文结构。
	blessing = sanitizeBlessing(blessing)
	if len([]rune(blessing)) > 40 {
		blessing = string([]rune(blessing)[:40])
	}
	return
}

// sanitizeBlessing whitelist-sanitizes blessing text to prevent markdown injection.
// Letters, digits, CJK, basic punctuation/spaces kept; * ` [ ] > # and newlines stripped.
// Keeps existing tests passing and adds one for sanitizer (Fix 5).
func sanitizeBlessing(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
			sb.WriteRune(r)
		case r == ' ' || r == '，' || r == '。' || r == '！' || r == '？' || r == '、' || r == '：' || r == '；' || r == '（' || r == '）' || r == '【' || r == '】' || r == '《' || r == '》':
			sb.WriteRune(r)
		// explicitly strip dangerous markdown chars and newlines
		case r == '*' || r == '`' || r == '[' || r == ']' || r == '>' || r == '#' || r == '\n' || r == '\r':
			continue
		default:
			// keep other safe punctuation
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// redPacketKeyboard 构造红包消息下方的按钮
//
// 抢红包用回调按钮：点一下就抢，不需要用户再发消息。
// Permission.Type=2 表示所有人可点。
func redPacketKeyboard(packetID int) *Keyboard {
	data := ButtonDataRedPacketGrab + strconv.Itoa(packetID)
	detailData := ButtonDataRedPacketDetail + strconv.Itoa(packetID)
	return &Keyboard{
		Content: &KeyboardContent{
			Rows: []Row{
				{
					Buttons: []Button{
						{
							ID: ButtonIDRedPacketGrab,
							RenderData: &RenderData{
								Label:        "抢红包",
								VisitedLabel: "已抢",
								Style:        1,
							},
							Action: &Action{
								Type:          1, // 回调按钮
								Permission:    &Permission{Type: 2},
								Data:          data,
								UnsupportTips: "请升级 QQ 版本后使用",
							},
						},
						{
							ID: ButtonIDRedPacketDetail,
							RenderData: &RenderData{
								Label: "查看战绩",
								Style: 0,
							},
							Action: &Action{
								Type:          1,
								Permission:    &Permission{Type: 2},
								Data:          detailData,
								UnsupportTips: "请升级 QQ 版本后使用",
							},
						},
					},
				},
				menuBackRow(),
			},
		},
	}
}

// HandleRedPacketCommand 处理发红包指令
func HandleRedPacketCommand(event *GroupAtMessageEvent, openID string) (content string, keyboard *Keyboard) {
	s := GetQQBotSetting()
	if !s.RedPacketEnabled {
		return buildPlainMarkdown(openID, "**红包功能未开启**"), nil
	}
	if IsRedPacketDisabledGroup(event.GroupOpenID) {
		return buildPlainMarkdown(openID, "**本群已关闭红包**"), nil
	}

	userId, bound := identity.IsQQBound(openID)
	if !bound {
		return buildPlainMarkdown(openID,
			"**发红包失败！**\n\n请先绑定站点账号：登陆后在 个人资料 → QQ 绑定验证码 获取验证码"), nil
	}

	amountUnits, count, blessing := parseRedPacketArgs(event.Content)
	if amountUnits <= 0 {
		return buildPlainMarkdown(openID, fmt.Sprintf(
			"**用法：**/红包 金额 个数\n\n例如：/红包 10 5 表示 10%s 分给 5 个人\n\n单个红包最低 %s%s",
			currencySymbolOrEmpty(),
			trimFloat(quotaToUnits(s.RedPacketMinAmount)), currencySymbolOrEmpty())), nil
	}
	if count <= 0 {
		count = s.RedPacketDefaultCount
		if count <= 0 {
			count = 5
		}
	}
	if count > s.RedPacketMaxCount && s.RedPacketMaxCount > 0 {
		return buildPlainMarkdown(openID, fmt.Sprintf(
			"**发红包失败！**\n\n单个红包最多分 %d 份", s.RedPacketMaxCount)), nil
	}

	amount := unitsToQuota(amountUnits)
	if amount < s.RedPacketMinAmount {
		return buildPlainMarkdown(openID, fmt.Sprintf(
			"**发红包失败！**\n\n红包总额最低 %s%s",
			trimFloat(quotaToUnits(s.RedPacketMinAmount)), currencySymbolOrEmpty())), nil
	}
	if s.RedPacketMaxAmount > 0 && amount > s.RedPacketMaxAmount {
		return buildPlainMarkdown(openID, fmt.Sprintf(
			"**发红包失败！**\n\n红包总额上限 %s%s",
			trimFloat(quotaToUnits(s.RedPacketMaxAmount)), currencySymbolOrEmpty())), nil
	}

	packet, err := CreateQQRedPacket(&QQRedPacketParams{
		SenderUserId:  userId,
		SenderOpenID:  openID,
		GroupOpenID:   event.GroupOpenID,
		Blessing:      blessing,
		TotalAmount:   amount,
		TotalCount:    count,
		ExpireSeconds: s.RedPacketExpireSeconds,
		DailyLimit:    s.RedPacketDailyLimit,
	})
	if err != nil {
		return buildPlainMarkdown(openID,
			"**发红包失败！**\n\n"+redPacketErrorText(err, s.RedPacketDailyLimit)), nil
	}

	usage.RecordLog(userId, usage.LogTypeSystem, fmt.Sprintf(
		"QQ 群发红包 %s，共 %d 份",
		logger.LogQuota(packet.TotalAmount), packet.TotalCount))

	symbol := currencySymbolOrEmpty()
	if blessing == "" {
		blessing = "大家一起来分奶酪！"
	}

	var sb strings.Builder
	sb.WriteString("**")
	sb.WriteString(blessing)
	sb.WriteString("**\n\n")
	sb.WriteString(atUser(openID))
	sb.WriteString(fmt.Sprintf(" 发了一个 %s%s 的红包，共 %d 份\n\n",
		trimFloat(quotaToUnits(packet.TotalAmount)), symbol, packet.TotalCount))
	sb.WriteString("点下面的按钮来抢！")
	return sb.String(), redPacketKeyboard(packet.Id)
}

// HandleRedPacketGrab 处理抢红包按钮回调
func HandleRedPacketGrab(packetIDStr, openID string) string {
	packetID, err := strconv.Atoi(packetIDStr)
	if err != nil {
		return buildPlainMarkdown(openID, "**红包无效**")
	}

	userId, bound := identity.IsQQBound(openID)
	if !bound {
		return buildPlainMarkdown(openID,
			"**抢红包失败！**\n\n请先绑定站点账号才能领取")
	}

	s := GetQQBotSetting()
	grab, packet, err := GrabQQRedPacket(
		packetID, userId, openID, s.RedPacketAllowOwnGrab)
	if err != nil {
		return buildPlainMarkdown(openID,
			"**没抢到！**\n\n"+redPacketErrorText(err, s.RedPacketDailyLimit))
	}

	usage.RecordLog(userId, usage.LogTypeSystem,
		fmt.Sprintf("QQ 抢到红包 %s", logger.LogQuota(grab.Amount)))

	// 抢完时标记运气王，便于「查看战绩」展示
	if packet.RemainingCount <= 0 {
		if err := MarkLuckiestGrab(packetID); err != nil {
			common.SysError("标记红包运气王失败: " + err.Error())
		}
	}

	balance, qErr := identity.GetUserQuota(userId, true)
	if qErr != nil {
		common.SysError("抢红包后查询余额失败: " + qErr.Error())
	}

	symbol := currencySymbolOrEmpty()
	var sb strings.Builder
	sb.WriteString(atUser(openID))
	sb.WriteString(fmt.Sprintf(" 抢到了 **%s%s**！\n\n",
		trimFloat(quotaToUnits(grab.Amount)), symbol))
	if packet.RemainingCount > 0 {
		sb.WriteString(fmt.Sprintf("红包还剩 %d 份，手快有手慢无\n\n", packet.RemainingCount))
	} else {
		sb.WriteString("红包已被抢完\n\n")
	}
	sb.WriteString(fmt.Sprintf("你的余额 %s%s",
		trimFloat(quotaToUnits(balance)), symbol))
	return sb.String()
}

// HandleRedPacketGrabAll 一键领取本群全部可抢红包，只回复一条汇总。
//
// 逐个走与单抢相同的 GrabQQRedPacket（并发安全、幂等），
// 中途被别人抢完的按「被抢完」归类；全部结束后只查一次余额，
// 避免批量领取在群里刷屏。
func HandleRedPacketGrabAll(groupOpenID, openID string) string {
	userId, bound := identity.IsQQBound(openID)
	if !bound {
		return buildPlainMarkdown(openID,
			"**抢红包失败！**\n\n请先绑定站点账号才能领取")
	}

	packets, err := GetGroupRedPackets(groupOpenID, 10)
	if err != nil {
		return buildPlainMarkdown(openID, "**查询失败，请稍后重试**")
	}

	s := GetQQBotSetting()
	var (
		claimedCount  int
		claimedQuota  int
		alreadyCount  int
		finishedCount int
		ownCount      int
		failedCount   int
	)
	for i := range packets {
		p := packets[i]
		if p.Status != RedPacketStatusActive {
			continue
		}
		grab, packet, gErr := GrabQQRedPacket(
			p.Id, userId, openID, s.RedPacketAllowOwnGrab)
		switch {
		case gErr == nil:
			claimedCount++
			claimedQuota += grab.Amount
			usage.RecordLog(userId, usage.LogTypeSystem,
				fmt.Sprintf("QQ 抢到红包 %s（一键领取）", logger.LogQuota(grab.Amount)))
			// 与单抢路径保持一致：抢完时标记运气王
			if packet.RemainingCount <= 0 {
				if err := MarkLuckiestGrab(p.Id); err != nil {
					common.SysError("标记红包运气王失败: " + err.Error())
				}
			}
		case errors.Is(gErr, ErrRedPacketAlreadyGrabbed):
			alreadyCount++
		case errors.Is(gErr, ErrRedPacketFinished), errors.Is(gErr, ErrRedPacketExpired):
			finishedCount++
		case errors.Is(gErr, ErrRedPacketOwnGrab):
			ownCount++
		default:
			failedCount++
			common.SysError("一键领取红包失败: " + gErr.Error())
		}
	}

	if claimedCount == 0 && alreadyCount == 0 && ownCount == 0 &&
		finishedCount == 0 && failedCount == 0 {
		return buildPlainMarkdown(openID, "**本群暂时没有可领取的红包**")
	}

	balance, qErr := identity.GetUserQuota(userId, true)
	if qErr != nil {
		common.SysError("一键领取后查询余额失败: " + qErr.Error())
	}

	symbol := currencySymbolOrEmpty()
	var sb strings.Builder
	sb.WriteString(atUser(openID))
	if claimedCount > 0 {
		sb.WriteString(fmt.Sprintf(" 一键领取 **%d** 个红包，共 **%s%s**！\n\n",
			claimedCount, trimFloat(quotaToUnits(claimedQuota)), symbol))
	} else {
		sb.WriteString(" 这次没能领取到红包\n\n")
	}
	var skips []string
	if alreadyCount > 0 {
		skips = append(skips, fmt.Sprintf("已抢过 %d 个", alreadyCount))
	}
	if finishedCount > 0 {
		skips = append(skips, fmt.Sprintf("被抢完 %d 个", finishedCount))
	}
	if ownCount > 0 {
		skips = append(skips, fmt.Sprintf("自己发的 %d 个", ownCount))
	}
	if failedCount > 0 {
		skips = append(skips, fmt.Sprintf("失败 %d 个", failedCount))
	}
	if len(skips) > 0 {
		sb.WriteString(strings.Join(skips, " · "))
		sb.WriteString("\n\n")
	}
	sb.WriteString(fmt.Sprintf("你的余额 %s%s",
		trimFloat(quotaToUnits(balance)), symbol))
	return sb.String()
}

// HandleRedPacketDetail 处理「查看战绩」按钮回调
func HandleRedPacketDetail(packetIDStr, openID string) string {
	packetID, err := strconv.Atoi(packetIDStr)
	if err != nil {
		return buildPlainMarkdown(openID, "**红包无效**")
	}
	packet, err := GetQQRedPacket(packetID)
	if err != nil {
		return buildPlainMarkdown(openID, "**红包不存在**")
	}
	grabs, err := GetQQRedPacketGrabs(packetID)
	if err != nil {
		return buildPlainMarkdown(openID, "**查询失败，请稍后重试**")
	}

	symbol := currencySymbolOrEmpty()
	var sb strings.Builder
	sb.WriteString("**红包战绩**\n\n")
	sb.WriteString(fmt.Sprintf("总额 %s%s，共 %d 份，已领 %d 份\n\n",
		trimFloat(quotaToUnits(packet.TotalAmount)), symbol,
		packet.TotalCount, len(grabs)))

	if len(grabs) == 0 {
		sb.WriteString("还没有人抢，快来当第一个！")
		return sb.String()
	}

	// 只展示前 10 条，避免消息过长被平台截断
	limit := len(grabs)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		g := grabs[i]
		line := fmt.Sprintf("%d. %s %s%s",
			i+1, atUser(g.OpenID), trimFloat(quotaToUnits(g.Amount)), symbol)
		if g.IsLuckic {
			line += " 运气王"
		}
		sb.WriteString(line)
		sb.WriteString("\n\n")
	}
	if len(grabs) > limit {
		sb.WriteString(fmt.Sprintf("……还有 %d 人", len(grabs)-limit))
	}
	return sb.String()
}

// redPacketErrorText 把底层错误翻译成群里能看懂的话
func redPacketErrorText(err error, dailyLimit int) string {
	switch {
	case errors.Is(err, ErrRedPacketDailyLimit):
		return fmt.Sprintf("你今天发红包的次数已用完（每天 %d 次）", dailyLimit)
	case errors.Is(err, ErrRedPacketInsufficient):
		return "余额不足"
	case errors.Is(err, ErrRedPacketAlreadyGrabbed):
		return "你已经抢过这个红包了"
	case errors.Is(err, ErrRedPacketFinished):
		return "红包已经被抢完了"
	case errors.Is(err, ErrRedPacketExpired):
		return "红包已过期，未领取的部分已退回发送者"
	case errors.Is(err, ErrRedPacketOwnGrab):
		return "不能抢自己发的红包"
	case errors.Is(err, ErrRedPacketNotFound):
		return "红包不存在"
	default:
		common.SysError("QQ 红包操作失败: " + err.Error())
		return err.Error()
	}
}

// redPacketStatusText 红包状态的中文文案
func redPacketStatusText(p *QQRedPacket) string {
	switch p.Status {
	case RedPacketStatusFinished:
		return "已抢完"
	case RedPacketStatusExpired:
		return "已过期"
	default:
		return "进行中"
	}
}

// HandleRedPacketList 构造本群红包列表文案与按钮。
//
// 列表分「可领取」「已领取完」两段（已过期也归入后者，行内标明状态）。
// 可领取段保留单抢按钮（最多 4 个，为一键领取按钮腾键盘行数），
// 并追加一键领取按钮：一次回调批量领取全部可抢红包，只回复一条汇总。
func HandleRedPacketList(groupOpenID string) (string, *Keyboard) {
	packets, err := GetGroupRedPackets(groupOpenID, 10)
	if err != nil {
		return buildPlainMarkdown("", "**查询失败，请稍后重试**"), redPacketMenuKeyboard()
	}
	if len(packets) == 0 {
		return buildPlainMarkdown("",
			"**本群还没有红包**\n\n用 /红包 金额 份数 发一个吧"), redPacketMenuKeyboard()
	}

	var claimable, done []QQRedPacket
	for _, p := range packets {
		if p.Status == RedPacketStatusActive {
			claimable = append(claimable, p)
		} else {
			done = append(done, p)
		}
	}

	symbol := currencySymbolOrEmpty()
	var sb strings.Builder
	sb.WriteString("**本群红包列表**\n\n")

	var rows []Row
	grabRows := 0
	num := 0
	if len(claimable) > 0 {
		sb.WriteString("**可领取**\n\n")
		for _, p := range claimable {
			num++
			grabbed := p.TotalCount - p.RemainingCount
			sb.WriteString(fmt.Sprintf("%d. %s 发 %s%s · %d/%d 份\n\n",
				num, atUser(p.SenderOpenID),
				trimFloat(quotaToUnits(p.TotalAmount)), symbol,
				grabbed, p.TotalCount))
			if grabRows < 4 {
				rows = append(rows, Row{Buttons: []Button{callbackBtn(
					"抢红包",
					ButtonDataRedPacketGrab+strconv.Itoa(p.Id), 1)}})
				grabRows++
			}
		}
		rows = append(rows, Row{Buttons: []Button{callbackBtn(
			"一键领取全部", ButtonDataRedPacketGrabAll, 1)}})
	}
	if len(done) > 0 {
		sb.WriteString("**已领取完**\n\n")
		for _, p := range done {
			num++
			grabbed := p.TotalCount - p.RemainingCount
			sb.WriteString(fmt.Sprintf("%d. %s 发 %s%s · %d/%d 份 · %s\n\n",
				num, atUser(p.SenderOpenID),
				trimFloat(quotaToUnits(p.TotalAmount)), symbol,
				grabbed, p.TotalCount, redPacketStatusText(&p)))
		}
	}
	rows = append(rows, menuBackRow())
	return sb.String(), &Keyboard{Content: &KeyboardContent{Rows: rows}}
}
