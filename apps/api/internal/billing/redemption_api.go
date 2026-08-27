package billing

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/i18n"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func GetAllRedemptions(c contract.Context) {
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.GetAllRedemptions(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.CtxApiSuccess(c, pageInfo)
	return
}

func SearchRedemptions(c contract.Context) {
	keyword := c.Query("keyword")
	status := c.Query("status")
	pageInfo := common.GetPageQuery(c)
	redemptions, total, err := model.SearchRedemptions(keyword, status, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(redemptions)
	common.CtxApiSuccess(c, pageInfo)
	return
}

func GetRedemption(c contract.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	redemption, err := model.GetRedemptionById(id)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    redemption,
	})
	return
}

func AddRedemption(c contract.Context) {
	if !operation_setting.IsPaymentComplianceConfirmed() {
		common.CtxApiErrorI18n(c, i18n.MsgPaymentComplianceRequired)
		return
	}

	redemption := model.Redemption{}
	err := c.BindJSON(&redemption)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if utf8.RuneCountInString(redemption.Name) == 0 || utf8.RuneCountInString(redemption.Name) > 20 {
		common.CtxApiErrorI18n(c, i18n.MsgRedemptionNameLength)
		return
	}
	if redemption.Count <= 0 {
		common.CtxApiErrorI18n(c, i18n.MsgRedemptionCountPositive)
		return
	}
	if redemption.Count > 100 {
		common.CtxApiErrorI18n(c, i18n.MsgRedemptionCountMax)
		return
	}
	if valid, msg := validateExpiredTime(c, redemption.ExpiredTime); !valid {
		_ = c.JSON(http.StatusOK, common.H{"success": false, "message": msg})
		return
	}
	var keys []string
	for i := 0; i < redemption.Count; i++ {
		key := common.GetUUID()
		cleanRedemption := model.Redemption{
			UserId:      c.GetInt("id"),
			Name:        redemption.Name,
			Key:         key,
			CreatedTime: common.GetTimestamp(),
			Quota:       redemption.Quota,
			ExpiredTime: redemption.ExpiredTime,
		}
		err = cleanRedemption.Insert()
		if err != nil {
			common.SysError("failed to insert redemption: " + err.Error())
			_ = c.JSON(http.StatusOK, common.H{
				"success": false,
				"message": i18n.TCtx(c, i18n.MsgRedemptionCreateFailed),
				"data":    keys,
			})
			return
		}
		keys = append(keys, key)
	}
	usage.RecordManageAudit(c, "redemption.create", map[string]interface{}{
		"name":  redemption.Name,
		"count": redemption.Count,
		"quota": logger.LogQuota(redemption.Quota),
	})
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    keys,
	})
	return
}

func DeleteRedemption(c contract.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	err := model.DeleteRedemptionById(id)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
	})
	return
}

func UpdateRedemption(c contract.Context) {
	statusOnly := c.Query("status_only")
	redemption := model.Redemption{}
	err := c.BindJSON(&redemption)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	cleanRedemption, err := model.GetRedemptionById(redemption.Id)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	if statusOnly == "" {
		if valid, msg := validateExpiredTime(c, redemption.ExpiredTime); !valid {
			_ = c.JSON(http.StatusOK, common.H{"success": false, "message": msg})
			return
		}
		// If you add more fields, please also update redemption.Update()
		cleanRedemption.Name = redemption.Name
		cleanRedemption.Quota = redemption.Quota
		cleanRedemption.ExpiredTime = redemption.ExpiredTime
	}
	if statusOnly != "" {
		cleanRedemption.Status = redemption.Status
	}
	err = cleanRedemption.Update()
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    cleanRedemption,
	})
	return
}

func DeleteInvalidRedemption(c contract.Context) {
	rows, err := DeleteInvalidRedemptions()
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    rows,
	})
	return
}

func validateExpiredTime(c contract.Context, expired int64) (bool, string) {
	if expired != 0 && expired < common.GetTimestamp() {
		return false, i18n.TCtx(c, i18n.MsgRedemptionExpireTimeInvalid)
	}
	return true, ""
}
