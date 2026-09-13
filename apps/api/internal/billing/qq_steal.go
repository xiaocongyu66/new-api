package billing

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/common/quotacache"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/usage"
	"gorm.io/gorm"
)

// QQSteal 偷奶酪尝试的审计记录。
// 与转账分表：偷是「概率判定 + 可能不发生额度流动」的行为，
// 失败的掷骰也要留痕（每日次数按尝试计，含失败），混进 qq_transfers 会破坏其记账语义。
type QQSteal struct {
	Id int `json:"id" gorm:"primaryKey;autoIncrement"`

	ThiefUserId  int    `json:"thief_user_id" gorm:"not null;index:idx_qq_steal_thief_date,priority:1"`
	VictimUserId int    `json:"victim_user_id" gorm:"not null;index:idx_qq_steal_victim_date,priority:1"`
	StealDate    string `json:"steal_date" gorm:"type:varchar(10);not null;index:idx_qq_steal_thief_date,priority:2;index:idx_qq_steal_victim_date,priority:2"`

	// Amount 实际搬运的额度（内部单位），失败为 0
	Amount int `json:"amount" gorm:"not null"`
	// Success 掷骰与余额判定综合结果
	Success bool `json:"success" gorm:"not null"`
	// Reason 失败原因文案，成功时为空
	Reason string `json:"reason" gorm:"type:varchar(64)"`

	ThiefOpenID  string `json:"thief_open_id" gorm:"type:varchar(128)"`
	VictimOpenID string `json:"victim_open_id" gorm:"type:varchar(128)"`
	GroupOpenID  string `json:"group_open_id" gorm:"type:varchar(128)"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;index"`
}

func (QQSteal) TableName() string {
	return "qq_steals"
}

var (
	// ErrStealLimit 发起方今日尝试次数已用完（失败也计入）
	ErrStealLimit = errors.New("今日的偷奶酪次数已用完")
	// ErrStealSelf 不能偷自己
	ErrStealSelf = errors.New("不能偷自己")
)

// 掷骰与门控失败的文案，写入 qq_steals.reason 并直接回复到群里
const (
	stealFailLucky = "手风不好，什么都没偷到"
	stealFailGrace = "对方刚被偷过，防守正严"
	stealFailBroke = "对方余额不足，无从下手"
)

// QQStealParams 偷奶酪入参
type QQStealParams struct {
	ThiefUserId  int
	VictimUserId int
	ThiefOpenID  string
	VictimOpenID string
	GroupOpenID  string

	// Amount 计划偷取的额度（内部单位），必须为正数
	Amount int
	// SuccessRate 掷骰成功率百分比，0-100 之外按边界钳制
	SuccessRate int
	// DailyLimit 每人每日尝试次数上限（失败也计），<=0 为不限
	DailyLimit int
	// GraceSeconds 得手后受害者不能再被偷的冷却窗口（秒），<=0 为关闭
	GraceSeconds int
}

// DoQQSteal 执行一次偷奶酪。
//
// 返回值语义：
//   - (record, nil)：尝试已完成并落审计表，是否得手看 record.Success / record.Reason
//   - (nil, error)：尝试本身不能成立（偷自己、当日次数用完），不产生记录
//
// 全流程与转账同构：事务路径把「次数校验、冷却判定、扣加额度、落记录」打包，
// SQLite 无事务路径按「先扣后加」排序，中途失败回滚，绝不凭空增发。
func DoQQSteal(p *QQStealParams) (*QQSteal, error) {
	if p.ThiefUserId == p.VictimUserId {
		return nil, ErrStealSelf
	}
	if p.Amount <= 0 {
		return nil, errors.New("偷取额度必须为正数")
	}

	steal := &QQSteal{
		ThiefUserId:  p.ThiefUserId,
		VictimUserId: p.VictimUserId,
		StealDate:    time.Now().Format("2006-01-02"),
		ThiefOpenID:  p.ThiefOpenID,
		VictimOpenID: p.VictimOpenID,
		GroupOpenID:  p.GroupOpenID,
		CreatedAt:    time.Now().Unix(),
	}

	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return stealWithoutTransaction(steal, p)
	}
	return stealWithTransaction(steal, p)
}

func stealRollWon(successRate int) bool {
	if successRate <= 0 {
		return false
	}
	if successRate >= 100 {
		return true
	}
	return rand.IntN(100) < successRate
}

// countStealsToday 事务内统计某 thief 今日尝试次数（含失败），避免并发刷穿上限
func countStealsToday(q *gorm.DB, thiefUserId int, date string) (int, error) {
	var count int64
	err := q.Model(&QQSteal{}).
		Where("thief_user_id = ? AND steal_date = ?", thiefUserId, date).
		Count(&count).Error
	return int(count), err
}

// victimInStealGrace 受害者最近 graceSeconds 秒内是否被偷成功过
func victimInStealGrace(q *gorm.DB, victimUserId, graceSeconds int) (bool, error) {
	if graceSeconds <= 0 {
		return false, nil
	}
	var lastAt int64
	err := q.Model(&QQSteal{}).
		Where("victim_user_id = ? AND success = ? AND created_at > ?",
			victimUserId, true, time.Now().Unix()-int64(graceSeconds)).
		Select("COALESCE(MAX(created_at), 0)").Scan(&lastAt).Error
	return lastAt > 0, err
}

// stealWithTransaction MySQL / PostgreSQL 事务路径
func stealWithTransaction(steal *QQSteal, p *QQStealParams) (*QQSteal, error) {
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		if p.DailyLimit > 0 {
			count, err := countStealsToday(tx, p.ThiefUserId, steal.StealDate)
			if err != nil {
				return err
			}
			if count >= p.DailyLimit {
				return ErrStealLimit
			}
		}

		switch {
		case p.GraceSeconds > 0:
			inGrace, err := victimInStealGrace(tx, p.VictimUserId, p.GraceSeconds)
			if err != nil {
				return err
			}
			if inGrace {
				steal.Reason = stealFailGrace
				return tx.Create(steal).Error
			}
			fallthrough
		default:
			if !stealRollWon(p.SuccessRate) {
				steal.Reason = stealFailLucky
				return tx.Create(steal).Error
			}
			// 条件更新 + RowsAffected：把余额检查与扣减做成一个原子操作，
			// 先查余额再扣会留下竞态窗口，可能把受害者扣成负数。
			res := tx.Model(&identity.User{}).
				Where("id = ? AND quota >= ?", p.VictimUserId, p.Amount).
				Update("quota", gorm.Expr("quota - ?", p.Amount))
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				steal.Reason = stealFailBroke
				return tx.Create(steal).Error
			}
			if err := tx.Model(&identity.User{}).Where("id = ?", p.ThiefUserId).
				Update("quota", gorm.Expr("quota + ?", p.Amount)).Error; err != nil {
				return err
			}
			steal.Success = true
			steal.Amount = p.Amount
			return tx.Create(steal).Error
		}
	})
	if err != nil {
		return nil, err
	}

	if steal.Success {
		go func() {
			_ = quotacache.DecrUser(p.VictimUserId, int64(p.Amount))
			_ = quotacache.IncrUser(p.ThiefUserId, int64(p.Amount))
		}()
	}
	return steal, nil
}

// stealWithoutTransaction SQLite 路径
//
// 扣减本身是单条条件更新语句，三种数据库里都原子；缺的只是跨语句事务，
// 因此按「先扣后加」排序，中途失败把已扣的退回受害者，宁可回滚也不凭空增发。
func stealWithoutTransaction(steal *QQSteal, p *QQStealParams) (*QQSteal, error) {
	if p.DailyLimit > 0 {
		count, err := countStealsToday(dbx.DB, p.ThiefUserId, steal.StealDate)
		if err != nil {
			return nil, err
		}
		if count >= p.DailyLimit {
			return nil, ErrStealLimit
		}
	}

	inGrace, err := victimInStealGrace(dbx.DB, p.VictimUserId, p.GraceSeconds)
	if err != nil {
		return nil, err
	}
	switch {
	case inGrace:
		steal.Reason = stealFailGrace
	case !stealRollWon(p.SuccessRate):
		steal.Reason = stealFailLucky
	default:
		res := dbx.DB.Model(&identity.User{}).
			Where("id = ? AND quota >= ?", p.VictimUserId, p.Amount).
			Update("quota", gorm.Expr("quota - ?", p.Amount))
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 0 {
			steal.Reason = stealFailBroke
			break
		}
		if err := identity.IncreaseUserQuota(p.ThiefUserId, p.Amount, true); err != nil {
			_ = identity.IncreaseUserQuota(p.VictimUserId, p.Amount, true)
			return nil, err
		}
		steal.Success = true
		steal.Amount = p.Amount
	}

	if err := dbx.DB.Create(steal).Error; err != nil {
		// 记录写不进去就把额度还原，避免出现无凭证的额度流动
		if steal.Success {
			_ = identity.DecreaseUserQuota(p.ThiefUserId, p.Amount, true)
			_ = identity.IncreaseUserQuota(p.VictimUserId, p.Amount, true)
		}
		return nil, err
	}

	if steal.Success {
		_ = quotacache.DecrUser(p.VictimUserId, int64(p.Amount))
		_ = quotacache.IncrUser(p.ThiefUserId, int64(p.Amount))
	}
	return steal, nil
}

// ─── 群指令 ────────────────────────────────────────────────────────────────

// 偷奶酪指令。用法：偷奶酪 @某人 或 偷奶酪 <数量> @某人（数量以显示货币为单位）。
// 裸动词形式，与签到/红包别名同款：不带斜杠前缀也能触发。
// stealAliases 可接受写法
var stealAliases = []string{"/偷奶酪", "偷奶酪"}

// isStealCommand 判断消息是否为偷奶酪指令
func isStealCommand(content string) bool {
	text := strings.TrimSpace(stripTags(content))
	for _, alias := range stealAliases {
		if text == alias || strings.HasPrefix(text, alias+" ") {
			return true
		}
	}
	return false
}

// parseStealAmount 解析可选的数量参数（显示货币单位）。未给出时返回 false。
func parseStealAmount(content string) (float64, bool) {
	text := stripTags(content)
	for _, alias := range stealAliases {
		text = strings.ReplaceAll(text, alias, " ")
	}
	return firstPositiveNumber(text)
}

// HandleStealCommand 处理偷奶酪指令，返回要回复的 markdown。
// 成功与失败都在同一条回复里说明；成功消息同时 @ 受害者，起到双向通知。
func HandleStealCommand(event *GroupAtMessageEvent, senderOpenID string) string {
	s := GetQQBotSetting()
	if !s.StealEnabled {
		return buildPlainMarkdown(senderOpenID, "**偷奶酪功能未开启**")
	}

	thiefUserId, bound := identity.IsQQBound(senderOpenID)
	if !bound {
		return buildPlainMarkdown(senderOpenID,
			"**偷奶酪失败！**\n\n请先绑定站点账号：登陆后在 个人资料→每日签到→QQ签到 获取验证码")
	}

	victimOpenID, victimUserId, ok := pickTransferTarget(event.Mentions, senderOpenID)
	if !ok {
		if hasNonSelfMention(event.Mentions, senderOpenID) {
			return buildPlainMarkdown(senderOpenID,
				"**偷奶酪失败！**\n\n对方还没有绑定站点账号，无从下手")
		}
		return buildPlainMarkdown(senderOpenID, fmt.Sprintf(
			"**用法：**偷奶酪 @某人 或 偷奶酪 数量 @某人\n\n数量 %s-%s%s，不填则随机",
			trimFloat(s.StealMinAmount), trimFloat(s.StealMaxAmount), currencySymbolOrEmpty()))
	}
	if victimUserId == thiefUserId {
		return buildPlainMarkdown(senderOpenID, "**偷奶酪失败！**\n\n不能偷自己")
	}

	minQuota := unitsToQuota(s.StealMinAmount)
	if minQuota < 1 {
		minQuota = 1
	}
	maxQuota := unitsToQuota(s.StealMaxAmount)
	if maxQuota < minQuota {
		maxQuota = minQuota
	}

	amount := randomDropQuota(minQuota, maxQuota)
	if units, has := parseStealAmount(event.Content); has {
		amount = unitsToQuota(units)
		// 显式指定的数量同样受区间约束：上限是防刷配置，不能静默突破
		if amount < minQuota {
			return buildPlainMarkdown(senderOpenID, fmt.Sprintf(
				"**偷奶酪失败！**\n\n单次最少 %s%s",
				trimFloat(s.StealMinAmount), currencySymbolOrEmpty()))
		}
		if amount > maxQuota {
			return buildPlainMarkdown(senderOpenID, fmt.Sprintf(
				"**偷奶酪失败！**\n\n单次上限 %s%s",
				trimFloat(s.StealMaxAmount), currencySymbolOrEmpty()))
		}
	}

	steal, err := DoQQSteal(&QQStealParams{
		ThiefUserId:  thiefUserId,
		VictimUserId: victimUserId,
		ThiefOpenID:  senderOpenID,
		VictimOpenID: victimOpenID,
		GroupOpenID:  event.GroupOpenID,
		Amount:       amount,
		SuccessRate:  s.StealSuccessRate,
		DailyLimit:   s.StealDailyLimit,
		GraceSeconds: s.StealRecipientGraceSeconds,
	})
	if err != nil {
		return buildPlainMarkdown(senderOpenID,
			"**偷奶酪失败！**\n\n"+stealErrorText(err, s.StealDailyLimit))
	}
	if !steal.Success {
		return buildPlainMarkdown(senderOpenID, "**偷奶酪失败！**\n\n"+steal.Reason)
	}

	// 双方各记一条流水，便于对账
	usage.RecordLog(thiefUserId, usage.LogTypeSystem, fmt.Sprintf(
		"QQ 群偷奶酪得手 %s", logger.LogQuota(steal.Amount)))
	usage.RecordLog(victimUserId, usage.LogTypeSystem, fmt.Sprintf(
		"QQ 群被偷奶酪，损失 %s", logger.LogQuota(steal.Amount)))

	symbol := currencySymbolOrEmpty()
	thiefBalance, qErr := identity.GetUserQuota(thiefUserId, true)
	if qErr != nil {
		common.SysError("偷奶酪后查询余额失败: " + qErr.Error())
	}

	var sb strings.Builder
	sb.WriteString(atUser(senderOpenID))
	sb.WriteString(" **偷奶酪成功！**\n\n")
	sb.WriteString(fmt.Sprintf("你从 %s 那里偷到了 %s%s！\n\n",
		atUser(victimOpenID), trimFloat(quotaToUnits(steal.Amount)), symbol))
	sb.WriteString(fmt.Sprintf("%s 的奶酪被偷走了 %s%s，注意防守！\n\n",
		atUser(victimOpenID), trimFloat(quotaToUnits(steal.Amount)), symbol))
	sb.WriteString(fmt.Sprintf("你的余额 %s%s",
		trimFloat(quotaToUnits(thiefBalance)), symbol))
	return sb.String()
}

// stealErrorText 把门控错误翻译成群里能看懂的话
func stealErrorText(err error, dailyLimit int) string {
	switch {
	case errors.Is(err, ErrStealLimit):
		return fmt.Sprintf("你今天的偷奶酪次数已用完（每人每天 %d 次，失败也算）", dailyLimit)
	case errors.Is(err, ErrStealSelf):
		return "不能偷自己"
	default:
		common.SysError("QQ 偷奶酪失败: " + err.Error())
		return "系统繁忙，请稍后重试"
	}
}
