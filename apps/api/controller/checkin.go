package controller

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// GetCheckinStatus 获取用户签到状态和历史记录
func GetCheckinStatus(c contract.Context) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		common.CtxApiErrorMsg(c, "签到功能未启用")
		return
	}
	userId := c.GetInt("id")
	// 获取月份参数，默认为当前月份
	month := c.DefaultQuery("month", time.Now().Format("2006-01"))

	stats, err := model.GetUserCheckinStats(userId, month)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data": common.H{
			"enabled":   setting.Enabled,
			"min_quota": setting.MinQuota,
			"max_quota": setting.MaxQuota,
			"stats":     stats,
		},
	})
}

// DoCheckin 执行用户签到
func DoCheckin(c contract.Context) {
	setting := operation_setting.GetCheckinSetting()
	if !setting.Enabled {
		common.CtxApiErrorMsg(c, "签到功能未启用")
		return
	}

	userId := c.GetInt("id")

	checkin, err := model.UserCheckin(userId)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	model.RecordLog(userId, model.LogTypeSystem, fmt.Sprintf("用户签到，获得额度 %s", logger.LogQuota(checkin.QuotaAwarded)))
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "签到成功",
		"data": common.H{
			"quota_awarded": checkin.QuotaAwarded,
			"checkin_date":  checkin.CheckinDate},
	})
}
