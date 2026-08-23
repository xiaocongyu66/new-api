package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func GetRatioConfig(c contract.Context) {
	if !ratio_setting.IsExposeRatioEnabled() {
		_ = c.JSON(http.StatusForbidden, common.H{
			"success": false,
			"message": "倍率配置接口未启用",
		})
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    ratio_setting.GetExposedData(),
	})
}
