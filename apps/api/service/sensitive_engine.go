package service

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// sensitiveHits checks a single text with the layered engine.
// Layer order (1:1 with the Python baseline):
//
//	L1a 原文 AC → L1b 分隔符剥离 AC → L1c 语言隔离 AC
//	→ L2 指纹打分（8 类）→ 预筛门控（可疑字符/编码标记）
//	→ L3 归一化 AC（NFKC + 同形字 + 零宽剥离）→ L4 解码 AC
//	→ 指纹裁决（≥4 或 DAN+≥2 → block）→ L5 模板前缀子串
//
// dict == nil/empty 时跳过 AC 层，指纹与模板层仍生效。
func sensitiveCheckHits(text string, dict []string) (bool, []string) {
	if text == "" {
		return false, nil
	}
	lowered, hasCJK, hasASCII, cjkStream, nonCJKStream := scanAndLower(text)

	// L1a：明文 AC
	if ok, words := acSearchWords(lowered, dict); ok {
		return true, words
	}

	// L1b：分隔符剥离 + AC
	if strings.IndexAny(lowered, sepCharSet) >= 0 {
		stripped := stripSepManual(lowered)
		if stripped != lowered {
			if ok, words := acSearchWords(stripped, dict); ok {
				words = append(words, "(sep-strip)")
				return true, words
			}
		}
	}

	// L1c：语言隔离（仅当 CJK 与 ASCII 混合；流由 scanAndLower 顺带生成）
	if hasCJK && hasASCII {
		// 中文流：剥掉 ASCII 后过 AC（与 Python _ASCII_WORD_RE.sub 语义一致：
		// 保留所有非 ASCII 字符，再由分隔符剥离，不把字族突围成"内网"）
		if cjkStream != "" {
			if ok, words := acSearchWords(cjkStream, dict); ok {
				words = append(words, "(cjk-only)")
				return true, words
			}
		}
		if nonCJKStream != "" {
			if ok, words := acSearchWords(nonCJKStream, dict); ok {
				words = append(words, "(ascii-only)")
				return true, words
			}
		}
	}

	// L2 指纹打分（与词法层并行，真实载荷带敏感词概率低）
	score, hits := fingerprintScore(lowered)

	suspicious, encoded := scanSuspicious(text)
	if !suspicious && !encoded {
		// 无乱码无编码标记：直接进指纹论断，再兜底模板前缀
		if blocked, words := fingerprintVerdict(score, hits); blocked {
			return true, words
		}
		return templateVerdict(lowered)
	}

	// L3 归一化（NFKC + 同形字 + 零宽剥离），双趟：
	//   pass1 keep-dot：域名词（gov.cn）的点是结构字符，不能剥
	//   pass2 strip-all：标点插入（政.府）需要剥点
	base := norm.NFKC.String(stripInvisible(lowered))
	base = applyHomoglyphs(base)
	keepDot := stripSepManualKeep(base, true)
	if keepDot != lowered {
		if ok, words := acSearchWords(keepDot, dict); ok {
			words = append(words, "(norm-keepdot)")
			return true, words
		}
	}
	normalized := stripSepManualKeep(base, false)
	if normalized != lowered {
		if ok, words := acSearchWords(normalized, dict); ok {
			words = append(words, "(norm-strip)")
			return true, words
		}
	}

	// L3 解码层
	if encoded {
		for _, dec := range decodeLayers(text) {
			if ok, words := acSearchWords(dec, dict); ok {
				words = append(words, "(decoded)")
				return true, words
			}
		}
	}

	// 全路径后指纹兜底 + 模板前缀
	blocked, words := fingerprintVerdict(score, hits)
	if blocked {
		return true, words
	}
	return templateVerdict(lowered)
}

// ──────────────────────────────────────────────────────────────
// 计算结果与单词返回
// ──────────────────────────────────────────────────────────────

func acSearchWords(text string, dict []string) (bool, []string) {
	if len(dict) == 0 {
		return false, nil
	}
	return AcSearch(text, dict, true)
}

// fingerprintVerdict 指纹分级裁决（与 Python _fp_verdict 对齐）：
//
//	≥4  或 (dan 且 ≥2) → block；≥3 → review（不拦）；≥2 → log（不拦）。
func fingerprintVerdict(score int, hits []string) (bool, []string) {
	if score >= 4 || (score >= 2 && fingerprintHitsDan(hits)) {
		words := make([]string, 0, len(hits)+1)
		words = append(words, hits...)
		words = append(words, "fingerprint")
		return true, words
	}
	return false, nil
}

func fingerprintHitsDan(hits []string) bool {
	for _, h := range hits {
		if h == "dan" {
			return true
		}
	}
	return false
}

// templateVerdict L3-模板前缀子串（44 条真实载荷前 80 字符，小写去重）。
// 用不裁剪的机器：前缀尾随空白是匹配边界的一部分。
func templateVerdict(lowered string) (bool, []string) {
	if len(sensitiveTemplates) == 0 {
		return false, nil
	}
	m := getOrBuildByteACRaw(sensitiveTemplates)
	if m == nil {
		return false, nil
	}
	if len(m.search(lowered, true)) > 0 {
		return true, []string{"payload-template"}
	}
	return false, nil
}

// ──────────────────────────────────────────────────────────────
// 预检与归一化工具
// ──────────────────────────────────────────────────────────────

// scanAndLower 单趟扫描：一次完成 小写化 + CJK/ASCII 存在性 + 两个语言流。
// 替代「ToLower + 两次正则探测 + 两次 ReplaceAllString」的组合（热路径主成本）。
// 流语义与 Python 一致：
//
//	cjkStream  = 非 ASCII 词字符流（_ASCII_WORD_RE 摘除后），分隔符已由调用方处理
//	nonCJKStream = 非 CJK 字符流（_CJK_RE 摘除后）
func scanAndLower(text string) (lowered string, hasCJK, hasASCII bool, cjkStream, nonCJKStream string) {
	// ASCII 快路径
	asciiOnly := true
	for i := 0; i < len(text); i++ {
		if text[i] >= 0x80 {
			asciiOnly = false
			break
		}
	}
	if asciiOnly {
		hasASCII = true
		for i := 0; i < len(text); i++ {
			c := text[i]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
				break
			}
			hasASCII = false
		}
		return strings.ToLower(text), false, hasASCII, "", ""
	}
	var lb, cb, ab strings.Builder
	lb.Grow(len(text))
	for _, ch := range text {
		lr := unicode.ToLower(ch)
		lb.WriteRune(lr)
		if isCJKRun(ch) {
			hasCJK = true
			cb.WriteRune(lr)
		} else {
			if (lr >= 'a' && lr <= 'z') || (lr >= '0' && lr <= '9') {
				hasASCII = true
			}
			ab.WriteRune(lr)
		}
	}
	return lb.String(), hasCJK, hasASCII, cb.String(), ab.String()
}

func isCJKRun(r rune) bool {
	return (r >= 0x4e00 && r <= 0x9fff) || (r >= 0x3400 && r <= 0x4dbf)
}

// scanSuspicious 单趟扫描：normalize 可疑字符 + 编码标记（与 Python _ENCODING_MARKERS 精确一致）。
func scanSuspicious(text string) (suspicious, encoded bool) {
	encoded = hasEncodingMarkers(text)
	for _, ch := range text {
		cp := ch
		if cp < 0x80 {
			continue
		}
		if cp >= 0x4e00 && cp <= 0x9fff {
			continue
		}
		if (cp >= 0x3400 && cp <= 0x4dbf) || (cp >= 0xf900 && cp <= 0xfaff) {
			continue
		}
		if cp >= 0x3000 && cp <= 0x303f {
			continue
		}
		if (cp >= 0xff01 && cp <= 0xff0f) || (cp >= 0xff1a && cp <= 0xff20) {
			continue
		}
		if (cp >= 0xff3b && cp <= 0xff40) || (cp >= 0xff5b && cp <= 0xff65) {
			continue
		}
		suspicious = true
		break
	}
	return suspicious, encoded
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// hasEncodingMarkers 与 Python _ENCODING_MARKERS.search 同义：
//
//	20+ 段 b64、%XX、&#N; / &#xN;、\uXXXX / \xNN、U+XXXX
//
// 注意：b64 段按「连续 20 字符」计、空白即截断——Python 原正则
// [A-Za-z0-9+/]{20,} 同样在空白处断开；短 base64 在 Python 基线中
// 同样不进入解码层（解码门 base64FullRe 另有 len>=8 限制，两层独立对齐）。
func hasEncodingMarkers(text string) bool {
	b64Run := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '/':
			b64Run++
			if b64Run >= 20 {
				return true
			}
		case c == '=':
			// b64 padding 仅在有 20+ 前段时考虑，单独出现不算
		case c == '%' && i+2 < len(text) && isHexDigit(text[i+1]) && isHexDigit(text[i+2]):
			return true
		case c == '&' && i+1 < len(text) && text[i+1] == '#':
			return true
		case c == '\\' && i+1 < len(text) && (text[i+1] == 'u' || text[i+1] == 'x'):
			if text[i+1] == 'u' && i+5 < len(text) && isHexDigit(text[i+2]) && isHexDigit(text[i+3]) && isHexDigit(text[i+4]) && isHexDigit(text[i+5]) {
				return true
			}
			if text[i+1] == 'x' && i+3 < len(text) && isHexDigit(text[i+2]) && isHexDigit(text[i+3]) {
				return true
			}
		case c == 'U' && i+1 < len(text) && text[i+1] == '+':
			if i+5 < len(text) && isHexDigit(text[i+2]) && isHexDigit(text[i+3]) && isHexDigit(text[i+4]) && isHexDigit(text[i+5]) {
				return true
			}
		default:
			b64Run = 0
		}
	}
	return false
}

func stripInvisible(text string) string {
	hasZeroWidth := false
	for _, ch := range text {
		if ch < 0x200b {
			continue
		}
		switch ch {
		case '\u200b', '\u200c', '\u200d', '\ufeff',
			'\u202a', '\u202b', '\u202c', '\u202d', '\u202e':
			hasZeroWidth = true
		}
		if hasZeroWidth {
			break
		}
	}
	if !hasZeroWidth {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, ch := range text {
		switch ch {
		case '\u200b', '\u200c', '\u200d', '\ufeff',
			'\u202a', '\u202b', '\u202c', '\u202d', '\u202e':
			// 剥离
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

func applyHomoglyphs(text string) string {
	var mapped bool
	for _, ch := range text {
		if _, ok := homoglyphMap[ch]; ok {
			mapped = true
			break
		}
	}
	if !mapped {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, ch := range text {
		if r, ok := homoglyphMap[ch]; ok {
			b.WriteRune(r)
		} else {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// stripSepManualKeep 与 stripSepManual 相同，但可通过 keepDot 保留点号
// （normKeepDot 趟：gov.cn 中的点不能剥）。
func stripSepManualKeep(s string, keepDot bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '.' && keepDot {
			b.WriteRune(r)
			continue
		}
		if isSepRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// decodeLayers 对编码文本做解码尝试，与 Python try_decode_layers 对齐。
// 输入为请求方可控且长度无上限，256KB 闸防止超大 base64/实体串的线性解码放大
// （每层解码均线性，无递归展开；多轮解码不会叠加）。
func decodeLayers(text string) []string {
	if len(text) > 1<<18 {
		return nil
	}
	var candidates []string
	add := func(s string) {
		if s == "" || s == text {
			return
		}
		for _, c := range candidates {
			if c == s {
				return
			}
		}
		candidates = append(candidates, s)
	}

	t := strings.TrimSpace(text)

	// base64
	if base64FullRe.MatchString(t) && len(t) >= 8 {
		if dec, err := decodeBase64Lenient(t); err == nil {
			if s := strings.ToValidUTF8(string(dec), ""); s != "" {
				add(s)
			}
		}
	}
	// URL 编码
	if strings.Contains(t, "%") {
		add(unquoteLenient(t))
	}
	// HTML 实体
	if strings.Contains(t, "&") {
		add(htmlUnescape(t))
	}
	// Unicode 转义 \uXXXX / \xNN / 八进制
	if strings.Contains(t, `\u`) || strings.Contains(t, `\x`) {
		if s := unicodeEscapeDecode(t); s != "" {
			add(s)
		}
	}
	return candidates
}
