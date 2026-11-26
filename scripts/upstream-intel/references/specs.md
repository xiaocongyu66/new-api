# 给一个上游写 spec

一份 spec 就是一个 JSON 文件，文件名是上游域名：`specs/<host>.json`。

## 三档，从便宜到贵

### 1. api — 公开或半公开的 JSON

先看有没有现成接口。这两条能覆盖大部分：

| 软件 | 接口 | rows |
|---|---|---|
| new-api | `/api/pricing` | `data` |
| sub2api | `/api/v1/model-plaza` | `data.groups[].models` |

```json
{
  "strategy": "api",
  "url": "/api/pricing",
  "rows": "data",
  "key": "model_name",
  "values": { "model_ratio": "model_ratio" },
  "numeric": ["model_ratio"],
  "min": 0, "max": 100000
}
```

`rows` 是「行集」的路径，`key` 和 `values` 是**行内**的字段。
支持 `[]` 通配，`data.groups[].models` 会把每个组的每个模型都摊成一行。

`values` 的值也可以是嵌套路径，例如 `pricing.input_price`。

### 2. scrape — 服务端渲染的 HTML

只有当页面 HTML 里**本来就有**数字时才用。判断方法：

```bash
curl -s https://host/pricing | grep -c '0\.0'
```

返回 0 就说明数字是 JS 渲染出来的，scrape 没用，直接跳到 browser。

```json
{
  "strategy": "scrape",
  "url": "/pricing",
  "rows": "table.price-table tbody tr",
  "key": "td.model-name",
  "values": { "input": "td.input-price" },
  "numeric": ["input"]
}
```

选择器语法只有：`tag`、`.class`、`#id`、`[attr=value]`、后代（空格），
以及 `选择器@属性` 取属性值。刻意不支持复杂 CSS——越花越容易静默失配。

### 3. browser — 需要 JS 或登录态

```json
{
  "strategy": "browser",
  "url": "/pricing",
  "rows": ".model-card",
  "key": ".model-name",
  "values": { "price": ".price" },
  "numeric": ["price"],
  "timeout_seconds": 45,
  "storage_state": "~/.config/newapi-upstream-intel/state/<host>.json"
}
```

`storage_state` 是一次人工登录后保存的 cookie 快照：

```bash
python3 - <<'PY'
from playwright.sync_api import sync_playwright
with sync_playwright() as p:
    b = p.chromium.launch(headless=False)
    c = b.new_context()
    pg = c.new_page()
    pg.goto("https://host/login")
    input("登录完成后按回车...")
    c.storage_state(path="/home/you/.config/newapi-upstream-intel/state/host.json")
    b.close()
PY
```

这个文件等价于密码，权限设 600，**不要**放进仓库、不要贴给 agent。

## 写完之后

```bash
python3 scripts/collect_prices.py --channels out/channels.json --only <渠道id>
```

先只跑一个渠道确认能取到行，再全量。

## 校验不要放宽

`numeric` + `min`/`max` 是防「抓到提示语」的唯一关卡。上游改版时
选择器会失配、页面会变成一句 "Not Found"，这时应该修 spec，
而不是把校验去掉让 0 混进来。
