package service

import (
	"regexp"
	"strings"
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

// defaultTargetDomains 内置目标名单（后缀匹配）。任何包含于文本中的
// 完整域名（或子域）命中即视为目标。写入顺序不影响结果。
var defaultTargetDomains = []string{
	// 中国政府网及政务站点
	"gov.cn",
	// 中国军方站点
	"81.cn",
	// 政府机关常用缩写（覆盖 1-3 级子域：如 www.xxx.gov.cn 已匹配，无需额外）
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
func CheckSensitiveTargets(text string) string {
	if text == "" {
		return ""
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
