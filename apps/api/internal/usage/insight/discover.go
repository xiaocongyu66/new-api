package insight

import (
	"net/http"
	"regexp"
	"strings"
)

// 本文件把"没有内置规则的客户端"也变成可封禁的具体对象。
//
// 为什么需要：内置 clientRules 是发布期常量，新工具（deepseek-harness、
// ohmyagent 这类）出现后只能落 unknown，运营方在封禁界面里看不到它们，
// 必须等下一次改代码加规则。自动发现从真实流量里提取标识，
// 让选项列表随流量自己长出来。
//
// 判定依据是 UA **与**专属请求头两者：单看 UA 会把同一 HTTP 栈的不同工具
// 混成一个（都叫 okhttp），而工具专属头（X-Foo-Version 之类）恰恰是
// 区分它们的强信号，也是 UA 被中转站改写后唯一残留的线索。

// DiscoveredClientID 是自动发现客户端的 ID 前缀。
// 加前缀是为了让封禁列表里的自动项与内置规则 ID 永不冲突，
// 也方便 UI 单独分组展示。
const DiscoveredClientID = "ua:"

// maxDiscoveredTokenLen 限制标识长度：它会进数据库列与配置列表，
// 且长 UA 的尾部通常是与工具无关的平台信息。
const maxDiscoveredTokenLen = 32

// discoveredVendorHeaderRe 匹配"工具专属请求头"的形状：X-<Vendor>-*。
// 这类头是工具自己加的，比 UA 更难被中转站抹掉。
var discoveredVendorHeaderRe = regexp.MustCompile(`^x-([a-z0-9]{2,20})-`)

// uaProductRe 取 UA 的第一个 product token（"name/version" 里的 name）。
// 绝大多数客户端 UA 都以自己的名字开头，后面才是平台/引擎信息。
var uaProductRe = regexp.MustCompile(`^([a-z0-9][a-z0-9._+-]{1,31})`)

// discoveredHeaderDenyList 是不能用来当"工具专属头"的前缀：
// 它们来自中转站、CDN、代理或标准协议，不代表调用方工具，
// 用它们生成标识会把无关流量归成同一个"客户端"。
var discoveredHeaderDenyList = map[string]bool{
	"forwarded":   true,
	"real":        true,
	"request":     true,
	"requested":   true,
	"correlation": true,
	"trace":       true,
	"amz":         true,
	"amzn":        true,
	"aws":         true,
	"goog":        true,
	"google":      true,
	"azure":       true,
	"ms":          true,
	"cache":       true,
	"cdn":         true,
	"cf":          true,
	"envoy":       true,
	"nginx":       true,
	"varnish":     true,
	"proxy":       true,
	"relay":       true,
	"upstream":    true,
	"api":         true,
	"auth":        true,
	"csrf":        true,
	"xsrf":        true,
	"content":     true,
	"accept":      true,
	"frame":       true,
	"scheme":      true,
	"host":        true,
	"port":        true,
	"for":         true,
	"stainless":   true,
	"ratelimit":   true,
	"rate":        true,
	"title":       true,
	"session":     true,
	"user":        true,
	"client":      true,
	"device":      true,
	"platform":    true,
	"lang":        true,
	"locale":      true,
	"timezone":    true,
	// 本网关自己的转发标识头：relay 下来的请求会带着 X-Oneapi-Request-Id
	// （常见于 new-api 系中继链），它来自中转站而不是调用方工具，
	// 绝不能用来生成"客户端"标识，否则整条中继链的流量会被归成一个客户端。
	"oneapi": true,
	"newapi": true,
}

// ResolveClient 给出一次请求最终的客户端标识，是画像与封禁共用的入口。
//
// 顺序：内置规则优先；规则没命中时走自动发现；命中的规则若只是通用
// HTTP 栈（curl / okhttp / node-fetch 之类）而请求又带着工具专属头，
// 则以自动发现结果为准——这类 UA 只说明"用什么库发的请求"，
// 同一个库背后可能是十几个不同工具，专属头才是区分它们的依据。
func ResolveClient(header http.Header, prompt string) (id, name, kind, version, source string, score int) {
	id, name, kind, version, source, score = DetectClient(header, prompt)
	if id != "" && kind != KindHTTPTool {
		return id, name, kind, version, source, score
	}
	// 通用 HTTP 栈：仅当确实存在工具专属头时才细分，否则保留 curl/okhttp
	// 这样的具体结论（它本身就是有用的分类）。
	if id != "" && discoverVendorHeader(header) == "" {
		return id, name, kind, version, source, score
	}
	if discoveredID, discoveredName := DiscoverClient(header); discoveredID != "" {
		return discoveredID, discoveredName, KindDiscovered, "", "header", 60
	}
	return id, name, kind, version, source, score
}

// DiscoverClient 从请求头推导一个"未内置规则客户端"的稳定标识。
//
// 返回值形如 "ua:deepseek-harness" 或 "ua:myagent+x-myagent-version"：
// 前半段来自 UA 的 product token，后半段是命中的工具专属请求头。
// 两者都取不到时返回空串——宁可留在 unknown，也不要造一个
// 把所有匿名流量混在一起的假客户端（那样封禁会一封一片）。
func DiscoverClient(header http.Header) (id, name string) {
	uaToken := discoverUAToken(header.Get("User-Agent"))
	vendorHeader := discoverVendorHeader(header)

	switch {
	case uaToken != "" && vendorHeader != "":
		// UA 与专属头都在时一起参与标识：同一个 HTTP 栈（okhttp）下
		// 带不同专属头的工具会被区分成不同客户端，这正是所需粒度。
		// 长度上界：token ≤32、vendor 段 ≤20（正则保证），组合 ≤53，
		// 低于封禁表 client 列的 64 字符宽。
		return DiscoveredClientID + uaToken + "+" + vendorHeader, uaToken + " (" + vendorHeader + ")"
	case uaToken != "":
		return DiscoveredClientID + uaToken, uaToken
	case vendorHeader != "":
		return DiscoveredClientID + vendorHeader, vendorHeader
	default:
		return "", ""
	}
}

// discoverUAToken 提取并归一化 UA 的首个 product token。
func discoverUAToken(ua string) string {
	ua = strings.ToLower(strings.TrimSpace(ua))
	if ua == "" || ua == "-" {
		return ""
	}
	// 版本号不进标识：否则每次工具升级都会长出一个新"客户端"，
	// 封禁配置会被同一工具的十几个版本刷满。
	product := ua
	if idx := strings.IndexAny(product, " \t"); idx > 0 {
		product = product[:idx]
	}
	if idx := strings.Index(product, "/"); idx > 0 {
		product = product[:idx]
	}
	match := uaProductRe.FindString(product)
	if match == "" {
		return ""
	}
	match = strings.Trim(match, ".-_+")
	// 纯数字或过短的 token 不具备识别价值。
	if len(match) < 3 || strings.IndexFunc(match, isASCIILetter) < 0 {
		return ""
	}
	if len(match) > maxDiscoveredTokenLen {
		match = match[:maxDiscoveredTokenLen]
	}
	return match
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// discoverVendorHeader 找出请求里最可能代表调用方工具的专属请求头，
// 返回形如 "x-<vendor>" 的短标识（vendor 段 ≤20 字符，由正则保证）。
// 不返回完整头名：完整名会把版本后缀等变体带进标识，还可能与
// maxDiscoveredTokenLen 截断交互产生碰撞。命中多个时取字典序最小的
// 一个，保证同一工具每次得到同一标识。头值为空的宿主头不算命中。
func discoverVendorHeader(header http.Header) string {
	best := ""
	for key, values := range header {
		if len(values) == 0 || values[0] == "" {
			continue
		}
		lower := strings.ToLower(key)
		match := discoveredVendorHeaderRe.FindStringSubmatch(lower)
		if match == nil {
			continue
		}
		vendor := match[1]
		if discoveredHeaderDenyList[vendor] {
			continue
		}
		// 已被内置规则用作识别依据的头不参与自动发现：
		// 那些请求本就会命中具体规则，不该再生成一个并行标识。
		if builtinHeaderVendors[vendor] {
			continue
		}
		identity := "x-" + vendor
		if best == "" || identity < best {
			best = identity
		}
	}
	return best
}

// builtinHeaderVendors 是内置规则已经使用的请求头 vendor 段，
// 在包初始化时从 clientRules 派生，避免与规则表手工同步而漂移。
var builtinHeaderVendors = func() map[string]bool {
	vendors := map[string]bool{}
	collect := func(key string) {
		if match := discoveredVendorHeaderRe.FindStringSubmatch(strings.ToLower(key)); match != nil {
			vendors[match[1]] = true
		}
	}
	for i := range clientRules {
		for _, key := range clientRules[i].HeaderKeys {
			collect(key)
		}
		for key := range clientRules[i].HeaderPairs {
			collect(key)
		}
	}
	return vendors
}()

// IsDiscoveredClient 判断一个客户端 ID 是否来自自动发现。
func IsDiscoveredClient(id string) bool {
	return strings.HasPrefix(id, DiscoveredClientID)
}

// DiscoveredClientName 把标识还原成界面上的显示名。
// 自动发现项去掉 "ua:" 前缀并把联合标识写成易读形式；
// 其它 ID（例如规则被删后遗留的历史值）原样返回。
func DiscoveredClientName(id string) string {
	if !IsDiscoveredClient(id) {
		return id
	}
	token := strings.TrimPrefix(id, DiscoveredClientID)
	if ua, vendorHeader, ok := strings.Cut(token, "+"); ok {
		return ua + " (" + vendorHeader + ")"
	}
	return token
}
