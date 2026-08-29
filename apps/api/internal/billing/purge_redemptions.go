package billing

import (
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
)

func DeleteInvalidRedemptions() (int64, error) {
	now := common.GetTimestamp()
	result := dbx.DB.Where("status IN ? OR (status = ? AND expired_time != 0 AND expired_time < ?)", []int{common.RedemptionCodeStatusUsed, common.RedemptionCodeStatusDisabled}, common.RedemptionCodeStatusEnabled, now).Delete(&Redemption{})
	return result.RowsAffected, result.Error
}
