package billing

import (
	"errors"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/common/quotacache"
	"github.com/QuantumNous/new-api/internal/identity"
	"gorm.io/gorm"
)

// QQDrop 群内消息掉落奖励记录
// 与签到分表：掉落是高频、每人每天多次的行为，混进 qq_checkins
// 会破坏 (user_id, checkin_date) 唯一索引的语义。
type QQDrop struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId       int    `json:"user_id" gorm:"not null;index:idx_qq_drop_user_date,priority:1"`
	DropDate     string `json:"drop_date" gorm:"type:varchar(10);not null;index:idx_qq_drop_user_date,priority:2"`
	QuotaAwarded int    `json:"quota_awarded" gorm:"not null"`
	OpenID       string `json:"open_id" gorm:"type:varchar(128)"`
	GroupOpenID  string `json:"group_open_id" gorm:"type:varchar(128)"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;index"`
}

func (QQDrop) TableName() string {
	return "qq_drops"
}

// CountQQDropsToday 统计用户今日已领取的掉落次数
func CountQQDropsToday(userId int) (int, error) {
	today := time.Now().Format("2006-01-02")
	var count int64
	err := dbx.DB.Model(&QQDrop{}).
		Where("user_id = ? AND drop_date = ?", userId, today).
		Count(&count).Error
	return int(count), err
}

// GetUserQQDropRecords 查询用户在日期区间内的掉落记录
func GetUserQQDropRecords(userId int, startDate, endDate string) ([]QQDrop, error) {
	var records []QQDrop
	err := dbx.DB.Where("user_id = ? AND drop_date >= ? AND drop_date <= ?",
		userId, startDate, endDate).
		Order("created_at DESC").
		Find(&records).Error
	return records, err
}

// SumUserQQDropQuota 统计用户历史掉落总额度
func SumUserQQDropQuota(userId int) (int64, error) {
	var total int64
	err := dbx.DB.Model(&QQDrop{}).Where("user_id = ?", userId).
		Select("COALESCE(SUM(quota_awarded), 0)").Scan(&total).Error
	return total, err
}

// AwardQQDrop 为用户发放一次掉落奖励
// dailyLimit <= 0 表示不限制次数。
// 次数校验、近 7 日累计与写入放在同一事务里，避免同一用户并发消息把上限刷穿；
// 余额加权与每日保底也在同一事务内计算（见 computeDropAward，保底补差封顶在 7 日总额）。
func AwardQQDrop(userId int, openID, groupOpenID string, quota int, dailyLimit int) (*QQDrop, error) {
	if quota <= 0 {
		return nil, errors.New("掉落额度必须为正数")
	}

	drop := &QQDrop{
		UserId:       userId,
		DropDate:     time.Now().Format("2006-01-02"),
		QuotaAwarded: quota,
		OpenID:       openID,
		GroupOpenID:  groupOpenID,
		CreatedAt:    time.Now().Unix(),
	}

	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return awardQQDropWithoutTransaction(drop, userId, quota, dailyLimit)
	}
	return awardQQDropWithTransaction(drop, userId, quota, dailyLimit)
}

// computeDropAward 计算本次掉落的最终发放额度。
//
// 余额加权（anchor > 0）：w = clamp(anchor / balance, 0.5, 2.0)，
// amount = clamp(round(drawn * w), dropMin, dropMax)。余额低于锚点的人多拿、
// 高于锚点的人少拿，把全员余额向锚点收敛；权重上下限保证单场放大/缩减有界。
// balance 为 0（查询失败或用户不存在）时跳过加权，按原摇点值发放。
//
// 每日保底（guarantee > 0 且本次是当日最后一次可领机会，即 dailyLimit > 0
// 且 todayCount+1 >= dailyLimit）：若近 7 日（含当日）累计 weekSum 加上本次
// 发放仍低于 guarantee，则补足差额，使 7 日累计恰好拿满保底；7 日内已拿满的
// 不再补——否则「每天领 2 次不领第 3 次」就能把保底刷成日收益。
// 补足后的单发不受 dropMax 约束（保底语义优先于单发上限）；
// dailyLimit <= 0 时不存在「最后一次」，保底不生效。
func computeDropAward(drawn, balance, todayCount int, weekSum int64,
	dailyLimit, anchor, guarantee, dropMin, dropMax int) int {
	amount := drawn
	if anchor > 0 && balance > 0 {
		w := float64(anchor) / float64(balance)
		if w < 0.5 {
			w = 0.5
		}
		if w > 2.0 {
			w = 2.0
		}
		amount = common.QuotaRound(float64(drawn) * w)
		if dropMin > 0 && amount < dropMin {
			amount = dropMin
		}
		if dropMax > 0 && amount > dropMax {
			amount = dropMax
		}
	}
	if amount < 1 {
		amount = 1
	}
	if guarantee > 0 && dailyLimit > 0 && todayCount+1 >= dailyLimit {
		// 保底补差封顶在 7 日总额上：7 天内已拿到保底额的人不再补，
		// 防止「领 2 次不领第 3 次」的玩法把保底变成日刷收益。
		// 最终值 ≤ max(guarantee, weekSum) 有界，不会溢出。
		if deficit := int64(guarantee) - weekSum - int64(amount); deficit > 0 {
			amount += int(deficit)
		}
	}
	return amount
}

// queryDropDayStats 统计用户当日已领取的掉落次数与近 7 日（含当日）领取总额。
// 7 日总额是保底补差的封顶：防止「每天领 2 次不领第 3 次」把保底刷成日收益。
// q 既接受事务句柄也接受 dbx.DB（SQLite 无事务路径）。
func queryDropDayStats(q *gorm.DB, userId int, date string) (int, int64, error) {
	var row struct {
		Cnt     int64 `gorm:"column:cnt"`
		WeekSum int64 `gorm:"column:week_sum"`
	}
	weekStart := time.Now().AddDate(0, 0, -6).Format("2006-01-02")
	err := q.Model(&QQDrop{}).
		Where("user_id = ? AND drop_date >= ?", userId, weekStart).
		Select("COALESCE(SUM(CASE WHEN drop_date = ? THEN 1 ELSE 0 END), 0) AS cnt, "+
			"COALESCE(SUM(quota_awarded), 0) AS week_sum", date).
		Scan(&row).Error
	return int(row.Cnt), row.WeekSum, err
}

// awardQQDropWithTransaction MySQL / PostgreSQL 走事务
func awardQQDropWithTransaction(drop *QQDrop, userId, quota, dailyLimit int) (*QQDrop, error) {
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		s := GetQQBotSetting()
		count, weekSum := 0, int64(0)
		if dailyLimit > 0 || s.DropDailyGuarantee > 0 {
			var err error
			count, weekSum, err = queryDropDayStats(tx, userId, drop.DropDate)
			if err != nil {
				return err
			}
			if dailyLimit > 0 && count >= dailyLimit {
				return errDropLimitReached
			}
		}
		if s.DropBalanceAnchor > 0 || s.DropDailyGuarantee > 0 {
			var balance int64
			if s.DropBalanceAnchor > 0 {
				if err := tx.Model(&identity.User{}).Where("id = ?", userId).
					Select("quota").Scan(&balance).Error; err != nil {
					return err
				}
			}
			drop.QuotaAwarded = computeDropAward(quota, int(balance), count, weekSum,
				dailyLimit, s.DropBalanceAnchor, s.DropDailyGuarantee, s.DropMinQuota, s.DropMaxQuota)
		}
		if err := tx.Create(drop).Error; err != nil {
			return err
		}
		return tx.Model(&identity.User{}).Where("id = ?", userId).
			Update("quota", gorm.Expr("quota + ?", drop.QuotaAwarded)).Error
	})
	if err != nil {
		return nil, err
	}

	go func() {
		_ = quotacache.IncrUser(userId, int64(drop.QuotaAwarded))
	}()
	return drop, nil
}

// awardQQDropWithoutTransaction SQLite 无事务路径
func awardQQDropWithoutTransaction(drop *QQDrop, userId, quota, dailyLimit int) (*QQDrop, error) {
	s := GetQQBotSetting()
	count, weekSum := 0, int64(0)
	if dailyLimit > 0 || s.DropDailyGuarantee > 0 {
		var err error
		count, weekSum, err = queryDropDayStats(dbx.DB, userId, drop.DropDate)
		if err != nil {
			return nil, err
		}
		if dailyLimit > 0 && count >= dailyLimit {
			return nil, errDropLimitReached
		}
	}
	if s.DropBalanceAnchor > 0 || s.DropDailyGuarantee > 0 {
		var balance int64
		if s.DropBalanceAnchor > 0 {
			if err := dbx.DB.Model(&identity.User{}).Where("id = ?", userId).
				Select("quota").Scan(&balance).Error; err != nil {
				return nil, err
			}
		}
		drop.QuotaAwarded = computeDropAward(quota, int(balance), count, weekSum,
			dailyLimit, s.DropBalanceAnchor, s.DropDailyGuarantee, s.DropMinQuota, s.DropMaxQuota)
	}
	if err := dbx.DB.Create(drop).Error; err != nil {
		return nil, err
	}
	if err := identity.IncreaseUserQuota(userId, drop.QuotaAwarded, true); err != nil {
		dbx.DB.Delete(drop)
		return nil, err
	}
	return drop, nil
}

// errDropLimitReached 今日掉落次数已达上限
var errDropLimitReached = errors.New("今日掉落次数已达上限")

// IsQQDropLimitReached 判断错误是否为「今日次数已达上限」
func IsQQDropLimitReached(err error) bool {
	return errors.Is(err, errDropLimitReached)
}
