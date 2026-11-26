# 采集阶梯：接口 → 爬虫 → 浏览器

同一个上游，先试最便宜的取法，取不到才升级。**每一级都要能明确失败**，
失败原因要具体到「哪一层、为什么」，否则排障时只会看到一句"没数据"。

```
① api      公开/半公开 JSON        ← 首选，稳定、可校验、成本最低
② scrape   服务端渲染的 HTML       ← 次选，选择器随改版失效
③ browser  渲染后的 DOM 或截图     ← 最后，能处理 JS 与登录态
```

三档产出同一种行结构 `{key, values{字段→文本}}`，所以上层不用关心数据从哪来。
每一档的结果都必须过校验（必需字段、数值范围），**校验不过就整次失败**。

## 阶梯怎么走

### 第一级：探接口

不要凭猜测写 spec。先跑一遍探测，把候选路径的真实状态打出来：

```bash
python3 scripts/discover.py https://upstream.example
```

它按顺序试这些路径，只报状态码和返回体形状：

```
/api/pricing                     → new-api 的定价表
/api/v1/model-plaza              → sub2api 的定价表
/api/perf-metrics/summary?hours=24 → new-api 的性能序列
/api/ratio_config                → new-api 的倍率配置
/v1/models                       → 网关的模型清单（含鉴权状态）
/jk/status.json                  → 部分 sub2api 系的公开监控快照
```

判读：

| 结果 | 含义 | 动作 |
|---|---|---|
| `200` + `{` 开头 | JSON 可用 | **落在第一级**，写 api spec |
| `200` + `<` 开头且体积很小（1–10KB） | SPA 的兜底 HTML | 不是数据，进第二级判定 |
| `200` + `<` 开头且体积较大、含数字 | 服务端渲染 | **落在第二级**，写 scrape spec |
| `401/403` + JSON 错误码 | 接口在但要鉴权 | 看是缺 token 还是模块未开放 |
| `403` + Cloudflare 页面 | 网络层或指纹封禁 | 见 `browser-fallback.md` 的结局 C |
| `404` | 该软件没这个路由 | 换候选，或确认它是什么面板 |

**关键判断：`200` 不等于有数据。** SPA 对所有未知路径都返回首页 HTML，
把它当成"接口可用"会写进一堆垃圾。一定要看响应体的第一个字节和大小。

### 第二级：爬 HTML

只有页面 HTML 里**本来就有数字**时才走这级。判断方法：

```bash
curl -s https://upstream.example/pricing | grep -c '0\.0'
```

返回 0 说明数字是 JS 渲染的，直接跳到第三级，别浪费时间写选择器。

选择器只用很小一撮语法（`tag` / `.class` / `#id` / `[attr=value]` / 后代 /
`sel@attr`）。越花越容易静默失配。

### 第三级：浏览器

这一级做的事，按价值从高到低：

1. **发现隐藏接口** —— 页面的数据往往来自某个 XHR。挂 `page.on("response")`
   把它自己调的接口全记下来，经常能直接把第三级降回第一级。
   这一步收益最大，应该第一个做。
2. **取 DOM 文本** —— 接口确实不存在时才用。选择器对应渲染后的结构。
3. **截图** —— 只用于判断「页面有没有数据 / 是否登录墙 / 是否被封」，
   以及作为告警时的现场证据。**不做主数据源**，因为 OCR 数字不可校验。

```python
calls = []
page.on("response", lambda r: calls.append((r.status, r.url)) if "/api/" in r.url else None)
page.goto(url, wait_until="networkidle", timeout=60000)
page.wait_for_timeout(4000)     # 等 XHR，不是等 load
print(calls[:20])               # ← 先看这个
print(page.inner_text("body")[:2000])
page.screenshot(path="shot.png")
```

登录态、封禁、表单陷阱见 `browser-fallback.md`。

## 一次完整的实跑记录（脱敏）

目标：拿到某面板的模型清单 + 价格 + 成功率/延迟。

| 步骤 | 动作 | 结果 | 结论 |
|---|---|---|---|
| 1 | `discover.py <base>` | `/api/pricing` 401、`/v1/models` 401、`/pricing` 200 HTML | 接口要鉴权，HTML 是 SPA |
| 2 | `curl /pricing \| grep -c '0\.0'` | 0 | 数字是 JS 渲染的，scrape 无用 |
| 3 | 浏览器打开 `/pricing`，挂 response 监听 | 页面渲染正常，但数据来自 `/api/perf-metrics/summary` 与 `/api/pricing` | **降回第一级**，改用接口 |
| 4 | 用登录态重试那两个接口 | `200` + JSON | 落在第一级，写 api spec |
| 5 | 点开模型卡片看详情 | 侧滑面板，URL 不变，含「按分组定价」 | 这部分没有接口，**只有这里需要第三级** |

两个可直接复用的结论：

- **先监听网络再写选择器。** 大多数"只能靠浏览器"的页面，其实背后有可用接口。
- **详情面板这类「URL 不变、内容靠交互产生」的部分**，是浏览器的真正用武之地；
  列表页几乎总能找到接口。

## 阶梯必须显式降级，不能隐式兜底

每一级的失败都要记原因，并写进报告：

```
channel 6   api=404 (upstream has no such endpoint: 404)
            scrape=no digits in html
            browser=rendered ok, 88 rows
```

如果只写"降级到 browser"，运维就看不出是接口没开、页面改版、还是被封了，
下次换个人还得重走一遍排查。