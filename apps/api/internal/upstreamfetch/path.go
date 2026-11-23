package upstreamfetch

// 极小的 JSON 路径求值器。
//
// 只支持声明式取数真正需要的语法，刻意不做完整 JSONPath：
//
//	data.items          取对象字段
//	data.items[0]       取数组下标
//	data.groups[].models 取数组全部元素的字段（[] 等价于 [*]）
//
// 返回的永远是切片：路径可能命中多个值（通配），调用方按顺序取用。

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// evalPath 求值一条路径，返回所有命中值。
func evalPath(root any, path string) ([]any, error) {
	segments, err := parsePath(path)
	if err != nil {
		return nil, err
	}
	current := []any{root}
	for _, seg := range segments {
		var next []any
		for _, node := range current {
			switch seg.kind {
			case segField:
				obj, ok := node.(map[string]any)
				if !ok {
					continue
				}
				if value, exists := obj[seg.field]; exists {
					next = append(next, value)
				}
			case segIndex:
				arr, ok := node.([]any)
				if !ok || seg.index < 0 || seg.index >= len(arr) {
					continue
				}
				next = append(next, arr[seg.index])
			case segWildcard:
				switch typed := node.(type) {
				case []any:
					next = append(next, typed...)
				case map[string]any:
					// 对象通配按 map 遍历，顺序不稳定，取数场景基本用不到；
					// 显式按键排序以保证可复现。
					keys := make([]string, 0, len(typed))
					for k := range typed {
						keys = append(keys, k)
					}
					sortStrings(keys)
					for _, k := range keys {
						next = append(next, typed[k])
					}
				}
			}
		}
		current = next
		if len(current) == 0 {
			return nil, nil
		}
	}
	// 路径落在数组上时展开元素：调用方关心的是「有哪些项」，而不是
	// 「有一个长度为 N 的数组」。
	expanded := make([]any, 0, len(current))
	for _, node := range current {
		if arr, ok := node.([]any); ok {
			expanded = append(expanded, arr...)
			continue
		}
		expanded = append(expanded, node)
	}
	return expanded, nil
}

type segmentKind int

const (
	segField segmentKind = iota
	segIndex
	segWildcard
)

type segment struct {
	kind  segmentKind
	field string
	index int
}

func parsePath(path string) ([]segment, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	var out []segment
	i := 0
	for i < len(path) {
		switch path[i] {
		case '.':
			i++
		case '[':
			end := strings.IndexByte(path[i:], ']')
			if end < 0 {
				return nil, fmt.Errorf("unclosed bracket in %q", path)
			}
			inner := strings.TrimSpace(path[i+1 : i+end])
			switch {
			case inner == "" || inner == "*":
				out = append(out, segment{kind: segWildcard})
			default:
				idx, err := strconv.Atoi(inner)
				if err != nil {
					return nil, fmt.Errorf("non-numeric index %q in %q", inner, path)
				}
				out = append(out, segment{kind: segIndex, index: idx})
			}
			i += end + 1
		default:
			j := i
			for j < len(path) && path[j] != '.' && path[j] != '[' {
				j++
			}
			field := strings.TrimSpace(path[i:j])
			if field != "" {
				out = append(out, segment{kind: segField, field: field})
			}
			i = j
		}
	}
	return out, nil
}

// sortStrings 是一个不引入 sort 包的插入排序；这里的键数量很小。
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// formatNumber 用最短的十进制写法输出浮点，避免 1.2e-6 这种科学计数法
// 在价格场景里被下游误读。
func formatNumber(value float64) string {
	text := strconv.FormatFloat(value, 'f', -1, 64)
	return text
}

// scalarText 把 JSON 标量转成取数输出用的文本。
func scalarText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return formatNumber(typed)
	case json.Number:
		// UseNumber 保留原始字面量；但价格常写成 1.2e-6，科学计数法在下游
		// 容易读错，带指数的统一转成定点写法。
		raw := typed.String()
		if strings.ContainsAny(raw, "eE") {
			if value, err := typed.Float64(); err == nil {
				return formatNumber(value)
			}
		}
		return raw
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return fmt.Sprintf("%v", typed)
	}
}
