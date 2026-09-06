package billing

import (
	"errors"
	"math/rand"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/common/quotacache"
	"github.com/QuantumNous/new-api/internal/identity"
	"gorm.io/gorm"
)

// QQCheckin QQ 渠道签到记录
// 与网页签到（checkins 表）分表存储，避免改动既有唯一索引
type QQCheckin struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId       int    `json:"user_id" gorm:"not null;uniqueIndex:idx_qq_user_checkin_date"`
	CheckinDate  string `json:"checkin_date" gorm:"type:varchar(10);not null;uniqueIndex:idx_qq_user_checkin_date"`
	QuotaAwarded int    `json:"quota_awarded" gorm:"not null"`
	OpenID       string `json:"open_id" gorm:"type:varchar(128)"`
	GroupOpenID  string `json:"group_open_id" gorm:"type:varchar(128)"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
}

func (QQCheckin) TableName() string {
	return "qq_checkins"
}

// HasQQCheckedInToday 检查用户今天是否已通过 QQ 签到
func HasQQCheckedInToday(userId int) (bool, error) {
	today := time.Now().Format("2006-01-02")
	var count int64
	err := dbx.DB.Model(&QQCheckin{}).
		Where("user_id = ? AND checkin_date = ?", userId, today).
		Count(&count).Error
	return count > 0, err
}

// HasCheckedInTodayAnyPlatform 检查用户今天是否在任一平台签到过
func HasCheckedInTodayAnyPlatform(userId int) (bool, error) {
	web, err := HasCheckedInToday(userId)
	if err != nil {
		return false, err
	}
	if web {
		return true, nil
	}
	return HasQQCheckedInToday(userId)
}

// GetUserQQCheckinRecords 获取用户在指定日期范围内的 QQ 签到记录
func GetUserQQCheckinRecords(userId int, startDate, endDate string) ([]QQCheckin, error) {
	var records []QQCheckin
	err := dbx.DB.Where("user_id = ? AND checkin_date >= ? AND checkin_date <= ?",
		userId, startDate, endDate).
		Order("checkin_date DESC").
		Find(&records).Error
	return records, err
}

// UserQQCheckin 执行 QQ 渠道签到
// openID / groupOpenID 仅用于留痕，便于排查问题
func UserQQCheckin(userId int, openID, groupOpenID string) (*QQCheckin, error) {
	setting := GetQQBotSetting()
	if !setting.QQCheckinEnabled {
		return nil, errors.New("QQ 签到功能未启用")
	}

	// 仅单平台签到：网页或 QQ 任一渠道签到过即视为今日已签到
	if setting.SinglePlatformOnly {
		hasChecked, err := HasCheckedInTodayAnyPlatform(userId)
		if err != nil {
			return nil, err
		}
		if hasChecked {
			return nil, errors.New("今日已签到")
		}
	} else {
		hasChecked, err := HasQQCheckedInToday(userId)
		if err != nil {
			return nil, err
		}
		if hasChecked {
			return nil, errors.New("今日已签到")
		}
	}

	// QQ 签到使用独立的额度区间
	minQuota, maxQuota := setting.MinQuota, setting.MaxQuota
	if minQuota < 0 {
		minQuota = 0
	}
	if maxQuota < minQuota {
		maxQuota = minQuota
	}
	quotaAwarded := minQuota
	if maxQuota > minQuota {
		quotaAwarded = minQuota + rand.Intn(maxQuota-minQuota+1)
	}

	checkin := &QQCheckin{
		UserId:       userId,
		CheckinDate:  time.Now().Format("2006-01-02"),
		QuotaAwarded: quotaAwarded,
		OpenID:       openID,
		GroupOpenID:  groupOpenID,
		CreatedAt:    time.Now().Unix(),
	}

	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		return qqCheckinWithoutTransaction(checkin, userId, quotaAwarded)
	}
	return qqCheckinWithTransaction(checkin, userId, quotaAwarded)
}

// qqCheckinWithTransaction 使用事务执行 QQ 签到（MySQL / PostgreSQL）
func qqCheckinWithTransaction(checkin *QQCheckin, userId int, quotaAwarded int) (*QQCheckin, error) {
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		// 唯一索引 (user_id, checkin_date) 可防止并发重复签到
		if err := tx.Create(checkin).Error; err != nil {
			return errors.New("签到失败，请稍后重试")
		}
		if err := tx.Model(&identity.User{}).Where("id = ?", userId).
			Update("quota", gorm.Expr("quota + ?", quotaAwarded)).Error; err != nil {
			return errors.New("签到失败：更新额度出错")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	go func() {
		_ = quotacache.IncrUser(userId, int64(quotaAwarded))
	}()

	return checkin, nil
}

// qqCheckinWithoutTransaction 不使用事务执行 QQ 签到（SQLite）
func qqCheckinWithoutTransaction(checkin *QQCheckin, userId int, quotaAwarded int) (*QQCheckin, error) {
	if err := dbx.DB.Create(checkin).Error; err != nil {
		return nil, errors.New("签到失败，请稍后重试")
	}
	if err := identity.IncreaseUserQuota(userId, quotaAwarded, true); err != nil {
		dbx.DB.Delete(checkin)
		return nil, errors.New("签到失败：更新额度出错")
	}
	return checkin, nil
}
