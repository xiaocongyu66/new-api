package insight

import (
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

// maxScanBytes 限制单次请求参与特征扫描的提示词长度。
// agent 类请求（Claude Code / Codex）经常携带上百 KB 的上下文，
// 全量扫描会显著拉高热路径开销；工具指纹与画像特征都集中在
// system 段与最近若干条消息里，因此截断不影响判定质量。
const maxScanBytes = 32 * 1024

// maxScanMessages 限制参与扫描的消息条数（从尾部向前取）。
const maxScanMessages = 12

// Options 控制可选的分析步骤，由调用方从系统配置映射而来，
// 避免 insight 包反向依赖 setting 包。
type Options struct {
	// GenderInference 关闭后不做角色扮演性别倾向推断。
	GenderInference bool
}

// Analyze 对单次请求做画像分析。body 为原始请求体（可为空），
// header 用于客户端与中转站识别。返回值始终非 nil。
func Analyze(header http.Header, body []byte, requestPath string, opts Options) *Result {
	result := &Result{}

	system, conversation, turns, hasTools, hasPrefill, truncated := extractPrompt(body)
	lowerSystem := strings.ToLower(system)
	rawAll := system + "\n" + conversation
	lowerAll := strings.ToLower(rawAll)

	result.Turns = turns
	result.SystemLen = len(system)
	result.PromptLen = len(system) + len(conversation)
	result.HasTools = hasTools
	result.HasPrefill = hasPrefill
	result.Truncated = truncated

	result.Client, result.ClientName, result.ClientKind, result.ClientVersion, result.ClientSource, result.ClientScore =
		DetectClient(header, lowerAll)

	result.IsRelay, result.RelayVendor, result.RelayScore, result.RelayReasons =
		DetectRelay(header, result.Client, result.ClientKind)

	// embedding / rerank 这类请求没有对话语义，直接按接口归类，避免误判。
	if isEmbeddingPath(requestPath) {
		result.Category = CategoryEmbedding
		result.CategoryScore = 100
		return result
	}

	roleplayBoost := 0
	if systemPromptLooksLikeCharacter(lowerSystem) {
		roleplayBoost += 20
	}
	if result.ClientKind == KindChatUI && result.Client == "sillytavern" {
		// SillyTavern 基本专用于角色扮演，客户端本身就是强信号。
		roleplayBoost += 25
	}

	usage := classifyUsage(lowerAll, rawAll, hasTools, roleplayBoost)
	result.Category = usage.Category
	result.CategoryScore = usage.CategoryScore
	result.CodeScore = usage.Code
	result.RoleplayScore = usage.Roleplay
	result.QAScore = usage.QA
	result.CodeModules = usage.CodeModules

	if usage.Category == CategoryCode {
		result.Stack = usage.Stack
		result.StackFront = usage.StackFront
		result.StackBack = usage.StackBack
		result.Languages = usage.Languages
	}

	// 客户端类型不再定性用途，只在结构判定已确认是代码时抬高置信度。
	//
	// 此前的逻辑是"agent_cli / ide 客户端一律判 code"。线上后果：
	// 用户 1 用 claude_code 跑 QQ 群聊机器人，13676 次请求全被计成写代码，
	// 而请求正文是群聊记录与人物画像摘要，与代码无关。
	// 客户端只说明"用什么工具发的请求"，它与"请求内容是不是代码"是两件事，
	// 前者已经单独存在 client / client_kind 字段里，看板可自行按客户端分组。
	if result.Category == CategoryCode &&
		(result.ClientKind == KindAgentCLI || result.ClientKind == KindIDE) {
		if result.CodeScore < 60 {
			result.CodeScore = 60
		}
		if result.CategoryScore < 60 {
			result.CategoryScore = 60
		}
	}

	if result.Category == CategoryRoleplay || result.RoleplayScore >= 30 {
		if opts.GenderInference {
			result.AIGender, result.UserGender, result.GuessGender, result.GenderScore, result.GenderBasis =
				inferGender(lowerAll)
		}
		result.RoleplayStyle = inferRoleplayStyle(lowerAll, turns)
	}

	result.JailbreakScore, result.JailbreakLevel, result.JailbreakTags, result.JailbreakVector =
		DetectJailbreakWithClient(lowerAll, lowerSystem, hasPrefill, result.ClientKind)
	if containsControlObfuscation(system + conversation) {
		result.JailbreakScore = clampScore(result.JailbreakScore + 20)
		result.JailbreakTags = dedupeStrings(append(result.JailbreakTags, "hidden_characters"))
		if result.JailbreakScore >= 45 && result.JailbreakLevel == JailbreakSuspect {
			result.JailbreakLevel = JailbreakLikely
		}
	}
	result.Jailbreak = result.JailbreakLevel == JailbreakLikely || result.JailbreakLevel == JailbreakConfirmed

	// 保留提示词引用，供上层在需要留证时二次扫描出命中原句。
	result.SetPromptText(system, conversation)

	return result
}

// extractPrompt 从请求体中抽取 system 段与对话正文。
// 同时兼容 OpenAI chat/completions、Claude messages 与 Gemini contents 三种结构，
// 只读文本部分，忽略图片/音频等二进制内容。
func extractPrompt(body []byte) (system, conversation string, turns int, hasTools, hasPrefill, truncated bool) {
	// 调用方可能只截取了请求体前缀，此时 JSON 结构不完整。
	// gjson 对截断输入是容错的：它会解析出截断点之前的完整字段并忽略残缺尾部，
	// 因此这里只做最基本的 JSON 起始判断，而不要求整体合法。
	trimmed := strings.TrimLeft(string(body), " \t\r\n")
	if trimmed == "" || trimmed[0] != '{' {
		return "", "", 0, false, false, false
	}
	root := gjson.Parse(trimmed)

	hasTools = root.Get("tools").IsArray() && len(root.Get("tools").Array()) > 0
	if !hasTools {
		hasTools = root.Get("functions").IsArray() && len(root.Get("functions").Array()) > 0
	}

	var systemBuilder strings.Builder
	// Claude 与 Gemini 把系统提示放在独立字段。
	appendTextValue(&systemBuilder, root.Get("system"))
	appendTextValue(&systemBuilder, root.Get("system_instruction"))
	appendTextValue(&systemBuilder, root.Get("systemInstruction"))
	appendTextValue(&systemBuilder, root.Get("instructions"))

	messages := root.Get("messages")
	if !messages.IsArray() {
		messages = root.Get("contents") // Gemini
	}
	if !messages.IsArray() {
		messages = root.Get("input") // OpenAI Responses
	}

	var convBuilder strings.Builder
	budget := maxScanBytes

	if messages.IsArray() {
		items := messages.Array()
		turns = len(items)
		start := 0
		if len(items) > maxScanMessages {
			// 保留首条（通常是 system/角色卡）与最近若干条。
			appendMessage(&convBuilder, items[0], &budget)
			start = len(items) - maxScanMessages
			truncated = true
		}
		for i := start; i < len(items); i++ {
			role := items[i].Get("role").String()
			if role == "system" || role == "developer" {
				appendMessageInto(&systemBuilder, items[i], &budget)
				continue
			}
			appendMessage(&convBuilder, items[i], &budget)
		}
		if len(items) > 0 {
			lastRole := items[len(items)-1].Get("role").String()
			hasPrefill = lastRole == "assistant" || lastRole == "model"
		}
	} else {
		// completions / images 等单 prompt 形态。
		appendTextValue(&convBuilder, root.Get("prompt"))
	}

	if budget <= 0 {
		truncated = true
	}
	system, conversation = systemBuilder.String(), convBuilder.String()
	if system == "" && conversation == "" {
		// 兜底：请求体只读了前 64KB，若第一条消息的 content 字符串在前缀里
		// 就被切断，gjson 无法解析出未闭合的字符串值，system/conversation 会双空。
		// 线上 1607 条画像里有 580 条属于这种情况（prompt_tokens 平均 5 万），
		// 全部落到 other，看板上表现为"其他类型特别多"。
		// 这里退化为纯文本扫描：把前缀反转义后整体当作会话正文，
		// 拿不到 system/conversation 的分段，但分类与破甲检测仍然有效。
		if salvaged := salvageTruncatedBody(trimmed); salvaged != "" {
			conversation = salvaged
			truncated = true
		}
	} else if tail := salvageTruncatedTail(trimmed); tail != "" {
		// 部分解析成功但最后一条消息被切断：gjson 拿不到未闭合的字符串值，
		// 那段正文会被整段丢弃。而"最后一条消息"恰恰是用户这次真正说的话——
		// 代码、报错、诉求都在里面。补回来，并标记截断。
		conversation += "\n" + tail
		truncated = true
	}
	return system, conversation, turns, hasTools, hasPrefill, truncated
}

// salvageTruncatedTail 在请求体尾部被切断时，把最后一段未闭合的
// content / text 值捞回来。已完整闭合的值不处理——那些 gjson 已经解析过，
// 重复追加只会让 PromptLen 虚高。
//
// 是否截断只由"最后一个 content/text 值有没有闭合引号"决定，不看末字节。
// 曾经用"末字节是 } 或 ] 就算完整"做 O(1) 快判，但代码片段本身就以 } 结尾：
// `..."content":"重构这段代码 func Sum(a int) int { return a }` 被误判成完整 JSON，
// 用户这次真正贴的代码整段丢失，请求落到 other。
// 也不用 gjson.Valid：那要求整体合法，截断前缀永远返回 false，给不出可用信息。
func salvageTruncatedTail(body string) string {
	trimmed := strings.TrimRight(body, " \t\r\n")
	if trimmed == "" {
		return ""
	}
	start := -1
	for _, key := range []string{`"content":"`, `"content": "`, `"text":"`, `"text": "`} {
		if i := strings.LastIndex(trimmed, key); i >= 0 && i+len(key) > start {
			start = i + len(key)
		}
	}
	if start < 0 || start >= len(trimmed) {
		return ""
	}
	value := trimmed[start:]
	// 值里出现未转义的引号说明它已经闭合，属于已解析内容，不重复追加。
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' {
			i++
			continue
		}
		if value[i] == '"' {
			return ""
		}
	}
	if len(value) > maxScanBytes {
		value = value[:maxScanBytes]
	}
	return jsonEscapeUnescaper.Replace(value)
}

// jsonEscapeUnescaper 只处理关键词匹配会遇到的转义序列。
// 不做完整 JSON 反转义（\uXXXX 等）：中文在实际请求里基本以 UTF-8 原样传输，
// 而这里的目标只是让关键词能匹配上，不是还原精确原文。
var jsonEscapeUnescaper = strings.NewReplacer(
	`\n`, "\n",
	`\r`, "",
	`\t`, " ",
	`\"`, `"`,
	`\\`, `\`,
	`\/`, "/",
)

// salvageTruncatedBody 把截断的 JSON 前缀降级成可供关键词匹配的纯文本。
func salvageTruncatedBody(body string) string {
	if len(body) == 0 {
		return ""
	}
	if len(body) > maxScanBytes {
		body = body[:maxScanBytes]
	}
	return jsonEscapeUnescaper.Replace(body)
}

func appendMessage(builder *strings.Builder, message gjson.Result, budget *int) {
	appendMessageInto(builder, message, budget)
}

func appendMessageInto(builder *strings.Builder, message gjson.Result, budget *int) {
	if *budget <= 0 {
		return
	}
	content := message.Get("content")
	if !content.Exists() {
		content = message.Get("parts") // Gemini
	}
	switch {
	case content.IsArray():
		for _, part := range content.Array() {
			if text := part.Get("text"); text.Exists() {
				writeBudgeted(builder, text.String(), budget)
				continue
			}
			if part.Type == gjson.String {
				writeBudgeted(builder, part.String(), budget)
			}
		}
	case content.Type == gjson.String:
		writeBudgeted(builder, content.String(), budget)
	}
}

func appendTextValue(builder *strings.Builder, value gjson.Result) {
	if !value.Exists() {
		return
	}
	budget := maxScanBytes
	switch {
	case value.Type == gjson.String:
		writeBudgeted(builder, value.String(), &budget)
	case value.IsArray():
		for _, item := range value.Array() {
			if text := item.Get("text"); text.Exists() {
				writeBudgeted(builder, text.String(), &budget)
				continue
			}
			if item.Type == gjson.String {
				writeBudgeted(builder, item.String(), &budget)
			}
		}
	case value.IsObject():
		if parts := value.Get("parts"); parts.IsArray() {
			for _, part := range parts.Array() {
				writeBudgeted(builder, part.Get("text").String(), &budget)
			}
			return
		}
		writeBudgeted(builder, value.Get("text").String(), &budget)
	}
}

func writeBudgeted(builder *strings.Builder, text string, budget *int) {
	if text == "" || *budget <= 0 {
		return
	}
	if len(text) > *budget {
		text = text[:*budget]
	}
	builder.WriteString(text)
	builder.WriteByte('\n')
	*budget -= len(text)
}

func isEmbeddingPath(path string) bool {
	return strings.Contains(path, "/embeddings") ||
		strings.Contains(path, "/rerank") ||
		strings.Contains(path, ":embedContent")
}
