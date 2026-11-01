package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/transport/contract"
	"github.com/QuantumNous/new-api/internal/usage"
)

// GetChannelRequestStats 返回近 N 小时（默认 24，clamp 到 [1,168]）内按渠道
// 聚合的请求情况：GET /api/channel/request_stats?hours=24&channel_ids=1,2,3。
// channel_ids 为空时 channels 返回空 map。
func GetChannelRequestStats(c contract.Context) {
	hours := 24
	if raw := c.Query("hours"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			hours = parsed
		}
	}
	if hours < 1 {
		hours = 1
	}
	if hours > 168 {
		hours = 168
	}

	// A silently dropped fragment would report a half-filled aggregate for a
	// malformed request, so an unparsable id is a client error instead.
	var channelIDs []int
	if raw := c.Query("channel_ids"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.Atoi(part)
			if err != nil {
				common.ApiErrorMsg(c, "无效的渠道 ID")
				return
			}
			channelIDs = append(channelIDs, id)
		}
	}

	endTime := time.Now().Unix()
	startTime := endTime - int64(hours)*3600

	stats, err := usage.GetChannelRequestStats(channelIDs, startTime, endTime)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	channels := make(map[string]usage.ChannelRequestStat, len(stats))
	for channelID, stat := range stats {
		channels[strconv.Itoa(channelID)] = stat
	}

	c.JSON(http.StatusOK, common.H{
		"success": true,
		"data": channelRequestStatsResponse{
			Hours:    hours,
			Channels: channels,
		},
	})
}

type channelRequestStatsResponse struct {
	Hours    int                                 `json:"hours"`
	Channels map[string]usage.ChannelRequestStat `json:"channels"`
}
