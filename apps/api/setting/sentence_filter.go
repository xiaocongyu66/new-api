package setting

import "strings"

// 短泛词过滤：SensitiveWordsFromString 在生产路径剔除易误伤的 2 字泛词
// （"政府""中央""军事"等），攻击意图明确的具名词保留（单独白名单）。
//
// 测试 fixture（sensitive_words_test.json）是 Python 基线锚，保持原样，
// 过滤只作用于用户配置的生产词库——基线回归与生产行为解耦。
var attackShortWords = map[string]struct{}{
	"破甲": {}, "越狱": {}, "脱狱": {}, "外挂": {}, "黑客": {},
	"炸弹": {}, "手枪": {}, "步枪": {}, "冰毒": {}, "毒品": {},
	"弹药": {}, "洗钱": {}, "军火": {}, "枪械": {}, "暴恐": {},
}

// FilterSensitiveWords 过滤易误伤短词（保留攻击词白名单）。
// 长度按 rune 判定；仅剔除 2 字词（2 字以内），1 字词一并剔除
// （单字命中率极高，无攻击词价值）。
func FilterSensitiveWords(words []string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		n := 0
		for range w {
			n++
		}
		if n > 2 {
			out = append(out, w)
			continue
		}
		if _, ok := attackShortWords[w]; ok {
			out = append(out, w)
		}
	}
	return out
}

func SensitiveWordsFromString(s string) {
	SensitiveWords = []string{}
	sw := strings.Split(s, "\n")
	for _, w := range sw {
		w = strings.TrimSpace(w)
		if w != "" {
			SensitiveWords = append(SensitiveWords, w)
		}
	}
	SensitiveWords = FilterSensitiveWords(SensitiveWords)
}