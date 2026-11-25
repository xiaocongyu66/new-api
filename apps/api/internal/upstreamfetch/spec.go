package upstreamfetch

// 通用上游取数引擎。
//
// 上游没有统一接口，但「怎么取」可以归纳成三档，从便宜到贵：
//
//	api      公开/半公开的 JSON 接口，声明 rows/key/values 路径即可
//	scrape   服务端渲染的 HTML，声明 CSS 选择器即可
//	browser  需要执行 JS 或登录态的页面，交给外部适配器（Playwright 等）
//
// 三档产出同一种 Row：{key, values{字段→文本}}。价格与可用性共用这一层，
// 所以新增一个上游只是写一份 Spec，不用改代码。
//
// 关键约束：任何一档拿到的数据都要过校验（必需字段、数值范围），
// 校验不过就整次失败。上游改版时宁可没数据，也不能写进错数字。

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
)

// Strategy 是取数方式。
type Strategy string

const (
	StrategyAPI     Strategy = "api"
	StrategyScrape  Strategy = "scrape"
	StrategyBrowser Strategy = "browser"
)

// AuthMode 是鉴权方式。
type AuthMode string

const (
	// AuthNone 匿名访问。
	AuthNone AuthMode = "none"
	// AuthChannelKey 用渠道自己的中转密钥，Authorization: Bearer <key>。
	AuthChannelKey AuthMode = "channel_key"
	// AuthBearer 用显式配置的 token，Authorization: Bearer <token>。
	AuthBearer AuthMode = "bearer"
	// AuthHeader 用显式配置的自定义请求头。
	AuthHeader AuthMode = "header"
)

// Auth 描述怎么给请求带上凭证。
type Auth struct {
	Mode   AuthMode `json:"mode"`
	Token  string   `json:"token,omitempty"`
	Header string   `json:"header,omitempty"`
}

// Spec 是一份声明式取数配置。
//
// Rows/Key/Values 的含义按 Strategy 变化：
//   - api：Rows 是 JSON 路径（如 data.list），Key/Values 是行内字段名
//   - scrape/browser：Rows 是 CSS 选择器，Key/Values 是行内 CSS 选择器
type Spec struct {
	Strategy   Strategy          `json:"strategy"`
	URL        string            `json:"url"`
	Auth       Auth              `json:"auth,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	TimeoutSec int               `json:"timeout_seconds,omitempty"`
	Rows       string            `json:"rows"`
	Key        string            `json:"key"`
	Values     map[string]string `json:"values"`

	// Numeric 列出必须能解析成数字的 values 字段；配合 Min/Max 做边界校验。
	// 价格与成功率都靠这个拦住「页面改版后抓到一句提示词」这类事故。
	Numeric []string `json:"numeric,omitempty"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`

	// AdapterCmd 仅 browser 使用：一个读 stdin、写 stdout 的外部程序。
	// 主程序因此不需要引入浏览器依赖，Playwright/chromedp 可以独立部署。
	AdapterCmd  string   `json:"adapter_cmd,omitempty"`
	AdapterArgs []string `json:"adapter_args,omitempty"`
}

// Row 是取数结果的一行：一个 key（模型名、分组名……）加若干字段。
type Row struct {
	Key    string            `json:"key"`
	Values map[string]string `json:"values"`
}

// Result 是一次取数。
type Result struct {
	Strategy Strategy
	Rows     []Row
}

// Validate 检查 Spec 自身是否完整。配置错误应该在建 Spec 时就报出来，
// 而不是等到线上取数时才发现少了路径。
func (s Spec) Validate() error {
	switch s.Strategy {
	case StrategyAPI, StrategyScrape, StrategyBrowser:
	default:
		return fmt.Errorf("unknown strategy %q", s.Strategy)
	}
	if strings.TrimSpace(s.URL) == "" {
		return fmt.Errorf("spec has no url")
	}
	if strings.TrimSpace(s.Rows) == "" {
		return fmt.Errorf("spec has no rows path/selector")
	}
	if strings.TrimSpace(s.Key) == "" {
		return fmt.Errorf("spec has no key field/selector")
	}
	if len(s.Values) == 0 {
		return fmt.Errorf("spec has no values")
	}
	if s.Strategy == StrategyBrowser && strings.TrimSpace(s.AdapterCmd) == "" {
		return fmt.Errorf("browser strategy requires adapter_cmd")
	}
	// 鉴权方式必须显式落在枚举里：ApplyAuth 的 switch 没有 default 分支，
	// 拼错的 mode 会静默退化成匿名请求，把半公开页面当成渠道数据写进库。
	switch s.Auth.Mode {
	case "", AuthNone, AuthChannelKey, AuthBearer, AuthHeader:
	default:
		return fmt.Errorf("unknown auth mode %q", s.Auth.Mode)
	}
	if s.Auth.Mode == AuthBearer && strings.TrimSpace(s.Auth.Token) == "" {
		return fmt.Errorf("bearer auth requires a token")
	}
	if s.Auth.Mode == AuthHeader && (strings.TrimSpace(s.Auth.Header) == "" || strings.TrimSpace(s.Auth.Token) == "") {
		return fmt.Errorf("header auth requires both header and token")
	}
	return nil
}

// Fetch 按 Spec 取一次数。
func Fetch(ctx context.Context, channel *channelpkg.Channel, spec Spec) (*Result, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	target, err := resolveURL(channel, spec.URL)
	if err != nil {
		return nil, err
	}

	var rows []Row
	switch spec.Strategy {
	case StrategyAPI:
		rows, err = fetchAPI(ctx, channel, target, spec)
	case StrategyScrape:
		rows, err = fetchScrape(ctx, channel, target, spec)
	case StrategyBrowser:
		rows, err = fetchBrowser(ctx, channel, target, spec)
	}
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("extracted no rows")
	}
	if err := validateRows(rows, spec); err != nil {
		return nil, err
	}
	return &Result{Strategy: spec.Strategy, Rows: rows}, nil
}

// resolveURL 允许写绝对地址，也允许写相对渠道 base 的路径。
func resolveURL(channel *channelpkg.Channel, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw, nil
	}
	if channel == nil {
		return "", fmt.Errorf("relative url needs a channel")
	}
	base := strings.TrimRight(channel.GetBaseURL(), "/")
	if base == "" {
		return "", fmt.Errorf("channel has no base url")
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return base + raw, nil
}

// validateRows 校验必需字段与数值边界。任一不满足即整次失败。
func validateRows(rows []Row, spec Spec) error {
	numeric := make(map[string]struct{}, len(spec.Numeric))
	for _, name := range spec.Numeric {
		numeric[name] = struct{}{}
	}
	for _, row := range rows {
		if strings.TrimSpace(row.Key) == "" {
			return fmt.Errorf("row with empty key")
		}
		for _, name := range spec.Numeric {
			raw, ok := row.Values[name]
			if !ok || strings.TrimSpace(raw) == "" {
				return fmt.Errorf("row %q missing numeric field %q", row.Key, name)
			}
			value, err := parseNumber(raw)
			if err != nil {
				return fmt.Errorf("row %q field %q is not numeric: %q", row.Key, name, raw)
			}
			if spec.Min != nil && value < *spec.Min {
				return fmt.Errorf("row %q field %q=%v below minimum %v", row.Key, name, value, *spec.Min)
			}
			if spec.Max != nil && value > *spec.Max {
				return fmt.Errorf("row %q field %q=%v above maximum %v", row.Key, name, value, *spec.Max)
			}
		}
	}
	return nil
}

// parseNumber 宽松地接受 "12.5"、"$12.50"、"1,234" 这类写法。
func parseNumber(raw string) (float64, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "$")
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.TrimSuffix(cleaned, "%")
	return strconv.ParseFloat(strings.TrimSpace(cleaned), 64)
}
