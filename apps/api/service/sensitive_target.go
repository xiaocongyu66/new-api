package service

import (
	"embed"
	"regexp"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// 目标域名黑名单——防攻击目标的硬闸门。
//
// 用户诉求：请求/响应中一旦出现政府目标站点（或其子域），立即终止整条链路，
// 不交给词库/指纹/模型判断。命中即代表攻击目标明确。
//
// 匹配规则：
//   - 大小写不敏感
//   - 通配子域：`gov.cn` 命中 `www.gov.cn`、`abc.gov.cn`、`gov.cn` 本身
//   - 支持裸域名（`gov.cn`）、URL（`https://www.gov.cn/xx`）、
//     括号引用（`(gov.cn)`）、路径段中的域名（`www.gov.cn/index`）
//   - 端口与路径不参与匹配；IDN（punycode）不展开

//go:embed testdata/sensitive_target_domains.json
var defaultTargetDomainsFS embed.FS

var (
	defaultTargetDomainsOnce sync.Once
	defaultTargetDomains     []string
)

// loadDefaultTargetDomains 从 fixture 加载内置目标名单（后缀匹配）。
// 任何包含于文本中的完整域名（或子域）命中即视为目标。写入顺序不影响结果。
func loadDefaultTargetDomains() []string {
	defaultTargetDomainsOnce.Do(func() {
		data, err := defaultTargetDomainsFS.ReadFile("testdata/sensitive_target_domains.json")
		if err != nil {
			common.SysError("target domains load failed: " + err.Error())
			return
		}
		var payload struct {
			Domains []string `json:"domains"`
		}
		if err := common.Unmarshal(data, &payload); err != nil {
			common.SysError("target domains parse failed: " + err.Error())
			return
		}
		defaultTargetDomains = payload.Domains
	})
	return defaultTargetDomains
}

func init() {
	loadDefaultTargetDomains() // 启动即加载，避免首请求触发解析
}

// targetDomainPattern 从文本提取“合法域名候选”。
// 允许的形式：
//   - scheme://host[/path]（http/https/ftp）
//   - 裸 host 段（连续字母数字与 . 和 - 形成的域名状串）
var targetDomainPattern = regexp.MustCompile(`(?i)(?:https?://|ftp://)?([a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+)`)

// IsTargetDomain 判断 domain（host-only）是否命中目标名单（含子域）。
func IsTargetDomain(domain string) bool {
	d := lowerNoWWW(domain)
	for _, t := range defaultTargetDomains {
		if d == t || strings.HasSuffix(d, "."+t) {
			return true
		}
	}
	return false
}

func lowerNoWWW(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".")
	// 去掉托管层：www. 前缀（不影响目标域本身匹配）
	s = strings.TrimPrefix(s, "www.")
	return s
}

// CheckSensitiveTargets 在文本中查找目标域。命中返回被命中的域名（最长者），
// 未命中返回空串。
//
// 入口先做同形折叠：西里尔 о(U+043E)/希腊 ο(U+03BF)→o、全角 ｇ(U+FF47)→g 等，
// 防 gоv.cn / ｇοv.cn / ｇov.cn 变体绕过 `[a-z0-9]` 域名正则逃逸硬闸。
// 折叠保留点号与字母数字（不剥分隔符，域名结构完整）；无折叠字符时零分配原串直通。
func CheckSensitiveTargets(text string) string {
	if text == "" {
		return ""
	}
	var folded bool
	for _, r := range text {
		if _, ok := homoglyphMap[r]; ok {
			folded = true
			break
		}
		if r >= 0xff01 && r <= 0xff5e {
			folded = true
			break
		}
	}
	if folded {
		var b strings.Builder
		b.Grow(len(text))
		for _, r := range text {
			if r >= 0xff01 && r <= 0xff5e {
				r -= 0xfee0 // 全角 ASCII → 半角
			} else if m, ok := homoglyphMap[r]; ok {
				r = m
			}
			b.WriteRune(r)
		}
		text = b.String()
	}
	best := ""
	for _, m := range targetDomainPattern.FindAllStringSubmatch(text, -1) {
		if len(m) < 2 || m[1] == "" {
			continue
		}
		host := m[1]
		// 去掉尾部点半径（正则已限制）；按名字判断
		if IsTargetDomain(host) {
			if len(host) > len(best) {
				best = host
			}
		}
	}
	return best
}

// 输出检测总开关由 setting.ShouldCheckCompletionSensitive() 承担
// （默认开启，用户要求输出默认 block）。
