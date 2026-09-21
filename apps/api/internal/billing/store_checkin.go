package billing

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/common/quotacache"
	"github.com/QuantumNous/new-api/internal/identity"
	"github.com/QuantumNous/new-api/internal/logger"
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

// countTodayCheckin 统计指定渠道今日的签到记录数。db 既可以是 dbx.DB（预判），
// 也可以是事务（事务内重判），使两段判定共用同一段逻辑。
func countTodayCheckin(db *gorm.DB, userId int, qqChannel bool) (bool, error) {
	today := time.Now().Format("2006-01-02")
	var model interface{}
	if qqChannel {
		model = &QQCheckin{}
	} else {
		model = &Checkin{}
	}
	var count int64
	err := db.Model(model).
		Where("user_id = ? AND checkin_date = ?", userId, today).
		Count(&count).Error
	return count > 0, err
}

// alreadyCheckedToday 按当前单平台策略判定用户今日是否已签到。单平台模式跨
// 两张表，否则只看调用方所在渠道。传入事务可把判定与后续插入收进同一临界区，
// 传入 dbx.DB 则用作打开事务前的快速预判。
func alreadyCheckedToday(db *gorm.DB, userId int, qqChannel bool) (bool, error) {
	if !GetCheckinSetting().SinglePlatformOnly {
		return countTodayCheckin(db, userId, qqChannel)
	}
	webChecked, err := countTodayCheckin(db, userId, false)
	if err != nil || webChecked {
		return webChecked, err
	}
	return countTodayCheckin(db, userId, true)
}

// evaluateDailyCheckin 执行网页与 QQ 两个签到入口共用的前置判定与额度计算：
// 启用状态、今日重复判定、额度区间钳制与随机。两侧必须走这里，否则会出现
// 一个渠道能签、另一个渠道判定不一致的双倍领取漏洞。
//
// qqChannel 为 true 时表示调用方是 QQ 群签到（QQ 侧还有自己的渠道开关，
// 由调用方在调用前检查）。返回值是本次应发放的内部 quota。
//
// 这里的"今日已签到"判定只是预判：真正的拦截在事务内锁用户行后重判（见
// userCheckinWithTransaction），否则网页与 QQ 两个并发请求可能各自通过对方
// 表外的检查后双发额度。额度区间在两个入口间共享，这是统一签到的设计目的。
func evaluateDailyCheckin(userId int, qqChannel bool) (int, error) {
	setting := GetCheckinSetting()
	if !setting.Enabled {
		return 0, errors.New("签到功能未启用")
	}

	hasChecked, err := alreadyCheckedToday(dbx.DB, userId, qqChannel)
	if err != nil {
		return 0, err
	}
	if hasChecked {
		return 0, errors.New("今日已签到")
	}

	// 计算随机额度奖励。负数与倒序区间在这里钳制，保证不发放负额度。
	// QQ 渠道使用独立的额度区间，网页渠道使用 checkin_setting 的区间。
	var minQuota, maxQuota int
	if qqChannel {
		minQuota, maxQuota = GetQQBotSetting().MinQuota, GetQQBotSetting().MaxQuota
	} else {
		minQuota, maxQuota = setting.MinQuota, setting.MaxQuota
	}
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

// lockUserForCheckin 锁定用户行直到事务结束。dbx.LockForUpdate 在 SQLite 上
// 被跳过（该库单写者模型本身串行写），在 MySQL / PostgreSQL 上发出 FOR UPDATE。
func lockUserForCheckin(tx *gorm.DB, userId int) error {
	var user identity.User
	return dbx.LockForUpdate(identity.UserQuery(tx)).
		Where("id = ?", userId).
		Select("id").
		Take(&user).Error
}

// userCheckinWithTransaction 使用事务执行签到（适用于 MySQL 和 PostgreSQL）
func userCheckinWithTransaction(checkin *Checkin, userId int, quotaAwarded int) (*Checkin, error) {
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		// 先锁用户行直到事务结束，再重判今日签到。网页与 QQ 两个渠道的并发
		// 请求会在同一用户行上串行，后到的重判能看到先到的已提交记录；
		// 每表各自的唯一约束只能挡同渠道重复，挡不住跨渠道双发。
		if err := lockUserForCheckin(tx, userId); err != nil {
			logger.LogError(nil, fmt.Sprintf("checkin: lock user %d failed: %s", userId, err))
			return errors.New("签到失败，请稍后重试")
		}
		checked, err := alreadyCheckedToday(tx, userId, false)
		if err != nil {
			logger.LogError(nil, fmt.Sprintf("checkin: recheck user %d failed: %s", userId, err))
			return errors.New("签到失败，请稍后重试")
		}
		if checked {
			return errors.New("今日已签到")
		}

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
	// SQLite 单写者语义下，把跨表重判与写入包进一个事务，重判即权威：
	// 网页与 QQ 两个并发入口在此串行化，双倍领取被拦截。
	// 额度更新用事务内裸更新，不用 identity 批量原语——否则加钱在事务外先行提交。
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		checked, err := alreadyCheckedToday(tx, userId, false)
		if err != nil {
			return err
		}
		if checked {
			return errors.New("今日已签到")
		}
		// 唯一约束 (user_id, checkin_date) 兜底防并发重复
		if err := tx.Create(checkin).Error; err != nil {
			return errors.New("签到失败，请稍后重试")
		}
		if err := identity.UserQuery(tx).Where("id = ?", userId).
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
	setting := GetCheckinSetting()
	var qqTotalCheckins int64
	var qqTotalQuota int64
	if setting.SinglePlatformOnly {
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
	if setting.SinglePlatformOnly {
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
