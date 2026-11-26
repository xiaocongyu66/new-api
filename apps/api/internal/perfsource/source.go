package perfsource

// 上游性能数据源聚合层。
//
// 上游没有一个统一的性能接口，所以这里把「怎么取」按上游拆成独立文件，
// 全部实现同一个 Source 接口、产出同一种 Point 形状，由本文件按优先级串联：
//
//	newapi_source.go   new-api 的 /api/perf-metrics/*
//	sub2api_source.go  sub2api 的 /api/v1/usage 与 /api/v1/usage/errors
//	jk_source.go       公开监控页 /jk/status.json（xcmapi 这类上游）
//	local_source.go    本站自己的请求日志（最后一级，不依赖上游）
//
// 顺序依据是「离本渠道的真实请求有多近」：上游按模型统计 > 上游按账号记账 >
// 上游按组监控 > 本站日志。任何一级拿到数据就停，取不到才继续往下走。

import (
	"context"
	"fmt"
	"strings"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
)

// Point 是某模型在某一个小时的表现。
type Point struct {
	Ts        int64   // 小时桶起点（unix 秒）
	Rate      float64 // 0-100
	LatencyMs int64   // 0 = 该来源未给出
	TtftMs    int64   // 0 = 该来源未给出
}

// Snapshot 是一次取数：本地模型别名 → 逐小时序列。
// Source 记录命中的是哪一级，落库后能看出这个点的证据来自哪里。
type Snapshot struct {
	Models map[string][]Point
	Source string
}

// Source 是一级上游性能数据源。
type Source interface {
	// Name 同时用于日志与快照的 source 字段，必须是稳定的短标识。
	Name() string
	// Fetch 返回 本地模型别名 → 逐小时序列。没有可用数据时返回 error，
	// 由 Collect 继续尝试下一级；返回空 map 与 error 等价。
	Fetch(ctx context.Context, channel *channelpkg.Channel) (map[string][]Point, error)
}

// sources 按优先级返回各级数据源。
func sources() []Source {
	return []Source{
		newAPISource{},
		sub2APISource{},
		jkSource{},
		localSource{},
	}
}

// Collect 依次尝试各级数据源，返回第一个有数据的结果。
//
// 只有全部失败才返回错误，且错误里带上每一级各自的失败原因——排障时
// 需要一眼看出是「上游没这个接口」还是「有接口但没配凭证」。
func Collect(ctx context.Context, channel *channelpkg.Channel) (*Snapshot, error) {
	if channel == nil {
		return nil, fmt.Errorf("nil channel")
	}
	return collectFrom(ctx, channel, sources())
}

// collectFrom 是 Collect 的可注入版本，便于验证回退顺序与失败原因汇总。
func collectFrom(ctx context.Context, channel *channelpkg.Channel, chain []Source) (*Snapshot, error) {
	var trails []string
	for _, src := range chain {
		models, err := src.Fetch(ctx, channel)
		if err == nil && len(models) > 0 {
			if len(trails) > 0 {
				common.SysLog(fmt.Sprintf("channel %d perf source %s after: %s",
					channel.Id, src.Name(), strings.Join(trails, " ")))
			}
			return &Snapshot{Models: models, Source: src.Name()}, nil
		}
		reason := "no data"
		if err != nil {
			reason = err.Error()
		}
		trails = append(trails, fmt.Sprintf("%s=%s", src.Name(), reason))
	}
	return nil, fmt.Errorf("no perf source: %s", strings.Join(trails, " "))
}

// declaredModels 返回渠道声明的模型集合，用于过滤上游多给的模型。
func declaredModels(channel *channelpkg.Channel) map[string]struct{} {
	models := channel.GetModels()
	out := make(map[string]struct{}, len(models))
	for _, m := range models {
		if name := strings.TrimSpace(m); name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}

// localAlias 把上游模型名翻回我们的公开别名；未配置映射时按同名处理。
func localAlias(aliases map[string]string, upstreamName string) string {
	if alias, ok := aliases[upstreamName]; ok && alias != "" {
		return alias
	}
	return upstreamName
}

// channelOrigin 把渠道 base_url 归一成 origin。
//
// 四档取数（newapi / sub2api / jk / local）打的都是上游根路径下的固定接口，
// 而 base_url 常被填成带面板路径的形式（https://host/v1）。直接用原值会拼出
// /v1/api/v1/... 或 /v1/jk/status.json 这种不存在的地址，表现为「这一档不可用」
// 并静默回退，很难定位。归一化放在这里，四档共用同一套规则。
// 解析失败时退回原值：宁可打错，也不要把一个能用的渠道直接判死。
func channelOrigin(channel *channelpkg.Channel) string {
	if channel == nil {
		return ""
	}
	base := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
	if base == "" {
		return ""
	}
	if origin, err := channelpkg.UpstreamAccountOrigin(base); err == nil {
		return origin
	}
	return base
}
