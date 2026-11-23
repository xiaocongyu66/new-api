package perfsource

// 渠道的模型路由映射：上游模型名 → 本地公开别名。
//
// 探活拿到的是上游的名字，落库与展示用的是我们的别名。配了 model_mapping
// 的渠道如果不做这层反查，一个都对不上。

import (
	"strings"

	"github.com/QuantumNous/new-api/internal/common/dbx"
)

// aliasMap 读取渠道的路由映射。没有配置时返回空 map（调用方按同名处理）。
func aliasMap(channelId int) (map[string]string, error) {
	var rows []struct {
		UpstreamModel    string
		PublicModelAlias string
	}
	if err := dbx.DB.Table("channel_model_routes AS r").
		Select("r.upstream_model, r.public_model_alias").
		Where("r.channel_id = ? AND r.enabled = ?", channelId, true).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		up := strings.TrimSpace(r.UpstreamModel)
		alias := strings.TrimSpace(r.PublicModelAlias)
		if up != "" && alias != "" {
			out[up] = alias
		}
	}
	return out, nil
}
