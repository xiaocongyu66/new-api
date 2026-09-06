package insight

import (
	"regexp"
	"strings"
)

// 本文件把"模型在调用工具"与"用户在写代码"分开。
//
// 线上实证（category=code 的样本按 hit_count 排序）：
//   - 13676 次：唯一证据是 claude_code 注入的 <system-reminder>，正文是 QQ 群聊记录；
//   - 1223 次：唯一代码证据是 "->"，来自群聊"发起了戳一戳 -> 3924002568"；
//   - 404 次：证据是 agent 系统提示词里的工具 schema（```ts interface ToolArgsMap）
//     与工具手册里的 str_replace 示例。
//
// 共性：这些都属于"工具调用协议"或"工具说明书"。它们的形态确实像代码
//（XML 标签、TS 类型声明、JSON schema），但既不是用户写的，也不代表用户在写代码。
//
// 因此在结构判定之前先剥离，分三级：
//  1. 整块丢弃：客户端注入块与工具清单/schema（标签内没有用户内容）；
//  2. 只抹标签：真正的 tool call 外壳——参数里可能就是用户要写的代码，
//     所以只去掉尖括号标签本体，内容留给结构判定自己审；
//  3. 丢弃数据围栏：```json / ```yaml 是数据不是代码，但花括号会命中块结构。

// toolBlockTags 是"整块丢弃"的标签：标签内全是客户端注入的提示复读、
// 环境快照、工具清单与 schema。
var toolBlockTags = []string{
	"system-reminder", "system_reminder",
	"environment_details", "env_details",
	"available_tools", "available-tools",
	"tool_definitions", "tool_schemas", "toolset",
	"tools", "functions", "function_metadata",
	"mcp_servers", "mcp_tools",
}

// toolBlockRes 每个标签一条正则：RE2 没有反向引用，无法用 \1 匹配同名闭合标签。
var toolBlockRes = buildToolBlockRes()

func buildToolBlockRes() []*regexp.Regexp {
	res := make([]*regexp.Regexp, 0, len(toolBlockTags))
	for _, tag := range toolBlockTags {
		quoted := regexp.QuoteMeta(tag)
		res = append(res, regexp.MustCompile(
			`(?is)<`+quoted+`\b[^>]{0,400}>.*?</\s*`+quoted+`\s*>`))
	}
	return res
}

// inlineToolTagNames 是 tool call 外壳标签名。只收工具协议专用名，
// 不收 <div>/<span> 这类正常标记语言标签。
var inlineToolTagNames = []string{
	"function_calls", "function_call", "function_results", "function_result",
	"invoke", "parameter", "param", "arguments", "args",
	"tool", "tool_use", "tool_uses", "tool_result", "tool_results",
	"tool_call", "tool_calls", "tool_name", "tool_input", "tool_output",
	"read_file", "write_to_file", "create_file", "edit_file", "apply_diff",
	"replace_in_file", "insert_content", "search_and_replace", "search_files",
	"list_files", "list_code_definition_names", "execute_command",
	"use_mcp_tool", "access_mcp_resource", "attempt_completion",
	"ask_followup_question", "new_task", "update_todo_list", "task_progress",
}

// inlineToolTagRe 只匹配标签本身（含闭合标签），内容保留。
// 覆盖 Anthropic 风格 <invoke name=…><parameter name=…>、
// Cline/Roo 风格 <read_file><path>…</path>、通用 <tool_use>/<tool_result>。
var inlineToolTagRe = regexp.MustCompile(
	`(?i)</?(?:antml:)?(?:` + strings.Join(inlineToolTagNames, "|") + `)\b[^>]{0,400}/?>`)

// dataFenceTags 是"内容是数据不是代码"的围栏标签。
var dataFenceTags = []string{
	"json", "jsonl", "json5", "ndjson",
	"yaml", "yml", "toml", "ini", "properties",
	"csv", "tsv", "log", "logs",
	"text", "txt", "plaintext", "plain",
	"markdown", "md", "mermaid",
	"env", "dotenv",
}

// dataFenceRe 丢弃完整的数据围栏。
var dataFenceRe = regexp.MustCompile(
	"(?is)`{3,}[ \\t]*(?:" + strings.Join(dataFenceTags, "|") + ")\\b[^\\n]{0,120}\\n.*?`{3,}")

// dataFenceTailRe 处理"围栏开了但正文被截断"的情况：只读请求体前缀时常见。
var dataFenceTailRe = regexp.MustCompile(
	"(?is)`{3,}[ \\t]*(?:" + strings.Join(dataFenceTags, "|") + ")\\b[^\\n]{0,120}\\n[^`]*$")

// stripToolCallSyntax 剥离 tool call 语法与数据围栏，返回供结构判定使用的文本。
// 只做删除不做重排，剩余文本的行结构（行首缩进、行尾花括号）仍然有效。
func stripToolCallSyntax(raw string) string {
	if raw == "" {
		return ""
	}
	out := raw
	// 有 '<' 才值得跑标签正则；纯中文对话通常一个都没有。
	if strings.IndexByte(out, '<') >= 0 {
		for _, re := range toolBlockRes {
			if re.MatchString(out) {
				out = re.ReplaceAllString(out, "\n")
			}
		}
		if inlineToolTagRe.MatchString(out) {
			out = inlineToolTagRe.ReplaceAllString(out, "\n")
		}
	}
	if strings.Contains(out, "```") {
		out = dataFenceRe.ReplaceAllString(out, "\n")
		out = dataFenceTailRe.ReplaceAllString(out, "\n")
	}
	return out
}
