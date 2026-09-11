package insight

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ResolveClient 是画像与封禁中间件共用的判定入口，封禁配置里勾选的标识
// 必须与它算出的完全一致，否则勾了也拦不住。本表锁定该契约。
func TestResolveClientIdentity(t *testing.T) {
	cases := []struct {
		name     string
		header   http.Header
		wantID   string
		wantKind string
	}{
		{
			name:     "builtin rule wins over discovery",
			header:   http.Header{"User-Agent": {"claude-cli/1.0.58 (external, cli)"}},
			wantID:   "claude_code",
			wantKind: KindAgentCLI,
		},
		{
			// HTTP 工具已按具体栈拆分，封禁粒度要能只封 curl 不封 okhttp。
			name:     "curl stays its own client",
			header:   http.Header{"User-Agent": {"curl/8.5.0"}},
			wantID:   "curl",
			wantKind: KindHTTPTool,
		},
		{
			name:     "okhttp stays its own client",
			header:   http.Header{"User-Agent": {"okhttp/4.12.0"}},
			wantID:   "okhttp",
			wantKind: KindHTTPTool,
		},
		{
			// 关键契约：通用 HTTP 栈只说明"用什么库发的请求"，同一个库背后
			// 可能是多个工具，带工具专属头时必须按专属头细分，否则封 okhttp
			// 会连带封掉所有安卓 App 的内置栈流量。
			name:     "http stack plus vendor header splits into its own identity",
			header:   http.Header{"User-Agent": {"okhttp/4.12.0"}, "X-Sillybot-Build": {"9"}},
			wantID:   "ua:okhttp+x-sillybot",
			wantKind: KindDiscovered,
		},
		{
			// 未内置规则的新工具（生产流量里的 deepseek-harness、ohmyagent
			// 属于这一类）必须自动获得标识，才能出现在封禁选项里。
			name:     "unknown tool is discovered from ua",
			header:   http.Header{"User-Agent": {"deepseek-harness/0.1.1-rc.2 (+https://github.com/deepseek-ai/deepseek-harness)"}},
			wantID:   "ua:deepseek-harness",
			wantKind: KindDiscovered,
		},
		{
			name:     "version is excluded so upgrades do not create new clients",
			header:   http.Header{"User-Agent": {"deepseek-harness/9.9.9"}},
			wantID:   "ua:deepseek-harness",
			wantKind: KindDiscovered,
		},
		{
			name:     "vendor header alone is enough when ua is absent",
			header:   http.Header{"X-Weirdtool-Id": {"abc"}},
			wantID:   "ua:x-weirdtool",
			wantKind: KindDiscovered,
		},
		{
			// 反例：代理/CDN/追踪头不代表调用方工具，用它们生成标识会把
			// 无关流量归成同一个"客户端"，封禁就会一封一片。
			name: "proxy and tracing headers never form an identity",
			header: http.Header{
				"X-Forwarded-For": {"1.2.3.4"},
				"X-Real-Ip":       {"1.2.3.4"},
				"X-Request-Id":    {"r1"},
				"X-Trace-Id":      {"t1"},
			},
			wantID:   "",
			wantKind: KindUnknown,
		},
		{
			name:     "missing user agent yields no identity",
			header:   http.Header{},
			wantID:   "",
			wantKind: KindUnknown,
		},
		{
			name:     "dash user agent yields no identity",
			header:   http.Header{"User-Agent": {"-"}},
			wantID:   "",
			wantKind: KindUnknown,
		},
		{
			// 手机端 App 与浏览器兜底必须留在各自分类里，
			// 它们在封禁界面是不同的处置对象。
			name:     "mobile app keeps its own kind",
			header:   http.Header{"User-Agent": {"RikkaHub-Android/2.4.5"}},
			wantID:   "rikkahub",
			wantKind: KindMobile,
		},
		{
			name:     "browser fallback keeps browser kind",
			header:   http.Header{"User-Agent": {"Mozilla/5.0 (Linux; Android 15) AppleWebKit/537.36"}},
			wantID:   "browser",
			wantKind: KindBrowser,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, _, kind, _, _, _ := ResolveClient(tc.header, "")
			assert.Equal(t, tc.wantID, id)
			assert.Equal(t, tc.wantKind, kind)
		})
	}
}

// 自动发现标识进入封禁列表与数据库列，前端也要能把它还原成可读名字。
func TestDiscoveredClientName(t *testing.T) {
	assert.Equal(t, "deepseek-harness", DiscoveredClientName("ua:deepseek-harness"))
	assert.Equal(t, "okhttp (x-sillybot)", DiscoveredClientName("ua:okhttp+x-sillybot"))
	// 内置 ID 不带前缀，原样返回，避免被误加工。
	assert.Equal(t, "claude_code", DiscoveredClientName("claude_code"))
}

// 内置规则已用作识别依据的请求头不参与自动发现，否则同一个请求会既命中
// 规则又生成一个并行标识，封禁列表里出现两个指向同一工具的选项。
func TestBuiltinHeaderVendorsAreExcludedFromDiscovery(t *testing.T) {
	assert.True(t, builtinHeaderVendors["cursor"])
	assert.True(t, builtinHeaderVendors["opencode"])
	assert.Empty(t, discoverVendorHeader(http.Header{"X-Cursor-Checksum": {"abc"}}))
}
