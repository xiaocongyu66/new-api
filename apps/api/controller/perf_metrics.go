package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"net/http"
	"strconv"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/samber/lo"
)

func GetPerfMetricsSummary(c contract.Context) {
	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	activeGroups := append(lo.Keys(ratio_setting.GetGroupRatioCopy()), "auto")
	result, err := perfmetrics.QuerySummaryAll(hours, activeGroups)
	if err != nil {
		_ = c.JSON(http.StatusInternalServerError, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data":    result,
	})
}

func GetPerfMetrics(c contract.Context) {
	modelName := c.Query("model")
	if modelName == "" {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "model is required",
		})
		return
	}

	hours := 24
	if rawHours := c.Query("hours"); rawHours != "" {
		if parsed, err := strconv.Atoi(rawHours); err == nil {
			hours = parsed
		}
	}

	result, err := perfmetrics.Query(perfmetrics.QueryParams{
		Model: modelName,
		Group: c.Query("group"),
		Hours: hours,
	})
	if err != nil {
		_ = c.JSON(http.StatusInternalServerError, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	result.Groups = filterActiveGroups(result.Groups)

	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data":    result,
	})
}

func filterActiveGroups(groups []perfmetrics.GroupResult) []perfmetrics.GroupResult {
	activeRatios := ratio_setting.GetGroupRatioCopy()
	return lo.Filter(groups, func(g perfmetrics.GroupResult, _ int) bool {
		_, ok := activeRatios[g.Group]
		return ok || g.Group == "auto"
	})
}
