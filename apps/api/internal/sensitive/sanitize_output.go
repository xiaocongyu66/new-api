package sensitive

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

// 敏感内容静默过滤：把目标域名与词库敏感词从文本中删除，替代旧的「命中即拦」。
// 匹配在折叠视图（小写 + 全角→半角 + 同形字归一）上进行，与检测引擎共用同一套
// 折叠表，ｇｏｖ．ｃｎ / gоv.cn 等变体同样被切除。
//
// ponytail: 分隔符插入（g o v . c n）、JSON \uXXXX 转义载荷不在过滤范围——
// 过滤器漏放只是内容透传（不再有安全语义），需要更强覆盖时给 AC 机加位置输出。
//
// 过滤入口：
//   - 输入侧 SanitizeRelayRequest：OpenAI/Claude 请求逐消息净化后照常转发；
//   - 输出侧 relay/helper 持续调用 SanitizeSensitiveText 做 chunk 净化。

// hostCandidatePattern 提取域名状 token（含 IP 样式误报，由 IsTargetDomain 过滤）。
var hostCandidatePattern = regexp.MustCompile(`(?i)[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+`)

// foldRune 单 rune 折叠：全角→半角、同形字→拉丁、小写。
func foldRune(r rune) rune {
	if r >= 0xff01 && r <= 0xff5e {
		return r - 0xfee0
	}
	if m, ok := homoglyphMap[r]; ok {
		return m
	}
	return unicode.ToLower(r)
}

// foldView 返回 text 的折叠副本，以及 folded 每个字节对应的原文字节偏移。
// starts 长度 == len(folded)，末尾附哨兵 len(text) 便于取结束偏移。
func foldView(text string) (string, []int) {
	var b strings.Builder
	b.Grow(len(text))
	starts := make([]int, 0, len(text))
	var tmp [utf8.UTFMax]byte
	for i, r := range text {
		n := utf8.EncodeRune(tmp[:], foldRune(r))
		for _, by := range tmp[:n] {
			b.WriteByte(by)
			starts = append(starts, i) // 每个输出字节都映射回源 rune 偏移
		}
	}
	return b.String(), append(starts, len(text))
}

// cutRanges 按 [start,end) 对删除文本片段；重叠区间合并。
func cutRanges(text string, rs [][2]int) string {
	sort.Slice(rs, func(i, j int) bool { return rs[i][0] < rs[j][0] })
	var b strings.Builder
	b.Grow(len(text))
	pos := 0
	for _, r := range rs {
		s, e := r[0], r[1]
		if s < pos {
			s = pos
		}
		if e <= s {
			continue
		}
		b.WriteString(text[pos:s])
		pos = e
	}
	b.WriteString(text[pos:])
	return b.String()
}

// targetHitRanges 在折叠视图上找目标域命中，映射回原文字节区间。
func targetHitRanges(folded string, starts []int) ([][2]int, []string) {
	locs := hostCandidatePattern.FindAllStringIndex(folded, -1)
	if len(locs) == 0 {
		return nil, nil
	}
	var rs [][2]int
	var hits []string
	seen := make(map[string]struct{})
	for _, loc := range locs {
		cand := folded[loc[0]:loc[1]]
		if !IsTargetDomain(cand) {
			continue
		}
		rs = append(rs, [2]int{starts[loc[0]], starts[loc[1]]})
		d := strings.ToLower(cand)
		if _, ok := seen[d]; !ok {
			seen[d] = struct{}{}
			hits = append(hits, "target:"+d)
		}
	}
	return rs, hits
}

// dictHitRanges 在折叠视图上找词库敏感词命中，映射回原文字节区间。
func dictHitRanges(folded string, starts []int) ([][2]int, []string) {
	if len(SensitiveWords) == 0 {
		return nil, nil
	}
	var rs [][2]int
	var hits []string
	for _, w := range SensitiveWords {
		lw := strings.ToLower(w)
		if lw == "" {
			continue
		}
		found := false
		off := 0
		for {
			j := strings.Index(folded[off:], lw)
			if j < 0 {
				break
			}
			a := off + j
			rs = append(rs, [2]int{starts[a], starts[a+len(lw)]})
			off = a + len(lw)
			found = true
		}
		if found {
			hits = append(hits, w)
		}
	}
	return rs, hits
}

// sanitizeText 用给定的过滤组合净化文本；无命中时原样返回（零分配热路径）。
func sanitizeText(text string, withDict bool) (string, []string) {
	if text == "" {
		return text, nil
	}
	folded, starts := foldView(text)
	rs, hits := targetHitRanges(folded, starts)
	if withDict {
		drs, dhits := dictHitRanges(folded, starts)
		rs = append(rs, drs...)
		hits = append(hits, dhits...)
	}
	if len(rs) == 0 {
		return text, nil
	}
	return cutRanges(text, rs), hits
}

// SanitizeSensitiveText 删除 text 中的目标域名与词库敏感词，返回净化文本与
// 命中标签（"target:gov.cn"、词库裸词）。
func SanitizeSensitiveText(text string) (string, []string) {
	return sanitizeText(text, true)
}

// SanitizeTargetDomains 只切除目标域名，不动词库词。
// 输出侧在词库过滤开关关闭时仍要保证目标域不透传（与旧硬闸语义对齐）。
func SanitizeTargetDomains(text string) (string, []string) {
	return sanitizeText(text, false)
}

// TargetDomains 暴露内置目标域名单（输出过滤器计算 holdback 长度用）。
func TargetDomains() []string { return loadDefaultTargetDomains() }

// SanitizeRelayRequest 原地净化聊天请求中的字符串消息文本。
// 覆盖 OpenAI 与 Claude 两种主链路格式；媒体数组 content 与其余格式暂不处理。
// 返回命中的全部标签；无命中时请求体保持不变。
func SanitizeRelayRequest(request dto.Request) []string {
	var labels []string
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		for i := range req.Messages {
			m := &req.Messages[i]
			if !m.IsStringContent() {
				continue
			}
			cleaned, l := SanitizeSensitiveText(m.StringContent())
			if len(l) > 0 {
				m.SetStringContent(cleaned)
				labels = append(labels, l...)
			}
		}
	case *dto.ClaudeRequest:
		if s := req.GetStringSystem(); s != "" {
			cleaned, l := SanitizeSensitiveText(s)
			if len(l) > 0 {
				req.System = cleaned
				labels = append(labels, l...)
			}
		}
		for i := range req.Messages {
			m := &req.Messages[i]
			if !m.IsStringContent() {
				continue
			}
			cleaned, l := SanitizeSensitiveText(m.GetStringContent())
			if len(l) > 0 {
				m.SetStringContent(cleaned)
				labels = append(labels, l...)
			}
		}
	}
	return labels
}

// sanitizeRanges 同 sanitizeText，但额外返回原文字节坐标系中的切除区间
// （升序、不重叠），供流式输出做发射边界映射。
func sanitizeRanges(text string, withDict bool) (string, []string, [][2]int) {
	if text == "" {
		return text, nil, nil
	}
	folded, starts := foldView(text)
	rs, hits := targetHitRanges(folded, starts)
	if withDict {
		drs, dhits := dictHitRanges(folded, starts)
		rs = append(rs, drs...)
		hits = append(hits, dhits...)
	}
	if len(rs) == 0 {
		return text, nil, nil
	}
	return cutRanges(text, rs), hits, rs
}

// SanitizeSensitiveTextRanges 词库+目标域版本，见 sanitizeRanges。
func SanitizeSensitiveTextRanges(text string) (string, []string, [][2]int) {
	return sanitizeRanges(text, true)
}

// SanitizeTargetDomainsRanges 仅目标域版本，见 sanitizeRanges。
func SanitizeTargetDomainsRanges(text string) (string, []string, [][2]int) {
	return sanitizeRanges(text, false)
}
