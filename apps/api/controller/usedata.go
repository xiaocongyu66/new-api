package controller

import (
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

func parseFlowQuotaTimeRange(c contract.Context) (int64, int64, bool) {
	startTimestamp, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || startTimestamp <= 0 {
		common.CtxApiErrorMsg(c, "invalid start_timestamp")
		return 0, 0, false
	}
	endTimestamp, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || endTimestamp <= 0 {
		common.CtxApiErrorMsg(c, "invalid end_timestamp")
		return 0, 0, false
	}
	if endTimestamp < startTimestamp {
		common.CtxApiErrorMsg(c, "invalid time range")
		return 0, 0, false
	}
	return startTimestamp, endTimestamp, true
}

func GetAllQuotaDates(c contract.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	dates, err := model.GetAllQuotaDates(startTimestamp, endTimestamp, username)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

func GetQuotaDatesByUser(c contract.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	dates, err := model.GetQuotaDataGroupByUser(startTimestamp, endTimestamp)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
}

func GetUserQuotaDates(c contract.Context) {
	userId := c.GetInt("id")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	// 判断时间跨度是否超过 1 个月
	if endTimestamp-startTimestamp > 2592000 {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "时间跨度不能超过 1 个月",
		})
		return
	}
	dates, err := model.GetQuotaDataByUserId(userId, startTimestamp, endTimestamp)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

func GetAllFlowQuotaDates(c contract.Context) {
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	username := c.Query("username")
	dates, err := model.GetFlowQuotaData(startTimestamp, endTimestamp, username, 0, c.GetInt("role"))
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}

func GetUserFlowQuotaDates(c contract.Context) {
	userId := c.GetInt("id")
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	if endTimestamp-startTimestamp > 2592000 {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": "时间跨度不能超过 1 个月",
		})
		return
	}
	dates, err := model.GetFlowQuotaData(startTimestamp, endTimestamp, "", userId, common.RoleCommonUser)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    dates,
	})
	return
}
