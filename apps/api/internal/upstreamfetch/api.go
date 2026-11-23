package upstreamfetch

// api 档：GET 一个 JSON 接口，按声明路径把数组拆成行。
//
// 典型形状（new-api 的 /api/pricing、sub2api 的 /api/v1/model-plaza）：
//
//	{"data": [...]}                              rows=data
//	{"data": {"groups": [{"models": [...]}]}}    rows=data.groups[].models
//
// 通配符会让同一层被展开成多行，所以嵌套结构不需要写特殊代码。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"net/http"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
)

func fetchAPI(ctx context.Context, channel *channelpkg.Channel, target string, spec Spec) ([]Row, error) {
	timeout := time.Duration(spec.TimeoutSec) * time.Second
	body, err := Get(ctx, target, timeout, defaultBodyLimit, func(req *http.Request) {
		ApplyAuth(req, channel, spec.Auth)
		for k, v := range spec.Headers {
			req.Header.Set(k, v)
		}
	})
	if err != nil {
		return nil, err
	}

	// 用 UseNumber 保住大整数与高精度小数，避免价格被 float64 截断。
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("unparseable json: %w", err)
	}

	nodes, err := evalPath(root, spec.Rows)
	if err != nil {
		return nil, err
	}
	// 路径通常落在一个数组上（data、data.groups[].models）。把数组展开成行，
	// 这样同一份 Spec 既能描述「数组本身是行集」，也能描述「字段是行集」。
	flattened := make([]any, 0, len(nodes))
	for _, node := range nodes {
		if arr, ok := node.([]any); ok {
			flattened = append(flattened, arr...)
			continue
		}
		flattened = append(flattened, node)
	}
	if len(flattened) == 0 {
		return nil, fmt.Errorf("rows path %q matched nothing", spec.Rows)
	}

	var rows []Row
	for _, node := range flattened {
		obj, ok := node.(map[string]any)
		if !ok {
			continue // 数组里混入非对象时跳过，不因此整次失败
		}
		key, err := fieldText(obj, spec.Key)
		if err != nil {
			return nil, err
		}
		values := make(map[string]string, len(spec.Values))
		for name, field := range spec.Values {
			text, err := fieldText(obj, field)
			if err != nil {
				return nil, err
			}
			if text != "" {
				values[name] = text
			}
		}
		rows = append(rows, Row{Key: key, Values: values})
	}
	return rows, nil
}

// fieldText 读取行内一个字段；字段名本身也允许是路径（兼容嵌套定价对象）。
func fieldText(obj map[string]any, field string) (string, error) {
	values, err := evalPath(obj, field)
	if err != nil {
		return "", err
	}
	if len(values) == 0 {
		return "", nil
	}
	return scalarText(values[0]), nil
}

// unmarshalNumbers 是一个测试辅助：确认 UseNumber 解析出的值不会被转成 float。
func unmarshalNumbers(body []byte) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, err
	}
	return root, nil
}
