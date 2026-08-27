package controller

import (
	catalog "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strings"
)

func GetChannelAffinityCacheStats(c contract.Context) {
	stats := catalog.GetChannelAffinityCacheStats()
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}

func ClearChannelAffinityCache(c contract.Context) {
	all := strings.TrimSpace(c.Query("all"))
	ruleName := strings.TrimSpace(c.Query("rule_name"))

	if all == "true" {
		deleted := catalog.ClearChannelAffinityCacheAll()
		_ = c.JSON(http.StatusOK, common.H{
			"success": true,
			"message": "",
			"data": common.H{
				"deleted": deleted,
			},
		})
		return
	}

	if ruleName == "" {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "缺少参数：rule_name，或使用 all=true 清空全部",
		})
		return
	}

	deleted, err := catalog.ClearChannelAffinityCacheByRuleName(ruleName)
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data": common.H{
			"deleted": deleted,
		},
	})
}

func GetChannelAffinityUsageCacheStats(c contract.Context) {
	ruleName := strings.TrimSpace(c.Query("rule_name"))
	usingGroup := strings.TrimSpace(c.Query("using_group"))
	keyFp := strings.TrimSpace(c.Query("key_fp"))

	if ruleName == "" {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "missing param: rule_name",
		})
		return
	}
	if keyFp == "" {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "missing param: key_fp",
		})
		return
	}

	stats := catalog.GetChannelAffinityUsageCacheStats(ruleName, usingGroup, keyFp)
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}
