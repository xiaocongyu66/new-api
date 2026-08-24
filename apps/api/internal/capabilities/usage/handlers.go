package usage

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/samber/lo"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func GetAllLogs(c contract.Context) {
	pageInfo := common.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	logs, total, err := GetAllLogsInternal(logType, startTimestamp, endTimestamp, modelName, username, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), channel, group, requestId, upstreamRequestId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.CtxApiSuccess(c, pageInfo)
}

func GetUserLogs(c contract.Context) {
	pageInfo := common.GetPageQuery(c)
	userId := c.GetInt("id")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	logs, total, err := GetUserLogsInternal(userId, logType, startTimestamp, endTimestamp, modelName, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), group, requestId, upstreamRequestId)
	if err != nil {
		common.CtxApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.CtxApiSuccess(c, pageInfo)
}

func SearchAllLogs(c contract.Context) {
	GetAllLogs(c)
}

func SearchUserLogs(c contract.Context) {
	GetUserLogs(c)
}

func GetLogByKey(c contract.Context) {
	tokenId, err := strconv.Atoi(c.Query("token_id"))
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": "invalid token id",
		})
		return
	}
	logs, err := GetLogByTokenIdInternal(tokenId)
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data":    logs,
	})
}

func GetLogsStat(c contract.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	modelName := c.Query("model_name")
	username := c.Query("username")
	tokenName := c.Query("token_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	stat, err := SumUsedQuotaInternal(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group)
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data":    stat,
	})
}

func GetLogsSelfStat(c contract.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	modelName := c.Query("model_name")
	tokenName := c.Query("token_name")
	token := SumUsedTokenInternal(logType, startTimestamp, endTimestamp, modelName, "", tokenName)
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data": map[string]interface{}{
			"token": token,
		},
	})
}

func GetAllQuotaDates(c contract.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	dates, err := GetAllQuotaDatesInternal(startTimestamp, endTimestamp, username)
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data":    dates,
	})
}

func GetQuotaDatesByUser(c contract.Context) {
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	dates, err := GetQuotaDataGroupByUser(startTimestamp, endTimestamp)
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data":    dates,
	})
}

func GetUserQuotaDates(c contract.Context) {
	userId := c.GetInt("id")
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	dates, err := GetQuotaDataByUserId(userId, startTimestamp, endTimestamp)
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data":    dates,
	})
}

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
		common.CtxApiErrorMsg(c, "end_timestamp must be greater than start_timestamp")
		return 0, 0, false
	}
	return startTimestamp, endTimestamp, true
}

func GetAllFlowQuotaDates(c contract.Context) {
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	username := c.Query("username")
	userId := c.GetInt("id")
	role := c.GetInt("role")
	var dates []*model.FlowQuotaData
	var err error
	switch {
	case role >= common.RoleRootUser:
		dates, err = getRootFlowQuotaData(startTimestamp, endTimestamp, username)
	case role >= common.RoleAdminUser:
		dates, err = getAdminFlowQuotaData(startTimestamp, endTimestamp, username)
	default:
		dates, err = getSelfFlowQuotaData(startTimestamp, endTimestamp, userId)
	}
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data":    dates,
	})
}

func GetUserFlowQuotaDates(c contract.Context) {
	startTimestamp, endTimestamp, ok := parseFlowQuotaTimeRange(c)
	if !ok {
		return
	}
	userId := c.GetInt("id")
	dates, err := getSelfFlowQuotaData(startTimestamp, endTimestamp, userId)
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, common.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"data":    dates,
	})
}

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

func GetChannelAffinityUsageCacheStats(c contract.Context) {
	ruleName := strings.TrimSpace(c.Query("rule_name"))
	usingGroup := strings.TrimSpace(c.Query("using_group"))
	keyFp := strings.TrimSpace(c.Query("key_fp"))
	stats := service.GetChannelAffinityUsageCacheStats(ruleName, usingGroup, keyFp)
	_ = c.JSON(http.StatusOK, common.H{
		"success": true,
		"message": "",
		"data":    stats,
	})
}

func GetRankings(c contract.Context) {
	result, err := service.GetRankingsSnapshot(c.DefaultQuery("period", "week"))
	if err != nil {
		_ = c.JSON(http.StatusBadRequest, map[string]any{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	_ = c.JSON(http.StatusOK, map[string]any{
		"success": true,
		"data":    result,
	})
}