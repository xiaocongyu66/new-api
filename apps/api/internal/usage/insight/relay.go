package insight

import (
	"net/http"
	"strings"
)

// relaySignature 描述一个上游中转站项目的指纹。
// 中转站转发时通常会保留自身注入的请求头、或抹掉真实客户端 UA，
// 这些痕迹比 UA 本身更难伪造。
type relaySignature struct {
	Vendor      string
	HeaderKeys  []string
	HeaderPairs map[string]string
	UAMarkers   []string
	// Weight 为命中单条特征时累加的分数。
	Weight int
}

// relaySignatures 覆盖 new-api / one-api 系及常见二次转发项目。
var relaySignatures = []relaySignature{
	{
		Vendor: "new-api",
		HeaderKeys: []string{
			"X-Oneapi-Request-Id",
			"X-New-Api-User",
			"X-New-Api-Request-Id",
			"New-Api-User",
		},
		UAMarkers: []string{"new-api", "newapi"},
		Weight:    60,
	},
	{
		Vendor: "one-api",
		HeaderKeys: []string{
			"X-Oneapi-User",
			"One-Api-User",
			"X-Oneapi-Channel-Id",
		},
		UAMarkers: []string{"one-api", "oneapi"},
		Weight:    60,
	},
	{
		Vendor:      "sub2api",
		HeaderKeys:  []string{"X-Sub2api-Key", "X-Sub2api-Version", "X-Sub-Token"},
		HeaderPairs: map[string]string{"X-Powered-By": "sub2api"},
		UAMarkers:   []string{"sub2api", "sub2-api"},
		Weight:      65,
	},
	{
		Vendor:     "uni-api",
		HeaderKeys: []string{"X-Uni-Api-Key", "X-Uniapi-Provider"},
		UAMarkers:  []string{"uni-api", "uniapi"},
		Weight:     60,
	},
	{
		Vendor:     "veloera",
		HeaderKeys: []string{"X-Veloera-Request-Id", "X-Veloera-User"},
		UAMarkers:  []string{"veloera"},
		Weight:     60,
	},
	{
		Vendor:     "done-hub",
		HeaderKeys: []string{"X-Done-Hub-Request-Id", "X-Donehub-User"},
		UAMarkers:  []string{"done-hub", "donehub"},
		Weight:     60,
	},
	{
		Vendor:     "voapi",
		HeaderKeys: []string{"X-Voapi-Request-Id", "X-Vo-Api-User"},
		UAMarkers:  []string{"voapi"},
		Weight:     60,
	},
	{
		Vendor:     "cherry-relay",
		HeaderKeys: []string{"X-Relay-Provider", "X-Relay-Channel", "X-Relay-Upstream"},
		Weight:     45,
	},
	{
		Vendor:     "openai-proxy",
		HeaderKeys: []string{"X-Proxy-Provider", "X-Openai-Proxy", "X-Api-Proxy"},
		Weight:     40,
	},
}

// 通用代理链特征：单项不足以定性，但与"无客户端指纹"叠加后可判为中转。
var proxyChainHeaders = []string{
	"X-Forwarded-For",
	"X-Real-Ip",
	"X-Forwarded-Host",
	"X-Original-Forwarded-For",
	"Cf-Connecting-Ip",
	"X-Client-Ip",
}

// DetectRelay 判断请求是否来自另一个中转站，并给出厂商与判定依据。
// clientID 为空表示未能识别出真实客户端工具，这是中转站转发的重要旁证。
func DetectRelay(header http.Header, clientID string, clientKind string) (isRelay bool, vendor string, score int, reasons []string) {
	ua := strings.ToLower(header.Get("User-Agent"))
	for i := range relaySignatures {
		sig := &relaySignatures[i]
		for _, key := range sig.HeaderKeys {
			if header.Get(key) == "" {
				continue
			}
			score += sig.Weight
			vendor = sig.Vendor
			reasons = append(reasons, "header:"+key)
			break
		}
		for key, want := range sig.HeaderPairs {
			if strings.Contains(strings.ToLower(header.Get(key)), strings.ToLower(want)) {
				score += sig.Weight
				vendor = sig.Vendor
				reasons = append(reasons, "header:"+key+"="+want)
			}
		}
		if ua != "" && containsAny(ua, sig.UAMarkers) {
			score += sig.Weight
			vendor = sig.Vendor
			reasons = append(reasons, "ua:"+sig.Vendor)
		}
	}

	chainDepth := 0
	for _, key := range proxyChainHeaders {
		value := header.Get(key)
		if value == "" {
			continue
		}
		chainDepth++
		// X-Forwarded-For 里出现多跳说明请求至少穿过两层代理。
		if strings.Count(value, ",") >= 1 {
			chainDepth++
		}
	}
	// 代理链越深越可疑：单层反代（本站自己的 nginx）很常见，
	// 但三层以上通常意味着请求先经过了另一个网关。
	if chainDepth >= 2 {
		score += 20
		reasons = append(reasons, "proxy_chain_depth")
	}
	if chainDepth >= 3 {
		score += 10
		reasons = append(reasons, "deep_proxy_chain")
	}

	// 中转站常把客户端 UA 换成自己的 HTTP 库，甚至直接不带 UA。
	// 正规客户端（SDK、IDE 插件、CLI）都会带 UA，缺失本身就是强信号。
	if ua == "" {
		score += 25
		reasons = append(reasons, "missing_user_agent")
	} else if clientID == "" || clientKind == KindSDK {
		if chainDepth >= 1 {
			// UA 是通用 HTTP 库且请求经过代理：可能是中转站，也可能是用户自写脚本。
			// 单独不足以定性，需要与鉴权头异常等信号叠加。
			score += 15
			reasons = append(reasons, "no_client_fingerprint_behind_proxy")
		}
	}

	// 上游中转站为了兼容多协议，常同时塞入 OpenAI 与 Anthropic 两套鉴权头。
	// 真实客户端只会带自己协议对应的那一个。
	if header.Get("Authorization") != "" && header.Get("X-Api-Key") != "" {
		score += 35
		reasons = append(reasons, "dual_auth_headers")
	}

	if score > 100 {
		score = 100
	}
	if score >= 50 {
		if vendor == "" {
			vendor = "unknown"
		}
		return true, vendor, score, reasons
	}
	if vendor == "" && len(reasons) == 0 {
		return false, "", 0, nil
	}
	return false, vendor, score, reasons
}
