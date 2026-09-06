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
		return ErrSporeInsufficient
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
	var (
		err     error
		content string
	)
	switch mode {
	case "add":
		err = IncreaseUserSpore(userId, units)
		content = fmt.Sprintf("管理员发放菌种 %s", FormatSpore(units))
	case "subtract":
		err = DecreaseUserSpore(userId, units)
		content = fmt.Sprintf("管理员扣除菌种 %s", FormatSpore(units))
	case "override":
		err = SetUserSpore(userId, units)
		content = fmt.Sprintf("管理员将菌种余额设为 %s", FormatSpore(units))
	default:
		return errors.New("不支持的调整模式")
	}
	if err != nil {
		return err
	}

	writeSystemLog(userId, content)
	common.SysLog(fmt.Sprintf("spore adjusted: user=%d mode=%s units=%d", userId, mode, units))
	return nil
}
