package perfsource

// 公开监控页：GET {base}/jk/status.json。
//
// 一些 sub2api 系上游（xcmapi 这类）把整站监控做成静态 JSON 对外公开，
// 无需鉴权、每分钟刷新，内容是模型级的 5 分钟采样：
//
//	groups[].id / supported_models
//	model_history[组id][模型] = [{at, status, response_ms}]
//
// 它按「上游的组」组织，不按我们的渠道。组用渠道设置里的 jk_group_id 指定；
// 未配置时用渠道密钥请求 /v1/models 拿到该密钥的模型集合，再与各组
// supported_models 精确比对。唯一命中才采用——多个组的模型集合完全相同时
// 拒绝猜测，因为猜错会把别的组的健康度当成自己渠道的。
//
// 状态口径（上游自己的定义）：green=正常，yellow=慢但成功，red=失败。
// 因此成功率 = (green+yellow) / 全部已知状态，而不是 green / 全部。

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
)

type jkSource struct{}

func (jkSource) Name() string { return "jk" }

// jkStatus 只声明用到的字段；这个 JSON 有十几 MB，多余的结构一律不解析。
type jkStatus struct {
	GeneratedAt string `json:"generated_at"`
	Groups      []struct {
		Id              int      `json:"id"`
		Name            string   `json:"name"`
		SupportedModels []string `json:"supported_models"`
	} `json:"groups"`
	ModelHistory map[string]map[string][]struct {
		At         string   `json:"at"`
		Status     string   `json:"status"`
		ResponseMs *float64 `json:"response_ms"`
	} `json:"model_history"`
}

func (jkSource) Fetch(ctx context.Context, channel *channelpkg.Channel) (map[string][]Point, error) {
	base := channelOrigin(channel)
	if base == "" {
		return nil, fmt.Errorf("channel has no base url")
	}
	body, err := getWithChannelKeyLimit(ctx, channel, base+"/jk/status.json", maxMonitorBody)
	if err != nil {
		return nil, fmt.Errorf("no jk monitor at this upstream: %w", err)
	}
	var status jkStatus
	if err := common.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("unparseable jk status: %w", err)
	}
	if len(status.ModelHistory) == 0 {
		return nil, fmt.Errorf("jk status has no model history")
	}

	groupID, err := resolveJKGroup(ctx, channel, base, &status)
	if err != nil {
		return nil, err
	}
	history := status.ModelHistory[strconv.Itoa(groupID)]
	if len(history) == 0 {
		return nil, fmt.Errorf("jk group %d has no model history", groupID)
	}

	aliases, err := aliasMap(channel.Id)
	if err != nil {
		return nil, err
	}
	declared := declaredModels(channel)

	type agg struct {
		ok, err  int
		latency  int64
		latCount int64
	}
	buckets := make(map[string]map[int64]*agg) // 别名 → 小时桶 → 聚合
	for upstreamName, samples := range history {
		alias := localAlias(aliases, upstreamName)
		if _, ok := declared[alias]; !ok {
			continue
		}
		for _, s := range samples {
			at, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(s.At))
			if parseErr != nil {
				continue
			}
			hour := at.Unix() - at.Unix()%3600
			if buckets[alias] == nil {
				buckets[alias] = make(map[int64]*agg)
			}
			a := buckets[alias][hour]
			if a == nil {
				a = &agg{}
				buckets[alias][hour] = a
			}
			// 未知状态不计入分母：宁可少算，也不把不认识的状态当成失败。
			switch strings.ToLower(strings.TrimSpace(s.Status)) {
			case "green", "yellow":
				a.ok++
			case "red":
				a.err++
			default:
				continue
			}
			if s.ResponseMs != nil && *s.ResponseMs > 0 {
				a.latency += int64(*s.ResponseMs)
				a.latCount++
			}
		}
	}

	out := make(map[string][]Point)
	for alias, hours := range buckets {
		for hour, a := range hours {
			total := a.ok + a.err
			if total == 0 {
				continue
			}
			point := Point{Ts: hour, Rate: float64(a.ok) / float64(total) * 100}
			if a.latCount > 0 {
				point.LatencyMs = a.latency / a.latCount
			}
			out[alias] = append(out[alias], point)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("jk monitor has no model matching this channel")
	}
	for alias := range out {
		sort.Slice(out[alias], func(i, j int) bool { return out[alias][i].Ts < out[alias][j].Ts })
	}
	return out, nil
}

// resolveJKGroup 决定这份监控数据里哪一组属于本渠道。
//
// 优先用渠道设置里显式配置的 jk_group_id；未配置时按渠道密钥能访问的模型
// 集合反查，并且只在唯一命中时采用。
func resolveJKGroup(ctx context.Context, channel *channelpkg.Channel, base string, status *jkStatus) (int, error) {
	if raw := strings.TrimSpace(channel.GetOtherSettings().JKGroupID); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid jk_group_id %q: %w", raw, err)
		}
		return id, nil
	}

	// 用渠道密钥取该密钥可访问的模型集合。这是上游按分组下发的能力清单，
	// 所以能用来反查分组——但它不保证唯一。
	body, err := getWithChannelKeyLimit(ctx, channel, base+"/v1/models", maxUpstreamBody)
	if err != nil {
		return 0, fmt.Errorf("cannot list models to identify jk group: %w", err)
	}
	var models struct {
		Data []struct {
			Id string `json:"id"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &models); err != nil {
		return 0, fmt.Errorf("unparseable model list: %w", err)
	}
	keyModels := make(map[string]struct{}, len(models.Data))
	for _, m := range models.Data {
		if id := strings.TrimSpace(m.Id); id != "" {
			keyModels[id] = struct{}{}
		}
	}
	if len(keyModels) == 0 {
		return 0, fmt.Errorf("upstream returned no models for this channel key")
	}

	var candidates []int
	for _, g := range status.Groups {
		if len(g.SupportedModels) != len(keyModels) {
			continue
		}
		same := true
		for _, m := range g.SupportedModels {
			if _, ok := keyModels[m]; !ok {
				same = false
				break
			}
		}
		if same {
			candidates = append(candidates, g.Id)
		}
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return 0, fmt.Errorf("no jk group matches this channel key's model list")
	default:
		sort.Ints(candidates)
		ids := make([]string, len(candidates))
		for i, id := range candidates {
			ids[i] = strconv.Itoa(id)
		}
		return 0, fmt.Errorf("ambiguous jk group: %s all serve the same models, set jk_group_id to one of them",
			strings.Join(ids, ", "))
	}
}
