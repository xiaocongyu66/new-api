package service

import (
	"embed"
	"encoding/base64"
	"html"
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

//go:embed testdata/sensitive_templates.json
var sensitiveTemplatesFS embed.FS

var (
	sensitiveTemplatesOnce  sync.Once
	sensitiveTemplates      []string
	sensitiveTemplateGroups []string
	sensitiveTemplatesTech  []string
)

// loadSensitiveTemplates L3-模板前缀库：真实攻击载荷前 80 字符（小写、去重），
// 与 Python 基线 44 条特征完全一致；groups 同步标注 tech/rp 组。
func loadSensitiveTemplates() ([]string, []string) {
	sensitiveTemplatesOnce.Do(func() {
		data, err := sensitiveTemplatesFS.ReadFile("testdata/sensitive_templates.json")
		if err != nil {
			common.SysError("sensitive templates load failed: " + err.Error())
			return
		}
		var payload struct {
			Prefixes []string `json:"prefixes"`
			Groups   []string `json:"groups"`
		}
		if err := common.Unmarshal(data, &payload); err != nil {
			common.SysError("sensitive templates parse failed: " + err.Error())
			return
		}
		if len(payload.Groups) != len(payload.Prefixes) {
			common.SysError("sensitive templates groups length mismatch")
			return
		}
		sensitiveTemplates = payload.Prefixes
		sensitiveTemplateGroups = payload.Groups
	})
	return sensitiveTemplates, sensitiveTemplateGroups
}

func init() {
	loadSensitiveTemplates() // 启动即加载，避免首请求触发解析
	loadFingerprintRaw()
	for _, fc := range fingerprintRaw {
		fpCategoryGroup[fc.name] = fc.group
	}
	// tech 组模板子集（rp 组关闭时的快路径机器）
	sensitiveTemplatesTech = make([]string, 0, len(sensitiveTemplates))
	for i, g := range sensitiveTemplateGroups {
		if g == "tech" {
			sensitiveTemplatesTech = append(sensitiveTemplatesTech, sensitiveTemplates[i])
		}
	}
}

// ──────────────────────────────────────────────────────────────
// 同形字映射（Cyrillic/Greek → Latin，NFKC 不处理的部分）
// 与 Python HOMOGLYPH_MAP 完全一致（多码点键在 Python 端同样不可达，未移植）。
// ──────────────────────────────────────────────────────────────
var homoglyphMap = map[rune]rune{
	'\u0430': 'a', '\u0435': 'e', '\u0456': 'i', '\u043e': 'o',
	'\u0440': 'p', '\u0441': 'c', '\u0445': 'x', '\u0443': 'y',
	'\u03b1': 'a', '\u03bf': 'o', '\u03b5': 'e', '\u03b9': 'i',
	'\u0432': 'b', '\u043d': 'h', '\u043a': 'k', '\u043c': 'm',
	'\u0442': 't', '\u0458': 'j',
}

// ──────────────────────────────────────────────────────────────
// 分隔符与归一化正则（与 Python regex 原串对齐；\s 按 Unicode 空白展开）
// ──────────────────────────────────────────────────────────────

// sepUnicodeWS Python 3 中 \s 匹配的全部 Unicode 空白。
const sepUnicodeWS = "\u0020\u0009\u000a\u000b\u000c\u000d\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"

// sepCharSet 用于廉价短路：文本中无任何分隔符时跳过正则剥离。
const sepCharSet = sepUnicodeWS + "._*\\-/\\|~`'\":;!?(){}[]<>@#$%^&=|"

// _SEP_STRIP 原串: r'[\s._*\-+/\\|~`\",;:!?(){}\[\]<>@#$%^&=|]+'
var sepStripRe = regexp.MustCompile("[" + sepUnicodeWS + "._*\\-+/\\\\|~`\"',;:!?(){}\\[\\]<>@#$%^&=|]+")

// normKeepDotRe: 同上但不含点号（gov.cn 的域名字符保留）
var normKeepDotRe = regexp.MustCompile("[" + sepUnicodeWS + "_*\\-+/\\\\|~`\"',;:!?(){}\\[\\]<>@#$%^&=|]+")

// stripSepManual 与 sepStripRe 字符类一致的手工单通剥离（热路径避免正则引擎全串重建）。
func stripSepManual(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isSepRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isSepRune(r rune) bool {
	if r < 0x80 {
		switch r {
		case ' ', '\t', '\n', '\v', '\f', '\r', '.', '_', '*', '-', '+', '/', '\\', '|', '~', '`', '"', '\'', ',', ';', ':', '!', '?', '(', ')', '{', '}', '[', ']', '<', '>', '@', '#', '$', '%', '^', '&', '=':
			return true
		}
		return false
	}
	switch {
	case r == 0x85 || r == 0xa0 || r == 0x1680 || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000:
		return true
	case r >= 0x2000 && r <= 0x200a:
		return true
	}
	return false
}

var asciiRe = regexp.MustCompile(`[A-Za-z0-9]`)
var cjkRe = regexp.MustCompile(`[\x{4e00}-\x{9fff}\x{3400}-\x{4dbf}]`)

// _ENCODING_MARKERS: 编码可疑标记
var encodingMarkersRe = regexp.MustCompile(`[A-Za-z0-9+/]{20,}={0,2}|%[0-9a-fA-F]{2}|&#\d+;|&#x[0-9a-fA-F]+;|\\u[0-9a-fA-F]{4}|\\x[0-9a-fA-F]{2}|U\+[0-9a-fA-F]{4}`)

// base64 全匹配门（与 Python re.fullmatch 一致）
var base64FullRe = regexp.MustCompile(`^[A-Za-z0-9+/=\s]+$`)

// ──────────────────────────────────────────────────────────────
// L2 指纹：8 类真实载荷特征（Python FINGERPRINTS 原串）
// 数据由 testdata/sensitive_fingerprints.json 提供。
// ──────────────────────────────────────────────────────────────

//go:embed testdata/sensitive_fingerprints.json
var fingerprintFS embed.FS

type fingerprintRawEntry struct {
	name     string
	group    string
	patterns []string
}

var (
	fingerprintRawOnce sync.Once
	fingerprintRaw     []fingerprintRawEntry
)

func loadFingerprintRaw() []fingerprintRawEntry {
	fingerprintRawOnce.Do(func() {
		data, err := fingerprintFS.ReadFile("testdata/sensitive_fingerprints.json")
		if err != nil {
			common.SysError("fingerprints load failed: " + err.Error())
			return
		}
		var payload struct {
			Categories []struct {
				Name     string   `json:"name"`
				Group    string   `json:"group"`
				Patterns []string `json:"patterns"`
			} `json:"categories"`
		}
		if err := common.Unmarshal(data, &payload); err != nil {
			common.SysError("fingerprints parse failed: " + err.Error())
			return
		}
		for _, c := range payload.Categories {
			fingerprintRaw = append(fingerprintRaw, fingerprintRawEntry{
				name:     c.Name,
				group:    c.Group,
				patterns: c.Patterns,
			})
		}
	})
	return fingerprintRaw
}



type fingerprintCategory struct {
	name  string
	group string // gov/tech/rp：SensitiveBlockGroups 开关组
	atoms []fpAtom
}

// fpAtom 一个展开后的字面量指纹（小写）+ 词界要求。
// 匹配全文小写后的 text；词界按 Python \w（Unicode 字母/数字/下划线）。
// 不用正则逐字符机器（~20ns/字符），strings.Contains 是 ~0.5ns/字符。
type fpAtom struct {
	s         string
	leadWord  bool
	trailWord bool
}


var fingerprintCategories []fingerprintCategory

// ──────────────────────────────────────────────────────────────
// 单趟指纹原子机：一次 AC 扫描全部 8 类原子（替代逐原子 strings.Index 循环）。
// ──────────────────────────────────────────────────────────────

// fpFlatAtoms 把所有类别原子摊平；fpCatOf[i] 为原子所属类别。
var (
	fpFlatAtoms []fpAtom // i == 原子下标；text 匹配后按词界校验
	fpCatOf     []int    // 原子 → 类别下标
	fpAc        *byteAC  // 全原子单趟 AC（raw 构建：原子已小写、不裁剪）
)

func buildFingerprintAC() {
	if len(fingerprintCategories) == 0 {
		return
	}
	for ci, cat := range fingerprintCategories {
		for _, a := range cat.atoms {
			fpFlatAtoms = append(fpFlatAtoms, a)
			fpCatOf = append(fpCatOf, ci)
		}
	}
	words := make([]string, 0, len(fpFlatAtoms))
	for _, a := range fpFlatAtoms {
		words = append(words, a.s)
	}
	fpAc = buildByteAC(words, true)
}

// fpSearchPos 一次扫描返回所有命中（原子下标 + 结束字节位置），词界未校验。
func fpSearchPos(text string) ([]int32, []int32) {
	if fpAc == nil {
		return nil, nil
	}
	state := 0
	var idx, pos []int32
	for i := 0; i < len(text); i++ {
		state = int(fpAc.next[state][text[i]])
		if hits := fpAc.output[state]; len(hits) > 0 {
			idx = append(idx, hits...)
			for range hits {
				pos = append(pos, int32(i+1))
			}
		}
	}
	return idx, pos
}

// isUnicodeWordChar Python \w（Unicode 模式）：字母/数字/下划线。
func isUnicodeWordChar(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// matchFpAtom 小写文本中查找原子（含词界校验）。
func matchFpAtom(a fpAtom, text string) bool {
	offset := 0
	for {
		i := strings.Index(text[offset:], a.s)
		if i < 0 {
			return false
		}
		start := offset + i
		end := start + len(a.s)
		beforeOk := true
		if a.leadWord && start > 0 {
			prev, _ := utf8.DecodeLastRuneInString(text[:start])
			beforeOk = !isUnicodeWordChar(prev)
		}
		afterOk := true
		if a.trailWord && end < len(text) {
			next, _ := utf8.DecodeRuneInString(text[end:])
			afterOk = !isUnicodeWordChar(next)
		}
		if beforeOk && afterOk {
			return true
		}
		offset = start + 1
		if offset >= len(text) {
			return false
		}
	}
}

// expandAtoms 把含 (a|b|c)、(x)? 、s? 的受限指纹模式展开为确定性字面量集合。
// 仅支持本项目 8 类指纹中出现的形式；不支持时返回含原串的单原子（保守）。
func expandAtoms(pattern string) []string {
	if pattern == "" {
		return []string{""}
	}
	// (…) 组：先展开最靠前的未嵌套组
	if open := strings.IndexByte(pattern, '('); open >= 0 {
		depth, close := 0, -1
		for i := open; i < len(pattern); i++ {
			switch pattern[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					close = i
					i = len(pattern)
				}
			}
		}
		if close >= 0 {
			prefix := pattern[:open]
			inner := pattern[open+1 : close]
			suffix := pattern[close+1:]
			// (x)? 的可选号在组外
			optional := false
			if len(suffix) > 0 && suffix[0] == '?' {
				optional = true
				suffix = suffix[1:]
			} else if strings.HasSuffix(inner, "?") {
				optional = true
				inner = inner[:len(inner)-1]
			}
			var alts []string
			if strings.Contains(inner, "|") {
				alts = strings.Split(inner, "|")
			} else {
				alts = []string{inner}
			}
			if optional {
				alts = append(alts, "")
			}
			var out []string
			for _, a := range alts {
				for _, expanded := range expandAtoms(a) {
					for _, tail := range expandAtoms(suffix) {
						out = append(out, prefix+expanded+tail)
					}
				}
			}
			return out
		}
	}
	// (待) 在字面量中的单个字符量词 ?
	if i := strings.IndexByte(pattern, '?'); i >= 0 && i > 0 {
		head, base, tail := pattern[:i-1], string(pattern[i-1]), pattern[i+1:]
		var out []string
		for _, b := range []string{base, ""} {
			for _, t := range expandAtoms(tail) {
				out = append(out, head+b+t)
			}
		}
		return out
	}
	// 无 ? 无 (：单个字面量
	return []string{pattern}
}

// fpCategoryGroup 指纹 ∈ 开关组：tech（技术破甲）与 rp（角色扮演）。
// 与 Python FP_GROUPS 对齐；gov 组由词库层承担。
// fpCategoryGroup 指纹 → 开关组。由 init() 从 fingerprintRaw 各类别 group 字段填入。
var fpCategoryGroup = map[string]string{}

func init() {
	for _, raw := range fingerprintRaw {
		cat := fingerprintCategory{name: raw.name, group: fpCategoryGroup[raw.name]}
		// 类别内原子去重：跨 pattern 的重叠字面量与可选组展开的重复变体都只保留一份，
		// 避免 AC 自动机构建冗余路径（issue #380）。
		seen := make(map[string]struct{}, len(raw.patterns))
		for _, p := range raw.patterns {
			plain := strings.ReplaceAll(p, `\b`, "")
			for _, atom := range expandAtoms(plain) {
				if atom == "" {
					continue
				}
				s := strings.ToLower(atom)
				key := s
				if strings.HasPrefix(p, `\b`) {
					key = "\x00" + key
				}
				if strings.HasSuffix(p, `\b`) {
					key += "\x00"
				}
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				cat.atoms = append(cat.atoms, fpAtom{
					s:         s,
					leadWord:  strings.HasPrefix(p, `\b`),
					trailWord: strings.HasSuffix(p, `\b`),
				})
			}
		}
		fingerprintCategories = append(fingerprintCategories, cat)
	}
	buildFingerprintAC() // 依赖 fingerprintCategories 就绪，装一次性原子机
}

// fingerprintScore 评分：每类命中计 1（与 Python 相同，类别内短路）。
// 命中的类别若所在组（tech/rp）未启用则不计分。
func fingerprintScore(text string) (int, []string) {
	idx, pos := fpSearchPos(text)
	if len(idx) == 0 {
		return 0, nil
	}
	catHit := make([]bool, len(fingerprintCategories))
	for k, atomIdx := range idx {
		ci := fpCatOf[atomIdx]
		if catHit[ci] {
			continue
		}
		a := &fpFlatAtoms[atomIdx]
		end := int(pos[k])
		start := end - len(a.s)
		beforeOk := true
		if a.leadWord && start > 0 {
			prev, _ := utf8.DecodeLastRuneInString(text[:start])
			beforeOk = !isUnicodeWordChar(prev)
		}
		afterOk := true
		if a.trailWord && end < len(text) {
			next, _ := utf8.DecodeRuneInString(text[end:])
			afterOk = !isUnicodeWordChar(next)
		}
		if beforeOk && afterOk {
			catHit[ci] = true
		}
	}
	score := 0
	hits := make([]string, 0, 4)
	for ci, cat := range fingerprintCategories {
		if catHit[ci] && (cat.group == "" || setting.SensitiveGroupEnabled(cat.group)) {
			score++
			hits = append(hits, cat.name)
		}
	}
	return score, hits
}

// ──────────────────────────────────────────────────────────────
// L3 解码层（与 Python try_decode_layers 对齐）
// ──────────────────────────────────────────────────────────────

// decodeBase64Lenient 行为接近 Python base64.b64decode(validate=False)：
// 去空白、补 padding、非法字符放行到报错。
func decodeBase64Lenient(s string) ([]byte, error) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		b.WriteByte(c)
	}
	clean := b.String()
	if n := len(clean) % 4; n != 0 {
		clean += strings.Repeat("=", 4-n)
	}
	return base64.StdEncoding.DecodeString(clean)
}

// unquoteLenient 等价 Python urllib.parse.unquote：只解合法 %XX，非法原样保留，'+' 不解。
func unquoteLenient(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			hi, lo := hexVal(s[i+1]), hexVal(s[i+2])
			b.WriteByte(hi<<4 | lo)
			i += 3
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

func htmlUnescape(s string) string {
	return html.UnescapeString(s)
}

// unicodeEscapeDecode 近似 Python 的 text.encode().decode('unicode_escape')：
// 支持 \uXXXX、\UXXXXXXXX、\xNN、八进制 \NNN、常用控制转义；非法转义原样保留。
func unicodeEscapeDecode(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	runes := []rune(s)
	for i := 0; i < len(runes); {
		r := runes[i]
		if r != '\\' || i+1 >= len(runes) {
			b.WriteRune(r)
			i++
			continue
		}
		next := runes[i+1]
		switch next {
		case 'n':
			b.WriteRune('\n')
			i += 2
		case 't':
			b.WriteRune('\t')
			i += 2
		case 'r':
			b.WriteRune('\r')
			i += 2
		case 'b':
			b.WriteRune('\b')
			i += 2
		case 'f':
			b.WriteRune('\f')
			i += 2
		case 'v':
			b.WriteRune('\v')
			i += 2
		case 'a':
			b.WriteRune('\a')
			i += 2
		case '\\':
			b.WriteRune('\\')
			i += 2
		case '\'':
			b.WriteRune('\'')
			i += 2
		case '"':
			b.WriteRune('"')
			i += 2
		case 'u':
			if i+5 < len(runes) && isHexRune(runes[i+2]) && isHexRune(runes[i+3]) && isHexRune(runes[i+4]) && isHexRune(runes[i+5]) {
				b.WriteRune(rune(hexRunes(runes[i+2])<<12 | hexRunes(runes[i+3])<<8 | hexRunes(runes[i+4])<<4 | hexRunes(runes[i+5])))
				i += 6
			} else {
				b.WriteRune(next)
				i += 2
			}
		case 'U':
			if i+9 < len(runes) {
				v := 0
				ok := true
				for j := 2; j <= 9; j++ {
					if !isHexRune(runes[i+j]) {
						ok = false
						break
					}
					v = v<<4 | hexRunes(runes[i+j])
				}
				if ok {
					b.WriteRune(rune(v))
					i += 10
				} else {
					b.WriteRune(next)
					i += 2
				}
			} else {
				b.WriteRune(next)
				i += 2
			}
		case 'x':
			if i+3 < len(runes) && isHexRune(runes[i+2]) && isHexRune(runes[i+3]) {
				b.WriteRune(rune(hexRunes(runes[i+2])<<4 | hexRunes(runes[i+3])))
				i += 4
			} else {
				b.WriteRune(next)
				i += 2
			}
		default:
			if next >= '0' && next <= '7' {
				v := 0
				j := 0
				for ; j < 3 && i+1+j < len(runes) && runes[i+1+j] >= '0' && runes[i+1+j] <= '7'; j++ {
					v = v<<3 | int(runes[i+1+j]-'0')
				}
				b.WriteRune(rune(v))
				i += 1 + j
			} else {
				b.WriteRune(next)
				i += 2
			}
		}
	}
	return b.String()
}

func isHexRune(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func hexRunes(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10
	default:
		return int(r-'A') + 10
	}
}
