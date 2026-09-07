package identity

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"gorm.io/gorm"
)

// Spore is an admin-issued voucher currency, stored as integer tenths on
// users.spore. It is not wallet quota: quota has recharge/refund/pre-consume
// and a USD conversion; spore is granted by operators and spent only on
// subscription plans.
const SporeUnitsPerSpore int64 = 10

var ErrSporeInsufficient = errors.New("菌种余额不足")

// SporeInsufficientError carries the current balance and required amount so
// callers (frontend, QQ bot) can show a precise message.
type SporeInsufficientError struct {
	Current  int64
	Required int64
}

func (e *SporeInsufficientError) Error() string {
	return fmt.Sprintf("菌种余额不足：当前 %s，需要 %s", FormatSpore(e.Current), FormatSpore(e.Required))
}

func (e *SporeInsufficientError) Unwrap() error {
	return ErrSporeInsufficient
}

func FormatSpore(units int64) string {
	negative := units < 0
	if negative {
		units = -units
	}
	text := fmt.Sprintf("%d.%d", units/SporeUnitsPerSpore, units%SporeUnitsPerSpore)
	if negative {
		return "-" + text
	}
	return text
}

func GetUserSpore(userId int) (int64, error) {
	if userId <= 0 {
		return 0, errors.New("invalid userId")
	}
	var spore int64
	err := dbx.DB.Model(&User{}).Where("id = ?", userId).
		Select("spore").Scan(&spore).Error
	if err != nil {
		return 0, err
	}
	return spore, nil
}

func IncreaseUserSpore(userId int, units int64) error {
	if userId <= 0 {
		return errors.New("invalid userId")
	}
	if units <= 0 {
		return errors.New("发放数量必须大于 0")
	}
	return dbx.DB.Model(&User{}).Where("id = ?", userId).
		Update("spore", gorm.Expr("spore + ?", units)).Error
}

func DecreaseUserSpore(userId int, units int64) error {
	if userId <= 0 {
		return errors.New("invalid userId")
	}
	if units <= 0 {
		return errors.New("扣减数量必须大于 0")
	}
	return DecreaseUserSporeTx(dbx.DB, userId, units)
}

func DecreaseUserSporeTx(tx *gorm.DB, userId int, units int64) error {
	if units < 0 {
		return errors.New("扣减数量不能为负数")
	}
	if units == 0 {
		return nil
	}
	result := tx.Model(&User{}).
		Where("id = ? AND spore >= ?", userId, units).
		Update("spore", gorm.Expr("spore - ?", units))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var current int64
		_ = tx.Model(&User{}).Where("id = ?", userId).
			Select("spore").Scan(&current).Error
		return &SporeInsufficientError{Current: current, Required: units}
	}
	return nil
}

func SetUserSpore(userId int, units int64) error {
	if userId <= 0 {
		return errors.New("invalid userId")
	}
	if units < 0 {
		return errors.New("菌种余额不能为负数")
	}
	return dbx.DB.Model(&User{}).Where("id = ?", userId).
		Update("spore", units).Error
}

func AdminAdjustUserSpore(userId int, mode string, units int64) error {
	var content string
	err := dbx.DB.Transaction(func(tx *gorm.DB) error {
		var opErr error
		switch mode {
		case "add":
			opErr = tx.Model(&User{}).Where("id = ?", userId).
				Update("spore", gorm.Expr("spore + ?", units)).Error
			content = fmt.Sprintf("管理员发放菌种 %s", FormatSpore(units))
		case "subtract":
			var current int64
			if err := tx.Model(&User{}).Where("id = ?", userId).
				Select("spore").Scan(&current).Error; err != nil {
				return err
			}
			if units <= 0 {
				return errors.New("扣减数量必须大于 0")
			}
			result := tx.Model(&User{}).
				Where("id = ? AND spore >= ?", userId, units).
				Update("spore", gorm.Expr("spore - ?", units))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return &SporeInsufficientError{Current: current, Required: units}
			}
			content = fmt.Sprintf("管理员扣除菌种 %s", FormatSpore(units))
		case "override":
			if units < 0 {
				return errors.New("菌种余额不能为负数")
			}
			opErr = tx.Model(&User{}).Where("id = ?", userId).
				Update("spore", units).Error
			content = fmt.Sprintf("管理员将菌种余额设为 %s", FormatSpore(units))
		default:
			return errors.New("不支持的调整模式")
		}
		return opErr
	})
	if err != nil {
		return err
	}

	writeSystemLog(userId, content)
	common.SysLog(fmt.Sprintf("spore adjusted: user=%d mode=%s units=%d", userId, mode, units))
	return nil
}
