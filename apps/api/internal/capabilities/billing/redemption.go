package billing

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func DeleteInvalidRedemptions() (int64, error) {
	now := common.GetTimestamp()
	result := model.DB.Where("status IN ? OR (status = ? AND expired_time != 0 AND expired_time < ?)", []int{common.RedemptionCodeStatusUsed, common.RedemptionCodeStatusDisabled}, common.RedemptionCodeStatusEnabled, now).Delete(&model.Redemption{})
	return result.RowsAffected, result.Error
}
