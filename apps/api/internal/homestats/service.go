/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package homestats

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/cachex"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	"github.com/QuantumNous/new-api/internal/logger"
	"github.com/QuantumNous/new-api/internal/usage"
	"github.com/samber/hot"
)

type HomeStats struct {
	Requests    int64   `json:"requests"`
	Success     int64   `json:"success"`
	Failed      int64   `json:"failed"`
	SuccessRate float64 `json:"success_rate"`
	FailureRate float64 `json:"failure_rate"`
	StatDate    string  `json:"stat_date"`
	UpdatedAt   int64   `json:"updated_at"`
}

const (
	homeStatsCacheNamespace = "new-api:home_stats:v1"
	homeStatsCacheTTL       = 26 * time.Hour
	homeStatsCacheCapacity  = 1000
)

var (
	homeStatsCache     *cachex.HybridCache[HomeStats]
	homeStatsCacheOnce sync.Once
)

func getHomeStatsCache() *cachex.HybridCache[HomeStats] {
	homeStatsCacheOnce.Do(func() {
		homeStatsCache = cachex.NewHybridCache[HomeStats](cachex.HybridCacheConfig[HomeStats]{
			Namespace: cachex.Namespace(homeStatsCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[HomeStats]{},
			Memory: func() *hot.HotCache[string, HomeStats] {
				return hot.NewHotCache[string, HomeStats](hot.LRU, homeStatsCacheCapacity).
					WithTTL(homeStatsCacheTTL).
					WithJanitor().
					Build()
			},
		})
	})
	return homeStatsCache
}

func yesterdayWindow() (startTs int64, endTs int64, statDate string) {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	yesterdayStart := todayStart.AddDate(0, 0, -1)
	return yesterdayStart.Unix(), todayStart.Unix(), yesterdayStart.Format("2006-01-02")
}

// ComputeAndCache 统计昨日窗口 [昨日0点, 今日0点) 的 relay 请求成功/失败数并写入缓存。
func ComputeAndCache(ctx context.Context) (HomeStats, error) {
	startTs, endTs, statDate := yesterdayWindow()

	var success int64
	if err := dbx.LogDB.WithContext(ctx).Model(&usage.Log{}).Where("type = ? AND created_at >= ? AND created_at < ?", usage.LogTypeConsume, startTs, endTs).Count(&success).Error; err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("home stats count consume failed: %v", err))
		return HomeStats{StatDate: statDate}, err
	}

	var failed int64
	if err := dbx.LogDB.WithContext(ctx).Model(&usage.Log{}).Where("type = ? AND created_at >= ? AND created_at < ?", usage.LogTypeError, startTs, endTs).Count(&failed).Error; err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("home stats count error failed: %v", err))
		return HomeStats{StatDate: statDate}, err
	}

	requests := success + failed
	var successRate, failureRate float64
	if requests > 0 {
		successRate = math.Round(float64(success)/float64(requests)*100*100) / 100
		failureRate = math.Round(float64(failed)/float64(requests)*100*100) / 100
	}

	stats := HomeStats{
		Requests:    requests,
		Success:     success,
		Failed:      failed,
		SuccessRate: successRate,
		FailureRate: failureRate,
		StatDate:    statDate,
		UpdatedAt:   time.Now().Unix(),
	}

	if err := getHomeStatsCache().SetWithTTL(statDate, stats, homeStatsCacheTTL); err != nil {
		common.SysError(fmt.Sprintf("home stats cache set failed: %v", err))
	}

	return stats, nil
}

// Latest 读缓存：昨日 key → 今日 key（跨天边界）→ miss 懒计算。
// 失败返回 found=false，不 panic。
func Latest(ctx context.Context) (HomeStats, bool) {
	_, _, statDate := yesterdayWindow()
	todayStatDate := time.Now().Format("2006-01-02")

	stats, found, err := getHomeStatsCache().Get(statDate)
	if err == nil && found {
		return stats, true
	}

	stats, found, err = getHomeStatsCache().Get(todayStatDate)
	if err == nil && found {
		return stats, true
	}

	stats, err = ComputeAndCache(ctx)
	if err != nil {
		common.SysError(fmt.Sprintf("home stats compute failed: %v", err))
		return HomeStats{StatDate: statDate}, false
	}
	return stats, true
}
