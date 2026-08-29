package billing

import (
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/model"
)

func DeleteInvalidRedemptions() (int64, error) {
	now := common.GetTimestamp()
	result := dbx.DB.Where("status IN ? OR (status = ? AND expired_time != 0 AND expired_time < ?)", []int{common.RedemptionCodeStatusUsed, common.RedemptionCodeStatusDisabled}, common.RedemptionCodeStatusEnabled, now).Delete(&model.Redemption{})
	return result.RowsAffected, result.Error
}
