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
// 次数校验与写入放在同一事务里，避免同一用户并发消息把上限刷穿。
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

// awardQQDropWithTransaction MySQL / PostgreSQL 走事务
func awardQQDropWithTransaction(drop *QQDrop, userId, quota, dailyLimit int) (*QQDrop, error) {
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		if dailyLimit > 0 {
			var count int64
			if err := tx.Model(&QQDrop{}).
				Where("user_id = ? AND drop_date = ?", userId, drop.DropDate).
				Count(&count).Error; err != nil {
				return err
			}
			if int(count) >= dailyLimit {
				return errDropLimitReached
			}
		}
		if err := tx.Create(drop).Error; err != nil {
			return err
		}
		return tx.Model(&identity.User{}).Where("id = ?", userId).
			Update("quota", gorm.Expr("quota + ?", quota)).Error
	})
	if err != nil {
		return nil, err
	}

	go func() {
		_ = quotacache.IncrUser(userId, int64(quota))
	}()
	return drop, nil
}

// awardQQDropWithoutTransaction SQLite 无事务路径
func awardQQDropWithoutTransaction(drop *QQDrop, userId, quota, dailyLimit int) (*QQDrop, error) {
	if dailyLimit > 0 {
		count, err := CountQQDropsToday(userId)
		if err != nil {
			return nil, err
		}
		if count >= dailyLimit {
			return nil, errDropLimitReached
		}
	}
	if err := dbx.DB.Create(drop).Error; err != nil {
		return nil, err
	}
	if err := identity.IncreaseUserQuota(userId, quota, true); err != nil {
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
