package billing

import (
	"errors"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/common/quotacache"
	"github.com/QuantumNous/new-api/internal/identity"
	"gorm.io/gorm"
	"math/rand"
	"sort"
	"time"
)

// Checkin 签到记录
type Checkin struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId       int    `json:"user_id" gorm:"not null;uniqueIndex:idx_user_checkin_date"`
	CheckinDate  string `json:"checkin_date" gorm:"type:varchar(10);not null;uniqueIndex:idx_user_checkin_date"` // 格式: YYYY-MM-DD
	QuotaAwarded int    `json:"quota_awarded" gorm:"not null"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
}

type CheckinRecord struct {
	CheckinDate         string  `json:"checkin_date"`
	QuotaAwarded        int     `json:"quota_awarded"`
	QuotaAwardedDisplay float64 `json:"quota_awarded_display"`
}

func (Checkin) TableName() string {
	return "checkins"
}

// GetUserCheckinRecords 获取用户在指定日期范围内的签到记录
func GetUserCheckinRecords(userId int, startDate, endDate string) ([]Checkin, error) {
	var records []Checkin
	err := dbx.DB.Where("user_id = ? AND checkin_date >= ? AND checkin_date <= ?",
		userId, startDate, endDate).
		Order("checkin_date DESC").
		Find(&records).Error
	return records, err
}

// HasCheckedInToday 检查用户今天是否已签到
func HasCheckedInToday(userId int) (bool, error) {
	today := time.Now().Format("2006-01-02")
	var count int64
	err := dbx.DB.Model(&Checkin{}).
		Where("user_id = ? AND checkin_date = ?", userId, today).
		Count(&count).Error
	return count > 0, err
}

// evaluateDailyCheckin 执行网页与 QQ 两个签到入口共用的前置判定与额度计算：
// 启用状态、今日重复判定、额度区间钳制与随机。两侧必须走这里，否则会出现
// 一个渠道能签、另一个渠道判定不一致的双倍领取漏洞。
//
// qqChannel 为 true 时表示调用方是 QQ 群签到（QQ 侧还有自己的渠道开关，
// 由调用方在调用前检查）。返回值是本次应发放的内部 quota。
func evaluateDailyCheckin(userId int, qqChannel bool) (int, error) {
	setting := GetCheckinSetting()
	if !setting.Enabled {
		return 0, errors.New("签到功能未启用")
	}

	// 单平台模式跨两张表判定；否则只看调用方自己渠道的记录
	var hasChecked bool
	var err error
	if setting.SinglePlatformOnly {
		hasChecked, err = HasCheckedInTodayAnyPlatform(userId)
	} else if qqChannel {
		hasChecked, err = HasQQCheckedInToday(userId)
	} else {
		hasChecked, err = HasCheckedInToday(userId)
	}
	if err != nil {
		return 0, err
	}
	if hasChecked {
		return 0, errors.New("今日已签到")
	}

	// 计算随机额度奖励。负数与倒序区间在这里钳制，保证不发放负额度。
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
	return quotaAwarded, nil
}

// UserCheckin 执行用户签到
// MySQL 和 PostgreSQL 使用事务保证原子性
// SQLite 不支持嵌套事务，使用顺序操作 + 手动回滚
func UserCheckin(userId int) (*Checkin, error) {
	quotaAwarded, err := evaluateDailyCheckin(userId, false)
	if err != nil {
		return nil, err
	}

	today := time.Now().Format("2006-01-02")
	checkin := &Checkin{
		UserId:       userId,
		CheckinDate:  today,
		QuotaAwarded: quotaAwarded,
		CreatedAt:    time.Now().Unix(),
	}

	// 根据数据库类型选择不同的策略
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		// SQLite 不支持嵌套事务，使用顺序操作 + 手动回滚
		return userCheckinWithoutTransaction(checkin, userId, quotaAwarded)
	}

	// MySQL 和 PostgreSQL 支持事务，使用事务保证原子性
	return userCheckinWithTransaction(checkin, userId, quotaAwarded)
}

// userCheckinWithTransaction 使用事务执行签到（适用于 MySQL 和 PostgreSQL）
func userCheckinWithTransaction(checkin *Checkin, userId int, quotaAwarded int) (*Checkin, error) {
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		// 步骤1: 创建签到记录
		// 数据库有唯一约束 (user_id, checkin_date)，可以防止并发重复签到
		if err := tx.Create(checkin).Error; err != nil {
			return errors.New("签到失败，请稍后重试")
		}

		// 步骤2: 在事务中增加用户额度
		if err := identity.UserQuery(tx).Where("id = ?", userId).
			Update("quota", gorm.Expr("quota + ?", quotaAwarded)).Error; err != nil {
			return errors.New("签到失败：更新额度出错")
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// 事务成功后，异步更新缓存
	go func() {
		_ = quotacache.IncrUser(userId, int64(quotaAwarded))
	}()

	return checkin, nil
}

// userCheckinWithoutTransaction 不使用事务执行签到（适用于 SQLite）
func userCheckinWithoutTransaction(checkin *Checkin, userId int, quotaAwarded int) (*Checkin, error) {
	// 步骤1: 创建签到记录
	// 数据库有唯一约束 (user_id, checkin_date)，可以防止并发重复签到
	if err := dbx.DB.Create(checkin).Error; err != nil {
		return nil, errors.New("签到失败，请稍后重试")
	}

	// 步骤2: 增加用户额度
	// 使用 db=true 强制直接写入数据库，不使用批量更新
	if err := identity.IncreaseUserQuota(userId, quotaAwarded, true); err != nil {
		// 如果增加额度失败，需要回滚签到记录
		dbx.DB.Delete(checkin)
		return nil, errors.New("签到失败：更新额度出错")
	}

	return checkin, nil
}

// GetUserCheckinStats 获取用户签到统计信息
func GetUserCheckinStats(userId int, month string) (map[string]interface{}, error) {
	// 获取指定月份的所有签到记录
	startDate := month + "-01"
	endDate := month + "-31"

	records, err := GetUserCheckinRecords(userId, startDate, endDate)
	if err != nil {
		return nil, err
	}
	checkinRecords := make([]CheckinRecord, len(records))
	for i, r := range records {
		checkinRecords[i] = CheckinRecord{
			CheckinDate:         r.CheckinDate,
			QuotaAwarded:        r.QuotaAwarded,
			QuotaAwardedDisplay: QuotaToDisplayAmount(r.QuotaAwarded),
		}
	}

	// 单平台模式下，QQ 签到的记录不在 checkins 表里，但网页日历与统计必须
	// 把它算进来，否则用户在 QQ 签到后网页仍显示"今日未签到"，与拦截逻辑
	// （evaluateDailyCheckin 跨表判定）自相矛盾。
	var qqTotalCheckins int64
	var qqTotalQuota int64
	if GetCheckinSetting().SinglePlatformOnly {
		qqRecords, err := GetUserQQCheckinRecords(userId, startDate, endDate)
		if err != nil {
			return nil, err
		}
		for _, r := range qqRecords {
			checkinRecords = append(checkinRecords, CheckinRecord{
				CheckinDate:         r.CheckinDate,
				QuotaAwarded:        r.QuotaAwarded,
				QuotaAwardedDisplay: QuotaToDisplayAmount(r.QuotaAwarded),
			})
		}
		// 按日期倒序合并（两侧各自已有序）
		sort.Slice(checkinRecords, func(i, j int) bool {
			return checkinRecords[i].CheckinDate > checkinRecords[j].CheckinDate
		})
		dbx.DB.Model(&QQCheckin{}).Where("user_id = ?", userId).Count(&qqTotalCheckins)
		dbx.DB.Model(&QQCheckin{}).Where("user_id = ?", userId).Select("COALESCE(SUM(quota_awarded), 0)").Scan(&qqTotalQuota)
	}

	// 单平台模式下"今天是否已签到"必须跨表判定，和签到拦截用同一套规则
	var hasCheckedToday bool
	if GetCheckinSetting().SinglePlatformOnly {
		hasCheckedToday, _ = HasCheckedInTodayAnyPlatform(userId)
	} else {
		hasCheckedToday, _ = HasCheckedInToday(userId)
	}

	// 获取用户所有时间的签到统计
	var totalCheckins int64
	var totalQuota int64
	dbx.DB.Model(&Checkin{}).Where("user_id = ?", userId).Count(&totalCheckins)
	dbx.DB.Model(&Checkin{}).Where("user_id = ?", userId).Select("COALESCE(SUM(quota_awarded), 0)").Scan(&totalQuota)

	return map[string]interface{}{
		"total_quota":         totalQuota + qqTotalQuota,                            // 所有时间累计获得的额度
		"total_quota_display": QuotaToDisplayAmount(int(totalQuota + qqTotalQuota)), // 累计额度的展示金额
		"total_checkins":      totalCheckins + qqTotalCheckins,                      // 所有时间累计签到次数
		"checkin_count":       len(checkinRecords),                                  // 本月签到次数
		"checked_in_today":    hasCheckedToday,                                      // 今天是否已签到
		"records":             checkinRecords,                                       // 本月签到记录详情（不含id和user_id）
	}, nil
}
