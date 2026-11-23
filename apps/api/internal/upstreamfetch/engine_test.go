package upstreamfetch

// 回归：三档取数各自的契约，以及「宁可失败也不写错数字」的校验。
//
// 真实上游的两种 JSON 形状都在这里钉住：
//   - new-api /api/pricing      → {"data":[{model_name, model_ratio, ...}]}
//   - sub2api /api/v1/model-plaza → {"data":{"groups":[{"models":[...]}]}}

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	channelpkg "github.com/QuantumNous/new-api/internal/catalog"
	"github.com/QuantumNous/new-api/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const newAPIPricingFixture = `{
  "success": true,
  "data": [
    {"model_name":"gpt-5.5","model_ratio":37.5,"completion_ratio":6,"model_price":0},
    {"model_name":"gemini-3.8-flash","model_ratio":0,"completion_ratio":0,"model_price":0.038}
  ]
}`

const sub2APIPlazaFixture = `{
  "code": "OK",
  "data": {
    "groups": [
      {"id":2,"name":"福利组","rate_multiplier":0.035,"models":[
        {"name":"gpt-5.6-sol","pricing":{"billing_mode":"token","input_price":2e-7,"output_price":1.2e-6}},
        {"name":"gpt-6-sol","pricing":{"billing_mode":"token","input_price":3e-7,"output_price":1.8e-6}}
      ]},
      {"id":6,"name":"Pro组","rate_multiplier":0.1,"models":[
        {"name":"gpt-5.6-sol","pricing":{"billing_mode":"token","input_price":2.5e-7,"output_price":1.5e-6}}
      ]}
    ]
  }
}`

func TestEvalPathSupportsWildcards(t *testing.T) {
	root, err := unmarshalNumbers([]byte(sub2APIPlazaFixture))
	require.NoError(t, err)

	groups, err := evalPath(root, "data.groups")
	require.NoError(t, err)
	require.Len(t, groups, 2)

	models, err := evalPath(root, "data.groups[].models")
	require.NoError(t, err)
	assert.Len(t, models, 3, "wildcard must flatten both groups' model arrays")

	first, err := evalPath(root, "data.groups[1].name")
	require.NoError(t, err)
	require.Len(t, first, 1)
	assert.Equal(t, "Pro组", scalarText(first[0]))

	missing, err := evalPath(root, "data.nope")
	require.NoError(t, err)
	assert.Empty(t, missing)
}

func TestEvalPathRejectsMalformed(t *testing.T) {
	_, err := evalPath(map[string]any{}, "data[oops]")
	require.Error(t, err)
	_, err = evalPath(map[string]any{}, "data[0")
	require.Error(t, err)
}

// TestAPIStrategyNewAPIPricing：new-api 那种扁平数组 + 字段直取。
func TestAPIStrategyNewAPIPricing(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(newAPIPricingFixture))
	}))
	defer srv.Close()

	spec := Spec{
		Strategy: StrategyAPI,
		URL:      srv.URL + "/api/pricing",
		Auth:     Auth{Mode: AuthNone},
		Rows:     "data",
		Key:      "model_name",
		Values: map[string]string{
			"model_ratio":      "model_ratio",
			"completion_ratio": "completion_ratio",
		},
		Numeric: []string{"model_ratio", "completion_ratio"},
		Min:     new(float64(0)),
	}
	result, err := Fetch(t.Context(), nil, spec)
	require.NoError(t, err)
	require.Len(t, result.Rows, 2)
	assert.Equal(t, "gpt-5.5", result.Rows[0].Key)
	assert.Equal(t, "37.5", result.Rows[0].Values["model_ratio"])
	assert.Equal(t, "6", result.Rows[0].Values["completion_ratio"])
}

// TestAPIStrategySub2APIPlaza：嵌套 groups[].models[]，一行一个模型。
// 组信息与模型信息不在同一层，所以值里用 @ 之外的方式取不到——这里用
// 行内字段，组维度由调用方按 Spec 拆成多次请求分别取。
func TestAPIStrategySub2APIPlaza(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sub2APIPlazaFixture))
	}))
	defer srv.Close()

	spec := Spec{
		Strategy: StrategyAPI,
		URL:      srv.URL + "/api/v1/model-plaza",
		Rows:     "data.groups[0].models",
		Key:      "name",
		Values: map[string]string{
			"input_price":  "pricing.input_price",
			"output_price": "pricing.output_price",
		},
		Numeric: []string{"input_price", "output_price"},
	}
	result, err := Fetch(t.Context(), nil, spec)
	require.NoError(t, err)
	require.Len(t, result.Rows, 2)
	assert.Equal(t, "gpt-5.6-sol", result.Rows[0].Key)
	assert.Equal(t, "0.0000002", result.Rows[0].Values["input_price"])
	assert.Equal(t, "0.0000012", result.Rows[0].Values["output_price"])
}

// TestAPIStrategyValidatesNumbers：页面改版抓到一句提示词时必须整次失败，
// 不能把 "Not Found" 当成价格写进去。
func TestAPIStrategyValidatesNumbers(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"model_name":"m","price":"Not Found"}]}`))
	}))
	defer srv.Close()

	spec := Spec{
		Strategy: StrategyAPI,
		URL:      srv.URL + "/api/pricing",
		Rows:     "data",
		Key:      "model_name",
		Values:   map[string]string{"price": "price"},
		Numeric:  []string{"price"},
	}
	_, err := Fetch(t.Context(), nil, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not numeric")
}

// TestAPIStrategyRejectsOutOfRange：价格上限用来拦住「抓到别的数字」这类事故。
func TestAPIStrategyRejectsOutOfRange(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"model_name":"m","price":"999999"}]}`))
	}))
	defer srv.Close()

	spec := Spec{
		Strategy: StrategyAPI,
		URL:      srv.URL + "/api/pricing",
		Rows:     "data",
		Key:      "model_name",
		Values:   map[string]string{"price": "price"},
		Numeric:  []string{"price"},
		Max:      new(float64(1000)),
	}
	_, err := Fetch(t.Context(), nil, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "above maximum")
}

// TestScrapeStrategy：服务端渲染的表格，用窄选择器取值。
func TestScrapeStrategy(t *testing.T) {
	allowLoopback(t)
	const page = `<html><body>
	  <table id="rates"><tbody>
	    <tr><td class="model">gpt-5.6-sol</td><td class="in" data-unit="usd">0.0000002</td><td class="out">0.0000012</td></tr>
	    <tr><td class="model">gpt-6-sol</td><td class="in" data-unit="usd">0.0000003</td><td class="out">0.0000018</td></tr>
	  </tbody></table>
	</body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()

	spec := Spec{
		Strategy: StrategyScrape,
		URL:      srv.URL + "/pricing",
		Rows:     "table#rates tbody tr",
		Key:      "td.model",
		Values: map[string]string{
			"input":  "td.in",
			"output": "td.out",
			"unit":   "td.in@data-unit",
		},
		Numeric: []string{"input", "output"},
	}
	result, err := Fetch(t.Context(), nil, spec)
	require.NoError(t, err)
	require.Len(t, result.Rows, 2)
	assert.Equal(t, "gpt-5.6-sol", result.Rows[0].Key)
	assert.Equal(t, "0.0000002", result.Rows[0].Values["input"])
	assert.Equal(t, "usd", result.Rows[0].Values["unit"])
}

// TestScrapeStrategyFailsLoudly：选择器失配必须报错，不能返回空结果让上层
// 以为「上游没有数据」。
func TestScrapeStrategyFailsLoudly(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><div>redesigned</div></body></html>`))
	}))
	defer srv.Close()

	spec := Spec{Strategy: StrategyScrape, URL: srv.URL, Rows: "table tr", Key: "td", Values: map[string]string{"v": "td"}}
	_, err := Fetch(t.Context(), nil, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "matched nothing")
}

// TestBrowserStrategyUsesAdapter：browser 档只负责把任务交给外部程序并校验
// 回来的结果，所以可以用一个假适配器完整覆盖。
func TestBrowserStrategyUsesAdapter(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "adapter.sh")
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
cat >/dev/null
echo '{"rows":[{"key":"gpt-5.6-sol","values":{"price":"0.038","latency":"3332"}}]}'
`), 0o755))

	spec := Spec{
		Strategy:   StrategyBrowser,
		URL:        "https://example.invalid/monitor",
		Rows:       ".card",
		Key:        ".model-name",
		Values:     map[string]string{"price": ".price", "latency": ".lat"},
		Numeric:    []string{"price", "latency"},
		AdapterCmd: script,
		TimeoutSec: 10,
	}
	result, err := Fetch(t.Context(), nil, spec)
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	assert.Equal(t, "gpt-5.6-sol", result.Rows[0].Key)
	assert.Equal(t, "0.038", result.Rows[0].Values["price"])
}

// TestBrowserStrategyFailsWhenAdapterDies：适配器崩溃要冒泡，不能静默。
func TestBrowserStrategyFailsWhenAdapterDies(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "adapter.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho 'selector not found' >&2\nexit 3\n"), 0o755))

	spec := Spec{
		Strategy:   StrategyBrowser,
		URL:        "https://example.invalid/monitor",
		Rows:       ".card",
		Key:        ".model-name",
		Values:     map[string]string{"price": ".price"},
		AdapterCmd: script,
		TimeoutSec: 5,
	}
	_, err := Fetch(t.Context(), nil, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "selector not found")
}

func TestSpecValidation(t *testing.T) {
	base := Spec{Strategy: StrategyAPI, URL: "/x", Rows: "data", Key: "name", Values: map[string]string{"v": "v"}}
	require.NoError(t, base.Validate())

	cases := map[string]Spec{
		"unknown strategy": {Strategy: "carrier-pigeon", URL: "/x", Rows: "d", Key: "k", Values: map[string]string{"v": "v"}},
		"no url":           {Strategy: StrategyAPI, Rows: "d", Key: "k", Values: map[string]string{"v": "v"}},
		"no rows":          {Strategy: StrategyAPI, URL: "/x", Key: "k", Values: map[string]string{"v": "v"}},
		"no values":        {Strategy: StrategyAPI, URL: "/x", Rows: "d", Key: "k"},
		"browser no cmd":   {Strategy: StrategyBrowser, URL: "/x", Rows: "d", Key: "k", Values: map[string]string{"v": "v"}},
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			require.Error(t, spec.Validate())
		})
	}
}

// TestRelativeURLUsesChannelBase：Spec 允许写相对路径，方便同一份模板套到
// 多个渠道上。
func TestRelativeURLUsesChannelBase(t *testing.T) {
	allowLoopback(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/pricing" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"model_name":"m","price":"1"}]}`))
	}))
	defer srv.Close()

	channel := &channelpkg.Channel{
		Id: 1, Type: 1, Key: "sk-test", Name: "x",
		Models: "m", Status: common.ChannelStatusEnabled,
		BaseURL: &[]string{srv.URL}[0],
	}
	result, err := Fetch(t.Context(), channel, Spec{
		Strategy: StrategyAPI,
		URL:      "/api/pricing",
		Auth:     Auth{Mode: AuthChannelKey},
		Rows:     "data",
		Key:      "model_name",
		Values:   map[string]string{"price": "price"},
		Numeric:  []string{"price"},
	})
	require.NoError(t, err)
	require.Len(t, result.Rows, 1)
	assert.Equal(t, "m", result.Rows[0].Key)
}
