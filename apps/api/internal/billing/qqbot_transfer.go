package billing

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/usage"
)

// 转账指令。用法：/转账 @某人 1.5
// 参数顺序不敏感，@ 与金额可以互换位置。
const (
	CmdTransfer     = "/转账"
	CmdTransferInfo = "/转账费率"
)

// transferCommandAliases 转账指令的可接受写法
var transferCommandAliases = []string{"/转账", "转账", "/打钱", "/给钱"}

// FeeBracket 累进手续费档位
//
// UpTo 为该档位的上界（以显示货币为单位，例如 🧀），Rate 为该档内的费率。
// 计税方式与个人所得税一致：只有超出下界的那部分按本档费率计费，
// 而不是整笔按最高档费率计——否则会出现「多转 0.01 反而到账更少」的断崖。
type FeeBracket struct {
	UpTo float64 `json:"up_to"` // <=0 表示无上界（最高档）
	Rate float64 `json:"rate"`  // 0.03 表示 3%
}

// defaultFeeBrackets 默认累进费率表
//
// 起征档 3% 对应用户要求的基础手续费，之后逐档递增，
// 大额转账的边际费率最高 18%，抑制刷号搬砖式的额度集中。
var defaultFeeBrackets = []FeeBracket{
	{UpTo: 1, Rate: 0.03},
	{UpTo: 5, Rate: 0.05},
	{UpTo: 20, Rate: 0.08},
	{UpTo: 100, Rate: 0.12},
	{UpTo: 0, Rate: 0.18},
}

// feeBrackets 返回当前生效的费率表
// 后台配置解析失败时回落到默认表，避免因为一个错字让转账整体不可用
func feeBrackets() []FeeBracket {
	raw := strings.TrimSpace(GetQQBotSetting().TransferFeeBrackets)
	if raw == "" {
		return defaultFeeBrackets
	}
	var brackets []FeeBracket
	if err := json.Unmarshal([]byte(raw), &brackets); err != nil || len(brackets) == 0 {
		common.SysError("转账费率表解析失败，使用默认档位: " + raw)
		return defaultFeeBrackets
	}
	for _, b := range brackets {
		if b.Rate < 0 || b.Rate >= 1 {
			common.SysError("转账费率表含非法费率，使用默认档位")
			return defaultFeeBrackets
		}
	}
	return brackets
}

// quotaToUnits 把内部额度换算成显示货币数值
func quotaToUnits(quota int) float64 {
	usd := float64(quota) / common.QuotaPerUnit
	return usd * GetUsdToCurrencyRate(USDExchangeRate)
}

// unitsToQuota 把显示货币数值换算成内部额度
func unitsToQuota(units float64) int {
	rate := GetUsdToCurrencyRate(USDExchangeRate)
	if rate <= 0 {
		rate = 1
	}
	return int(math.Round(units / rate * common.QuotaPerUnit))
}

// CalcTransferFee 按累进档位计算手续费（返回内部额度单位）
//
// 手续费至少为 1，保证任何一笔成功的转账都产生非零成本，
// 否则拆成大量小额转账就能零成本搬运额度。
func CalcTransferFee(amount int) int {
	if amount <= 0 {
		return 0
	}
	units := quotaToUnits(amount)

	var fee float64
	lower := 0.0
	for _, b := range feeBrackets() {
		if b.UpTo <= 0 || units <= b.UpTo {
			// 落在本档（或本档为最高档）：剩余部分全部按本档费率
			fee += (units - lower) * b.Rate
			lower = units
			break
		}
		fee += (b.UpTo - lower) * b.Rate
		lower = b.UpTo
	}
	// 配置表最高档带上界且金额超出时，超出部分按最后一档费率兜底
	if lower < units {
		brackets := feeBrackets()
		fee += (units - lower) * brackets[len(brackets)-1].Rate
	}

	feeQuota := unitsToQuota(fee)
	if feeQuota < 1 {
		feeQuota = 1
	}
	// 手续费不能吞掉全部本金，至少留 1 给收款方
	if feeQuota >= amount {
		feeQuota = amount - 1
	}
	return feeQuota
}

// effectiveFeeRate 整笔的实际综合费率，用于回执展示
func effectiveFeeRate(amount, fee int) float64 {
	if amount <= 0 {
		return 0
	}
	return float64(fee) / float64(amount) * 100
}

// isTransferCommand 判断消息是否为转账指令
func isTransferCommand(content string) bool {
	text := strings.TrimSpace(stripTags(content))
	for _, alias := range transferCommandAliases {
		if text == alias || strings.HasPrefix(text, alias+" ") {
			return true
		}
	}
	return false
}

// isTransferInfoCommand 判断是否为费率查询指令
func isTransferInfoCommand(content string) bool {
	return strings.TrimSpace(stripTags(content)) == CmdTransferInfo
}

// parseTransferAmount 从指令文本里解析转账金额（显示货币单位）
//
// 去掉标签后按空白切分，取第一个能解析成正数的 token。
// 允许用户写「1.5」「1.5🧀」「￥1.5」这类带修饰的写法。
func parseTransferAmount(content string) (float64, error) {
	text := stripTags(content)
	for _, alias := range transferCommandAliases {
		text = strings.ReplaceAll(text, alias, " ")
	}

	for _, token := range strings.Fields(text) {
		// 剥掉数字两侧的货币符号与标点
		cleaned := strings.TrimFunc(token, func(r rune) bool {
			return !(r >= '0' && r <= '9') && r != '.'
		})
		if cleaned == "" {
			continue
		}
		value, err := strconv.ParseFloat(cleaned, 64)
		if err != nil || value <= 0 {
			continue
		}
		return value, nil
	}
	return 0, fmt.Errorf("未识别到转账金额")
}

// pickTransferTarget 从 mentions 里挑出收款人
//
// mentions 会包含被 @ 的机器人自己，也可能包含发送者本人（自问自答场景）。
// 这里的筛选规则：跳过发送者，跳过未绑定站点账号的 openid（机器人必然未绑定），
// 取第一个满足条件的 openid。这样不需要单独维护机器人 openid 的配置。
func pickTransferTarget(mentions []Mention, senderOpenID string) (openID string, userId int, ok bool) {
	for _, m := range mentions {
		if m.ID == "" || m.ID == senderOpenID {
			continue
		}
		if m.Bot {
			continue
		}
		if uid, bound := identity.IsQQBound(m.ID); bound {
			return m.ID, uid, true
		}
	}
	return "", 0, false
}

// hasNonSelfMention 是否存在除发送者与机器人以外的 @ 对象
// 用于区分「没 @ 任何人」与「@ 的人没绑定账号」两种错误
func hasNonSelfMention(mentions []Mention, senderOpenID string) bool {
	for _, m := range mentions {
		if m.ID != "" && m.ID != senderOpenID && !m.Bot {
			return true
		}
	}
	return false
}

// transferFeeTableText 渲染费率表，用于 /转账费率 与错误提示
func transferFeeTableText() string {
	symbol := currencySymbolOrEmpty()
	var sb strings.Builder
	lower := 0.0
	for _, b := range feeBrackets() {
		if b.UpTo <= 0 {
			sb.WriteString(fmt.Sprintf("超过 %s%s 的部分：%.0f%%\n\n",
				trimFloat(lower), symbol, b.Rate*100))
			break
		}
		sb.WriteString(fmt.Sprintf("%s%s - %s%s 的部分：%.0f%%\n\n",
			trimFloat(lower), symbol, trimFloat(b.UpTo), symbol, b.Rate*100))
		lower = b.UpTo
	}
	return sb.String()
}

// currencySymbolOrEmpty TOKENS 显示模式下没有货币符号
func currencySymbolOrEmpty() string {
	if GetQuotaDisplayType() == QuotaDisplayTypeTokens {
		return ""
	}
	return GetCurrencySymbol()
}

// trimFloat 去掉浮点数多余的尾随零
func trimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 4, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		s = "0"
	}
	return s
}

// HandleTransferInfo 回复费率说明
func HandleTransferInfo(openID string) string {
	s := GetQQBotSetting()
	var sb strings.Builder
	sb.WriteString(atUser(openID))
	sb.WriteString(" **转账费率说明**\n\n")
	sb.WriteString("用法：/转账 @某人 金额\n\n")
	sb.WriteString(transferFeeTableText())
	sb.WriteString(fmt.Sprintf("手续费按段累进计算，收发双方每人每天各限 %d 次\n\n", s.TransferDailyLimit))
	sb.WriteString(fmt.Sprintf("单笔最低 %s%s",
		trimFloat(quotaToUnits(s.TransferMinAmount)), currencySymbolOrEmpty()))
	return sb.String()
}

// HandleTransferCommand 处理转账指令，返回要回复的 markdown
func HandleTransferCommand(event *GroupAtMessageEvent, senderOpenID string) string {
	s := GetQQBotSetting()
	if !s.TransferEnabled {
		return buildPlainMarkdown(senderOpenID, "**转账功能未开启**")
	}
	if IsTransferDisabledGroup(event.GroupOpenID) {
		return buildPlainMarkdown(senderOpenID, "**本群已关闭转账**")
	}

	fromUserId, bound := identity.IsQQBound(senderOpenID)
	if !bound {
		return buildPlainMarkdown(senderOpenID,
			"**转账失败！**\n\n请先绑定站点账号：登陆后在 个人资料→每日签到→QQ签到 获取验证码")
	}

	// 收款人
	toOpenID, toUserId, ok := pickTransferTarget(event.Mentions, senderOpenID)
	if !ok {
		if hasNonSelfMention(event.Mentions, senderOpenID) {
			return buildPlainMarkdown(senderOpenID,
				"**转账失败！**\n\n对方还没有绑定站点账号，无法接收转账")
		}
		return buildPlainMarkdown(senderOpenID,
			"**转账失败！**\n\n请 @ 要转账的对象，例如：/转账 @某人 1.5")
	}
	if toUserId == fromUserId {
		return buildPlainMarkdown(senderOpenID, "**转账失败！**\n\n不能转账给自己")
	}

	// 金额
	units, err := parseTransferAmount(event.Content)
	if err != nil {
		return buildPlainMarkdown(senderOpenID,
			"**转账失败！**\n\n请写明金额，例如：/转账 @某人 1.5")
	}
	amount := unitsToQuota(units)
	if amount < s.TransferMinAmount {
		return buildPlainMarkdown(senderOpenID, fmt.Sprintf(
			"**转账失败！**\n\n单笔最低 %s%s",
			trimFloat(quotaToUnits(s.TransferMinAmount)), currencySymbolOrEmpty()))
	}
	if s.TransferMaxAmount > 0 && amount > s.TransferMaxAmount {
		return buildPlainMarkdown(senderOpenID, fmt.Sprintf(
			"**转账失败！**\n\n单笔上限 %s%s",
			trimFloat(quotaToUnits(s.TransferMaxAmount)), currencySymbolOrEmpty()))
	}

	fee := CalcTransferFee(amount)
	transfer, err := DoQQTransfer(&QQTransferParams{
		FromUserId:  fromUserId,
		ToUserId:    toUserId,
		FromOpenID:  senderOpenID,
		ToOpenID:    toOpenID,
		GroupOpenID: event.GroupOpenID,
		Amount:      amount,
		Fee:         fee,
		DailyLimit:  s.TransferDailyLimit,
	})
	if err != nil {
		return buildPlainMarkdown(senderOpenID, "**转账失败！**\n\n"+transferErrorText(err, s.TransferDailyLimit))
	}

	// 双方都记一条流水，便于对账
	usage.RecordLog(fromUserId, usage.LogTypeSystem, fmt.Sprintf(
		"QQ 转账支出 %s（手续费 %s），实际到账 %s",
		logger.LogQuota(transfer.Amount), logger.LogQuota(transfer.Fee),
		logger.LogQuota(transfer.Received)))
	usage.RecordLog(toUserId, usage.LogTypeSystem, fmt.Sprintf(
		"QQ 转账收入 %s", logger.LogQuota(transfer.Received)))

	fromBalance, qErr := identity.GetUserQuota(fromUserId, true)
	if qErr != nil {
		common.SysError("转账后查询余额失败: " + qErr.Error())
	}

	symbol := currencySymbolOrEmpty()
	var sb strings.Builder
	sb.WriteString(atUser(senderOpenID))
	sb.WriteString(" **转账成功！**\n\n")
	sb.WriteString(fmt.Sprintf("转出 %s%s，手续费 %s%s（%.1f%%）\n\n",
		trimFloat(quotaToUnits(transfer.Amount)), symbol,
		trimFloat(quotaToUnits(transfer.Fee)), symbol,
		effectiveFeeRate(transfer.Amount, transfer.Fee)))
	sb.WriteString(fmt.Sprintf("%s 实际到账 %s%s\n\n",
		atUser(toOpenID), trimFloat(quotaToUnits(transfer.Received)), symbol))
	sb.WriteString(fmt.Sprintf("你的余额 %s%s",
		trimFloat(quotaToUnits(fromBalance)), symbol))
	return sb.String()
}

// transferErrorText 把底层错误翻译成群里能看懂的话
func transferErrorText(err error, dailyLimit int) string {
	switch {
	case errors.Is(err, ErrTransferLimitSelf):
		return fmt.Sprintf("你今天的转账次数已用完（每人每天 %d 次，收发都算）", dailyLimit)
	case errors.Is(err, ErrTransferLimitPeer):
		return fmt.Sprintf("对方今天的转账次数已用完（每人每天 %d 次，收发都算）", dailyLimit)
	case errors.Is(err, ErrTransferInsufficient):
		return "余额不足"
	case errors.Is(err, ErrTransferSelf):
		return "不能转账给自己"
	default:
		common.SysError("QQ 转账失败: " + err.Error())
		return "系统繁忙，请稍后重试"
	}
}
