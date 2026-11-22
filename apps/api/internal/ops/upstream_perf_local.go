package ops

// 第三级回退：本地真实请求。
//
// 当上游既没有 new-api 性能接口、也没有可用的 sub2api 用量接口时，唯一还有
// 意义的证据就是本站自己记录的真实请求。它只反映本站流量（模型长时间没调用
// 就没有新证据），但比编造一条平线诚实。
//
// 与旧实现的关键差别：这里只取「窗口内每个 (模型, 小时) 的实际聚合」，
// 不再把某一条陈旧日志的最新结果无限复制到后续每个小时。没有请求的小时
// 就没有点，图表自然留白。

import (
	"context"
	"fmt"
	"strings"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/QuantumNous/new-api/internal/common/dbx"
	usagedomain "github.com/QuantumNous/new-api/internal/usage"
)

const localPerfWindowSeconds = 24 * 3600

// FetchLocalRequestPerf 聚合本站在最近 24 小时内的真实请求，按 (模型, 小时)
// 输出成功率与平均耗时。没有请求的模型不会出现在结果里。
func FetchLocalRequestPerf(ctx context.Context, channel *channelpkg.Channel) (map[string][]UpstreamHourPoint, error) {
	now := common.GetTimestamp()
	cutoff := now - localPerfWindowSeconds

	type row struct {
		ModelName string
		Type      int
		UseTime   int
		CreatedAt int64
	}
	var rows []row
	if err := dbx.LogDB.WithContext(ctx).Table("logs").
		Select("model_name, type, use_time, created_at").
		Where("channel_id = ? AND created_at >= ? AND model_name <> '' AND type IN ?",
			channel.Id, cutoff, []int{usagedomain.LogTypeConsume, usagedomain.LogTypeError}).
		Order("created_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no local requests for this channel in window")
	}

	declared := make(map[string]struct{}, len(channel.GetModels()))
	for _, m := range channel.GetModels() {
		if name := strings.TrimSpace(m); name != "" {
			declared[name] = struct{}{}
		}
	}

	type key struct {
		model string
		ts    int64
	}
	type agg struct {
		ok, err      int
		latencySum   int64
		latencyCount int64
	}
	buckets := make(map[key]*agg)
	for _, r := range rows {
		name := strings.TrimSpace(r.ModelName)
		if name == "" {
			continue
		}
		if _, ok := declared[name]; !ok {
			continue
		}
		k := key{name, r.CreatedAt - r.CreatedAt%3600}
		a := buckets[k]
		if a == nil {
			a = &agg{}
			buckets[k] = a
		}
		if r.Type == usagedomain.LogTypeConsume {
			a.ok++
			if r.UseTime > 0 {
				a.latencySum += int64(r.UseTime) * 1000
				a.latencyCount++
			}
		} else {
			a.err++
		}
	}

	out := make(map[string][]UpstreamHourPoint)
	for k, a := range buckets {
		total := a.ok + a.err
		if total == 0 {
			continue
		}
		point := UpstreamHourPoint{
			Ts:   k.ts,
			Rate: float64(a.ok) / float64(total) * 100,
		}
		if a.latencyCount > 0 {
			point.LatencyMs = a.latencySum / a.latencyCount
		}
		out[k.model] = append(out[k.model], point)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("local requests had no model matching this channel")
	}
	return out, nil
}
