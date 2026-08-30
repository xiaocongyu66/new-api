package billing

import (
	"fmt"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage"
	"net/http"
	"time"
)

// GetCheckinStatus 获取用户签到状态和历史记录
func GetCheckinStatus(c contract.Context) {
	setting := GetCheckinSetting()
	if !setting.Enabled {
		common.CtxApiErrorMsg(c, "签到功能未启用")
		return
	}
	userId := c.GetInt("id")
	// 获取月份参数，默认为当前月份
	month := c.DefaultQuery("month", time.Now().Format("2006-01"))

	stats, err := GetUserCheckinStats(userId, month)
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
	setting := GetCheckinSetting()
	if !setting.Enabled {
		common.CtxApiErrorMsg(c, "签到功能未启用")
		return
	}

	userId := c.GetInt("id")

	checkin, err := UserCheckin(userId)
	if err != nil {
		_ = c.JSON(http.StatusOK, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	usage.RecordLog(userId, usage.LogTypeSystem, fmt.Sprintf("用户签到，获得额度 %s", logger.LogQuota(checkin.QuotaAwarded)))
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "签到成功",
		"data": common.H{
			"quota_awarded": checkin.QuotaAwarded,
			"checkin_date":  checkin.CheckinDate},
	})
}
