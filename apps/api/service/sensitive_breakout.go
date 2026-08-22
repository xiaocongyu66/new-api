package service

import (
	"embed"
	"regexp"
	"sync"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// 破甲术语精准词库（tech 层）——具名破甲/越狱词汇，命中即拦，输入输出双向生效。
//
//go:embed testdata/sensitive_breakout_terms.json
var breakoutTermsFS embed.FS

var (
	breakoutTermsOnce sync.Once
	breakoutTerms     []string
)

func loadBreakoutTerms() []string {
	breakoutTermsOnce.Do(func() {
		data, err := breakoutTermsFS.ReadFile("testdata/sensitive_breakout_terms.json")
		if err != nil {
			common.SysError("breakoutTerms load failed: " + err.Error())
			return
		}
		var payload struct {
			Terms []string `json:"terms"`
		}
		if err := common.Unmarshal(data, &payload); err != nil {
			common.SysError("breakoutTerms parse failed: " + err.Error())
			return
		}
		breakoutTerms = payload.Terms
	})
	return breakoutTerms
}


//go:embed testdata/sensitive_target_action_terms.json
var targetActionTermsFS embed.FS

var (
	targetActionTermsOnce sync.Once
	targetActionTerms     []string
)

func loadTargetActionTerms() []string {
	targetActionTermsOnce.Do(func() {
		data, err := targetActionTermsFS.ReadFile("testdata/sensitive_target_action_terms.json")
		if err != nil {
			common.SysError("targetActionTerms load failed: " + err.Error())
			return
		}
		var payload struct {
			Terms []string `json:"terms"`
		}
		if err := common.Unmarshal(data, &payload); err != nil {
			common.SysError("targetActionTerms parse failed: " + err.Error())
			return
		}
		targetActionTerms = payload.Terms
	})
	return targetActionTerms
}


var (
	breakoutTermsLower []string
	targetActionLower  []string
)

func init() {
	loadBreakoutTerms()
	loadTargetActionTerms()
	breakoutTermsLower = make([]string, 0, len(breakoutTerms))
	for _, t := range breakoutTerms {
		if t = strings.TrimSpace(t); t != "" {
			breakoutTermsLower = append(breakoutTermsLower, strings.ToLower(t))
		}
	}
	targetActionLower = make([]string, 0, len(targetActionTerms))
	for _, t := range targetActionTerms {
		if t = strings.TrimSpace(t); t != "" {
			targetActionLower = append(targetActionLower, strings.ToLower(t))
		}
	}
}

// 匹配前对文本做归一化（同形字/分隔符剥离），覆盖 "ig nore"、"actas" 类变体。
// 原文也匹配一遍：同形字折叠会把西里尔/希腊字母转成拉丁（покажи→пokaжи），
// 纯西里尔词（俄语等）在归一化变体里匹配不到，必须保留原文通道。
func checkBreakoutTerms(text string) string {
	variants := append([]string{strings.ToLower(text)}, breakoutNormVariants(text)...)
	for _, norm := range variants {
		for _, t := range breakoutTermsLower {
			if strings.Contains(norm, t) {
				return t
			}
		}
	}
	return ""
}

// checkTargetActionTerms 命中任一个攻击目标动作词即返回命中的词。
func checkTargetActionTerms(text string) string {
	for _, norm := range breakoutNormVariants(text) {
		normLower := strings.ToLower(norm)
		for _, t := range targetActionLower {
			if i := strings.Index(normLower, t); i >= 0 && !isDefenseContext(normLower, i, i+len(t)) {
				return t
			}
		}
	}
	return ""
}

// breakoutNormVariants 生成归一化变体：
//  1. 剥离分隔符（中文词 "越.狱"→"越狱"、英文词连打 "actasofan"）——覆盖中文分隔符插入
//  2. 标点→空格（英文短语 "ignore.previous.instructions"→"ignore previous instructions"）
//
// 全角→半角、同形字折叠（Cyrillic/Greek 伪 ASCII）在两个变体中都先做。
func breakoutNormVariants(text string) []string {
	return []string{normalizeBreakoutText(text), normalizeBreakoutSpaces(text)}
}

// normalizeBreakoutText 归一化：同形字折叠（Cyrillic/Greek 伪 ASCII）+ 全角→半角 +
// 分隔符剥离（保留单词空格与换行，避免英文短语连死）。
// 不引入额外 jar；直接复用 sensitive_data.go 的同形表与分隔符判定。
func normalizeBreakoutText(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch r {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			b.WriteRune(r)
			continue
		}
		// 全角 ASCII（U+FF01..FF5E）→ 半角
		if r >= 0xff01 && r <= 0xff5e {
			b.WriteRune(r - 0xfee0)
			continue
		}
		if mapped, ok := homoglyphMap[r]; ok {
			b.WriteRune(mapped)
			continue
		}
		if isSepRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// normalizeBreakoutSpaces 归一化变体二：同形字折叠 + 全角→半角 +
// 非空白分隔符替换为空格（保留单词间空格），"ignore.previous.instructions"→
// "ignore previous instructions"，覆盖英文短语被标点隔断的变体。
func normalizeBreakoutSpaces(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	prevSpace := false
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f' {
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		// 全角 ASCII（U+FF01..FF5E）→ 半角
		if r >= 0xff01 && r <= 0xff5e {
			b.WriteRune(r - 0xfee0)
			continue
		}
		if mapped, ok := homoglyphMap[r]; ok {
			b.WriteRune(mapped)
			continue
		}
		if isSepRune(r) {
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// CheckSensitiveAll 输入/输出统一入口：目标域名 → 攻击目标动作词 → 破甲术语 → 词库/指纹/模板。
// 返回 (是否拦截, 标签)。
func CheckSensitiveAll(text string) (bool, string) {
	if text == "" {
		return false, ""
	}
	if d := CheckSensitiveTargets(text); d != "" {
		return true, "target:" + d
	}
	if t := checkTargetActionTerms(text); t != "" {
		return true, "target-action:" + t
	}
	if t := checkTargetActionCombo(text); t != "" {
		return true, "target-combo:" + t
	}
	if t := checkPersonaEvasionCombo(text); t != "" {
		return true, "persona-evasion:" + t
	}
	if t := checkBreakoutTerms(text); t != "" {
		return true, "breakout:" + t
	}
	if hit, words := sensitiveCheckHits(text, setting.SensitiveWords); hit {
		return true, strings.Join(words, ",")
	}
	return false, ""
}

// ──────────────────────────────────────────────────────────────
// 实体×动词组合层：精确短语词库追不上自然语言变形
// （"攻陷广东省政府门户网站"、"ddos the chinese government website"），
// 用"强攻击动词 ∩ 政府目标实体"同现判定兜底。
// 只在高置信词对上生效，避免误伤技术讨论。
// （网上调研结论：政务站点攻击防护的标准做法是实体×动作组合规则）
// ──────────────────────────────────────────────────────────────

var (
	// govTargetRe 政府目标实体：政务/平级政府站点名词（中英）。
	// 全部来自 govdir.cn 政府网址目录中的站点类型。
	govTargetRe = regexp.MustCompile(`(?i)政务(?:网|平台|系统|网站|服务器|数据)|政府(?:网站|门户|内网|官网|服务器|数据库|网络|平台)|政企(?:网站|官网|门户)|社保(?:局|系统)|人社(?:局|系统)|government\s*(?:website|site|portal|system|database|web)|gov\s*website`)
	// govAction 攻击动作（中英）——未受控动词如"写""用"不算
	govActionRe = regexp.MustCompile(`(?i)(攻击|入侵|渗透|攻破|攻陷|拿下|黑|黑掉|破解|爆破|瘫痪|篡改|劫持|注入|撞库|脱库|爬取|扫描|探测|钓鱼|伪造|克隆|仿冒|冒充|ddos|dos|hack|attack|breach|penetr|exploit|take\s*down|knock\s*(down|out)|deface|phish|clone|impersonat|mimic|sql\s*injection|fivezero|denial\s*of\s*service)`)
)

// govDefenseRe 防御语境词：出现即豁免 combo/target-action 判定
// （"如何防范政府网站被钓鱼攻击"是防护咨询，不是攻击指令）。
var govDefenseRe = regexp.MustCompile(`(?i)(防范|防御|防护|预防|加固|阻止|屏蔽|保护|防攻|安全(建议|指南|方案)|prevent|defend|protect|harden|mitigat|avoid|protection|defense|security check|防火墙|拦截(攻击|钓鱼)|应对)`)

// isDefenseContext 命中区间 ±60 字符内出现防御语境词 → true。
func isDefenseContext(text string, hitStart, hitEnd int) bool {
	lo := hitStart - 60
	if lo < 0 {
		lo = 0
	}
	hi := hitEnd + 60
	if hi > len(text) {
		hi = len(text)
	}
	return govDefenseRe.MatchString(text[lo:hi])
}

// distanceFor target/action 位置：任一 target 与 action 相距 ≤ maxGap 判定命中。
// maxGap=120 容忍长句包装，正常技术讨论（"政府网站的渗透测试指南"）也含
// target+action 但动作是防御性词（"防护""加固"），不在 action 集合内。
const govComboMaxGap = 120

// checkTargetActionCombo 当精确词库未命中时，用目标实体+攻击动作同现判定。
func checkTargetActionCombo(text string) string {
	var ts, as [][]int
	tl := strings.ToLower(text)
	for _, m := range govTargetRe.FindAllStringIndex(tl, -1) {
		ts = append(ts, m)
	}
	if len(ts) == 0 {
		return ""
	}
	for _, m := range govActionRe.FindAllStringIndex(tl, -1) {
		as = append(as, m)
	}
	if len(as) == 0 {
		return ""
	}
	for _, t := range ts {
		for _, a := range as {
			if isDefenseContext(tl, t[0], t[1]) || isDefenseContext(tl, a[0], a[1]) {
				continue
			}
			// 相邻判定：目标实体与攻击词当前位置相距不超过阈值
			dist := a[0] - t[1]
			if dist < 0 {
				dist = t[0] - a[1]
			}
			if dist >= 0 && dist <= govComboMaxGap {
				return tl[t[0]:t[1]]
			}
		}
	}
	return ""
}

// ──────────────────────────────────────────────────────────────
// 人格×规则规避组合层（persona-evasion combo）：人格接管类破甲
// （STAN/DUDE/Mongo Tom/ZORG）的指纹得分只有 0-1，够不到 ≥4 阈值，
// 但结构稳定 = 人格标记 ∩ 规则规避语言 同现。良性角色扮演（tavern
// 角色卡）只有人格标记、零规避语言，AND 天然切开，无需防御语境豁免。
// 与 gov 实体×动作 combo 同架构；挂 tech 组（破甲语义）。
// ──────────────────────────────────────────────────────────────

var (
	// personaRe 人格接管标记（中英）。
	personaRe = regexp.MustCompile(`(?i)pretend to be|immerse yourself into the role|roleplay as|you will respond|going to (act|pretend)|respond (to .{0,30} )?as|in character as|have a roleplay|act as a|扮演|假扮|你现在是`)
	// evasionRe 规则规避语言（中英）——良性角色扮演不含这些。
	evasionRe = regexp.MustCompile(`(?i)broken free of the (typical )?confines|not have to abide by the rules|does not have to abide|devoid of any warnings|regardless of its nature|strive to avoid (the )?norms|no moral or ethical restrictions|bypassing.{0,20}(limitations|constraints)|not bound by any laws|without restriction|programming is invalid|不受约束|为所欲为|无视(规则|限制)`)
)

// personaEvasionMaxGap 人格标记与规避语言的最大间距。
// Mongo Tom 两者相距 391 字符，取 500 留余量。
const personaEvasionMaxGap = 500

// checkPersonaEvasionCombo 人格标记 + 规则规避语言同现（≤gap）→ 命中。
// tech 组关闭时不判定。
func checkPersonaEvasionCombo(text string) string {
	if !setting.SensitiveGroupEnabled("tech") {
		return ""
	}
	tl := strings.ToLower(text)
	ps := personaRe.FindAllStringIndex(tl, -1)
	if len(ps) == 0 {
		return ""
	}
	es := evasionRe.FindAllStringIndex(tl, -1)
	if len(es) == 0 {
		return ""
	}
	for _, p := range ps {
		for _, e := range es {
			dist := e[0] - p[1]
			if dist < 0 {
				dist = p[0] - e[1]
			}
			if dist >= 0 && dist <= personaEvasionMaxGap {
				return tl[p[0]:p[1]]
			}
		}
	}
	return ""
}
