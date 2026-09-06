package insight

import (
	"regexp"
)

// 本文件识别"工具调用参数区"——即请求体中 tool call 的 arguments 字段值。
// 这些区域里的代码是"模型在调用工具"，不是"用户在写代码"。
//
// 线上实证：
//   - Claude Code 的 str_replace 工具：arguments 里是完整的目标文件内容，
//     13676 次请求全被计成写代码；
//   - code_interpreter 工具：arguments 里是 Python/JS 代码；
//   - DeepSeek R1 的 tool_calls：arguments 里是 JSON 包裹的代码片段。
//
// 判定逻辑：
//   1. 先定位 tool call 外壳（JSON 中的 "arguments" 字段、Anthropic 的
//      <parameter>、OpenAI function_call 的 arguments）；
//   2. 提取参数值的字节区间；
//   3. 结构判定时，落在这些区间内的命中按"工具上下文"降级处理。

// toolCallArgPatterns 匹配各种 tool call 外壳中的 arguments 字段。
// 每个 pattern 必须带一个捕获组，捕获的是 arguments 的值（不含外层引号/标签）。
var toolCallArgPatterns = []*regexp.Regexp{
	// OpenAI / DeepSeek 风格：{"arguments": "...代码..."}
	// 匹配 "arguments" 后的字符串值，支持转义引号
	regexp.MustCompile(`(?i)"arguments"\s*:\s*"((?:[^"\\]|\\.)*)"`),
	// Anthropic 风格：<parameter name="...">...代码...</parameter>
	regexp.MustCompile(`(?is)<parameter\b[^>]{0,120}>(.*?)</parameter>`),
	// 通用 tool call：<tool_input>...代码...</tool_input>
	regexp.MustCompile(`(?is)<(?:tool_input|tool_use|tool_call|function_calls|function_call)\b[^>]{0,120}>(.*?)</(?:tool_input|tool_use|tool_call|function_calls|function_call)>`),
}

// ToolSpan 标记一段属于工具调用参数的文本区间。
type ToolSpan struct {
	Start int
	End   int
}

// extractToolCallSpans 从原文中提取所有 tool call 参数区的字节区间。
// 返回的区间是左闭右开 [Start, End)，按 Start 升序排列。
func extractToolCallSpans(raw string) []ToolSpan {
	if raw == "" {
		return nil
	}
	var spans []ToolSpan
	for _, re := range toolCallArgPatterns {
		matches := re.FindAllStringSubmatchIndex(raw, -1)
		for _, m := range matches {
			// m[2:4] 是第一个捕获组的 [start, end)
			if len(m) >= 4 && m[2] >= 0 && m[3] > m[2] {
				spans = append(spans, ToolSpan{Start: m[2], End: m[3]})
			}
		}
	}
	if len(spans) == 0 {
		return nil
	}
	// 按 Start 排序并合并重叠区间
	sortToolSpans(spans)
	return spans
}

func sortToolSpans(spans []ToolSpan) {
	// 简单插入排序，区间数通常很少
	for i := 1; i < len(spans); i++ {
		for j := i; j > 0 && spans[j].Start < spans[j-1].Start; j-- {
			spans[j], spans[j-1] = spans[j-1], spans[j]
		}
	}
	// 合并
	merged := spans[:0]
	for _, s := range spans {
		if n := len(merged); n > 0 && s.Start <= merged[n-1].End {
			if s.End > merged[n-1].End {
				merged[n-1].End = s.End
			}
			continue
		}
		merged = append(merged, s)
	}
	copy(spans, merged)
}

// isInToolContext 判断一个命中区间是否落在工具调用参数区内。
// hitStart/hitEnd 是正则匹配的字节区间。
func isInToolContext(hitStart, hitEnd int, spans []ToolSpan) bool {
	for _, s := range spans {
		if hitStart >= s.Start && hitEnd <= s.End {
			return true
		}
	}
	return false
}

// hasToolCallContext 判断原文是否包含 tool call 外壳。
func hasToolCallContext(raw string) bool {
	for _, re := range toolCallArgPatterns {
		if re.MatchString(raw) {
			return true
		}
	}
	return false
}

// stripToolCallArguments 剥离 tool call 参数内容（用于 autoban 路径）。
// 与 stripToolCallSyntax 不同，这里连参数值一起删，只保留工具名。
func stripToolCallArguments(raw string) string {
	if raw == "" {
		return ""
	}
	out := raw
	for _, re := range toolCallArgPatterns {
		out = re.ReplaceAllString(out, "")
	}
	return out
}

