package insight

import (
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"sort"
	"strings"
	"unicode/utf8"
)

// 本文件负责"可人工复核"能力：把命中关键词的原句摘出来，
// 让管理员看到判定依据，而不是只看到一个分数。
// 证据只在被抽样的请求上收集（第二次扫描），不影响热路径。

// snippetWindow 是关键词左右各保留的字符数。
// 120 字节足以看清一句话的上下文，又不至于把整段提示词搬进数据库。
const snippetWindow = 120

// maxEvidenceItems 限制单次请求的证据条数，防止破甲预设命中上百个词把行撑爆。
const maxEvidenceItems = 40

// maxSnippetBytes 是单条片段的硬上限。
const maxSnippetBytes = 320

// 证据分类，前端按此分组展示。
const (
	EvidenceJailbreak = "jailbreak"
	EvidenceCode      = "code"
	EvidenceRoleplay  = "roleplay"
	EvidenceQA        = "qa"
	EvidenceTranslate = "translate"
	EvidenceStack     = "stack"
	EvidenceGender    = "gender"
	EvidenceClient    = "client"
)

// Evidence 是一条命中记录：哪个词、属于哪类判定、原文上下文。
type Evidence struct {
	Kind    string `json:"kind"`              // 判定类别，见 Evidence* 常量
	Tag     string `json:"tag,omitempty"`     // 细分标签，如 instruction_override / frontend
	Keyword string `json:"keyword"`           // 命中的关键词
	Snippet string `json:"snippet"`           // 关键词所在的原文片段
	Offset  int    `json:"offset,omitempty"`  // 关键词在扫描文本中的字节偏移
	Section string `json:"section,omitempty"` // system / conversation
}

// Sample 是一次请求的可复核材料，由中间件收集、日志阶段落库。
type Sample struct {
	Evidence []Evidence
	// Body 是请求体前缀原文。仅在配置开启完整留存时才带值。
	Body []byte
	// BodySize 是原始请求体大小（可能大于 Body 长度，因为只截前缀）。
	BodySize int
}

// ShouldCollect 决定是否为这次请求收集证据。
// 破甲与中转站命中必收（这两类最需要人工复核）；
// 有明确用途结论（code/roleplay/qa/translate）的请求也保底留证，
// 否则看板上每个画像用户都挂着"查看证据"按钮点进去却是空的——
// 判定依据无从复核。写库时按命中指纹去重（同一用户同一模式只留一行、
// 累加 hit_count），配额满了会滚动淘汰，因此保底留证不会撑爆存储。
// 其余请求（other/embedding，无可展示的判定依据）按采样率抽取。
func ShouldCollect(r *Result, ratePercent int) bool {
	if r == nil {
		return false
	}
	if r.JailbreakLevel != "" && r.JailbreakLevel != JailbreakNone {
		return true
	}
	if r.IsRelay {
		return true
	}
	if hasDefinitiveCategory(r.Category) {
		return true
	}
	if ratePercent <= 0 {
		return false
	}
	if ratePercent >= 100 {
		return true
	}
	return rand.Intn(100) < ratePercent
}

// hasDefinitiveCategory 判断用途类别是否有可供复核的判定依据。
// other 是"没匹配上任何类别"的兜底、embedding 是按接口归类无对话语义，
// 两者都没有可展示的命中证据，因此不纳入保底留证。
func hasDefinitiveCategory(category string) bool {
	switch category {
	case CategoryCode, CategoryRoleplay, CategoryQA, CategoryTranslate:
		return true
	default:
		return false
	}
}

// CollectEvidence 重新扫描提示词，收集所有命中关键词及其上下文。
// system 与 conversation 分开传入，以便标注命中位置在哪一段
// （破甲词出现在 system 段的可疑度远高于出现在用户对话里）。
func CollectEvidence(system, conversation string, result *Result) []Evidence {
	collector := &evidenceCollector{limit: maxEvidenceItems}

	for _, section := range []struct {
		name string
		text string
	}{
		{"system", system},
		{"conversation", conversation},
	} {
		if section.text == "" {
			continue
		}
		lower := strings.ToLower(section.text)

		// 破甲手法：逐规则记录，标签与 JailbreakTags 一致，便于前端对齐。
		for i := range jailbreakRules {
			rule := &jailbreakRules[i]
			collector.scan(section.name, section.text, lower, EvidenceJailbreak, rule.Tag, rule.Keywords)
		}
		// known_preset 的证据只在真正判定为自述使用越狱预设时才收集。
		// 否则"破甲"这类与世界书条目名同形的词会被单独摘出来当证据展示，
		// 而它并没有参与定级——证据面板与判定结论必须一致。
		if detectKnownPreset(lower) {
			collector.scan(section.name, section.text, lower, EvidenceJailbreak, "known_preset", jailbreakPresetMarkers)
			collector.scan(section.name, section.text, lower, EvidenceJailbreak, "known_preset", jailbreakPresetContextual)
		}
		// 用途判定依据。
		collector.scanSets(section.name, section.text, lower, EvidenceCode, codeSignals)
		// 代码结构证据：判定"是不是写代码"用的是基础语法结构共现，
		// 因此证据也必须是那些结构，否则复核时看到的依据与判定不是一回事。
		// 在剥离过 tool call 协议的文本上跑，避免把工具外壳摘成代码证据。
		stripped := stripToolCallSyntax(section.text)
		collector.scanStructure(section.name, stripped, codeStructureRules)
		// 语言级语法证据：说明"判定为 Go 是因为出现了 func 定义 / err != nil"。
		collector.scanSyntax(section.name, stripped, codeSyntaxRules)
		collector.scanSets(section.name, section.text, lower, EvidenceRoleplay, roleplaySignals)
		collector.scanSets(section.name, section.text, lower, EvidenceTranslate, translateSignals)

		// 技术栈与问答关键词证据已移除。
		//
		// 技术栈词（html/android/apk/linux/shell/并发/部署/监控…）在客户端注入的
		// 工具 JSON Schema 里成片出现——例如 "使用后端识别模型进行分析"、
		// "\"linux\"（本地 Ubuntu 24 终端环境）"、"apk_reverse : ... 逆向工具包"。
		// 它们既不是用户写的内容，也不代表任何技术倾向，展示出来只会误导人工复核
		// （线上出现过角色扮演用户的证据面板里挂着 9 条"技术栈特征"）。
		//
		// 问答词（"如何"/"为什么"/"建议"/"什么是"）是泛用疑问词，会命中任意
		// 长文本——线上见过 "如何" 命中在 NSFW 正文、"建议" 命中在游戏规则说明里。
		// 作为"这是问答"的证据几乎没有复核价值。
		//
		// 两类关键词仍参与打分（qa 需要它定类别、技术栈用于细化方向），
		// 但不再作为可展示证据。代码维度只保留 scanSyntax 的语法证据——
		// func 定义、err != nil、<?php 这些形态不可能出现在 schema 描述文本里。

		// 性别推断依据：这是最需要人工复核的一项，必须能看到原句。
		if result != nil && result.GuessGender != "" && result.GuessGender != GenderUnknown {
			collector.scan(section.name, section.text, lower, EvidenceGender, "ai_female", aiRoleMarkers.Female)
			collector.scan(section.name, section.text, lower, EvidenceGender, "ai_male", aiRoleMarkers.Male)
			collector.scan(section.name, section.text, lower, EvidenceGender, "user_female", userRoleMarkers.Female)
			collector.scan(section.name, section.text, lower, EvidenceGender, "user_male", userRoleMarkers.Male)
			// 内容偏好推断（BL/GL 题材）也要能回看命中的题材词，
			// 否则管理员看到"判定女性 依据:内容偏好"却不知道命中了什么。
			if result.GenderBasis == GenderBasisPreference {
				collector.scan(section.name, section.text, lower, EvidenceGender, "pref_bl", blMarkers)
				collector.scan(section.name, section.text, lower, EvidenceGender, "pref_gl", glMarkers)
			}
		}

		// 客户端指纹如果来自提示词，也把那句注入语摘出来。
		if result != nil && result.Client != "" && result.ClientSource != "header" {
			if rule := findClientRule(result.Client); rule != nil {
				collector.scan(section.name, section.text, lower, EvidenceClient, result.Client, rule.PromptMarkers)
			}
		}
	}
	return collector.items
}

type evidenceCollector struct {
	items []Evidence
	limit int
	// seen 避免同一关键词在同一段落里重复记录。
	seen map[string]struct{}
}

func (c *evidenceCollector) scanSets(section, text, lower, kind string, sets []keywordSet) {
	for _, set := range sets {
		c.scan(section, text, lower, kind, "", set.Keywords)
	}
}

// scanStructure 对基础语法结构规则做正则匹配，命中即摘出原句作为证据。
// Tag 用模块名（function / datatype / control …）、Keyword 用规则描述，
// 让管理员看到"判定为写代码是因为同时出现了函数定义与花括号块"。
// 同一模块在同一段落只留一条（取该模块第一条命中的规则）。
func (c *evidenceCollector) scanStructure(section, text string, rules []structureRule) {
	if text == "" {
		return
	}
	if c.seen == nil {
		c.seen = map[string]struct{}{}
	}
	lower := strings.ToLower(text)
	for i := range rules {
		if len(c.items) >= c.limit {
			return
		}
		rule := &rules[i]
		key := section + "|" + EvidenceCode + "|module:" + rule.Module
		if _, dup := c.seen[key]; dup {
			continue
		}
		loc := rule.Re.FindStringIndex(text)
		if loc == nil {
			continue
		}
		c.seen[key] = struct{}{}
		c.items = append(c.items, Evidence{
			Kind:    EvidenceCode,
			Tag:     rule.Module,
			Keyword: rule.Desc,
			Snippet: extractSnippet(text, lower, loc[0], loc[1]-loc[0]),
			Offset:  loc[0],
			Section: section,
		})
	}
}

// scanSyntax 对代码语法规则做正则匹配，命中即摘出原句作为证据。
// 与 scan 不同，它匹配的是语法结构而非字面关键词，因此在原文上跑正则、
// 用 FindStringIndex 拿到命中位置去截原句。同一语言在同一段落只记一条
// （取该语言第一条命中的规则），避免一段代码把证据表撑爆。
func (c *evidenceCollector) scanSyntax(section, text string, rules []syntaxRule) {
	if text == "" {
		return
	}
	if c.seen == nil {
		c.seen = map[string]struct{}{}
	}
	lower := strings.ToLower(text)
	for i := range rules {
		if len(c.items) >= c.limit {
			return
		}
		rule := &rules[i]
		// 每门语言在同一段落只留一条证据。
		key := section + "|" + EvidenceCode + "|lang:" + rule.Lang
		if _, dup := c.seen[key]; dup {
			continue
		}
		loc := rule.Re.FindStringIndex(text)
		if loc == nil {
			continue
		}
		c.seen[key] = struct{}{}
		c.items = append(c.items, Evidence{
			Kind:    EvidenceCode,
			Tag:     rule.Lang,
			Keyword: rule.Desc,
			Snippet: extractSnippet(text, lower, loc[0], loc[1]-loc[0]),
			Offset:  loc[0],
			Section: section,
		})
	}
}

func (c *evidenceCollector) scan(section, text, lower, kind, tag string, keywords []string) {
	for _, keyword := range keywords {
		if len(c.items) >= c.limit {
			return
		}
		if keyword == "" {
			continue
		}
		// 与打分路径共用同一套词边界规则，否则会出现"分数没算但证据里有"
		// 或反之的错位，人工复核时无法对齐；同时保证截句位置就是计分的那处命中。
		idx := indexKeyword(lower, keyword)
		if idx < 0 {
			continue
		}
		if c.seen == nil {
			c.seen = map[string]struct{}{}
		}
		key := section + "|" + kind + "|" + keyword
		if _, dup := c.seen[key]; dup {
			continue
		}
		c.seen[key] = struct{}{}
		c.items = append(c.items, Evidence{
			Kind:    kind,
			Tag:     tag,
			Keyword: keyword,
			Snippet: extractSnippet(text, lower, idx, len(keyword)),
			Offset:  idx,
			Section: section,
		})
	}
}

// EvidenceFingerprint 计算"同一类命中"的指纹，用于样本去重。
//
// 指纹只取类别、客户端与命中关键词集合，不含原句：agent 客户端每次请求
// 都带同一份注入提示词，命中词完全相同但对话正文各不相同，
// 若把原句纳入指纹就永远无法去重（线上出现过一个用户 499 条重复证据）。
// 关键词先排序再拼接，保证扫描顺序变化不影响指纹稳定性。
func EvidenceFingerprint(category, client string, items []Evidence) string {
	if len(items) == 0 {
		return ""
	}
	keys := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		key := item.Kind + ":" + item.Tag + ":" + item.Keyword
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(category + "|" + client + "|" + strings.Join(keys, ",")))
	// 取前 16 字节的十六进制（32 字符），碰撞概率可忽略且能进 varchar(32)。
	return hex.EncodeToString(sum[:16])
}

// extractSnippet 取关键词周围的原文。匹配在小写文本上做，
// 但展示时优先用原文（保留大小写）；若大小写转换改变了字节长度，
// 偏移不再可靠，则退回小写文本，保证不会切出乱码。
func extractSnippet(text, lower string, idx, keywordLen int) string {
	source := text
	if len(text) != len(lower) {
		source = lower
	}
	start := idx - snippetWindow
	if start < 0 {
		start = 0
	}
	end := idx + keywordLen + snippetWindow
	if end > len(source) {
		end = len(source)
	}
	// 对齐到 UTF-8 边界，避免中文被切成半个字。
	for start > 0 && !utf8.RuneStart(source[start]) {
		start--
	}
	for end < len(source) && !utf8.RuneStart(source[end]) {
		end++
	}
	snippet := strings.TrimSpace(source[start:end])
	if len(snippet) > maxSnippetBytes {
		snippet = snippet[:maxSnippetBytes]
		for len(snippet) > 0 && !utf8.ValidString(snippet) {
			snippet = snippet[:len(snippet)-1]
		}
	}
	return snippet
}
