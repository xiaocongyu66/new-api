# 上游覆盖情况

对应生产环境 24 个渠道、8 个上游域名（2026-11-23 实测）。

## 价格（`/api/pricing` 或 `/api/v1/model-plaza`）

| 上游 | 渠道数 | 走法 | 实测 |
|---|---|---|---|
| www.apiloop.top | 7 | `api` `/api/pricing` | 131 行 |
| aiwble.com | 3 | `api` `/api/pricing` | 29 行 |
| tokunexai.com | 1 | `api` `/api/pricing` | 121 行 |
| xcmapi.org | 8 | `api` `/api/v1/model-plaza` | 289 行 / 31 组 / 20 档倍率 |
| yyt.im | 1 | `api` `/api/v1/model-plaza` | 1386 行 / 24 组 / 11 档倍率 |
| ddl.ddpapi.top | 1 | `api` `/api/v1/model-plaza` | 578 行 / 43 组 / 27 档倍率 |
| pub.els.edu.rs | 1 | **需登录态** | 浏览器可达但 `/pricing` 重定向到登录页；需一次性 `storage_state` |
| beiluoxi.top | 2 | **需代理 + 登录** | 境内 IP 被 Cloudflare 拦（`Sorry, you have been blocked`）；加 `UPSTREAM_PROXY` 换出口后可登录 |

## 可用率与延迟

| 上游 | 渠道数 | 走法 | 实测 |
|---|---|---|---|
| www.apiloop.top | 7 | `newapi` `/api/perf-metrics/*` | 432 点 / 23 小时 |
| aiwble.com | 3 | `newapi` 同上 | 582 点 / 24 小时 |
| tokunexai.com | 1 | `newapi` 同上 | 1155 点 / 24 小时 |
| xcmapi.org | 8 | `jk` `/jk/status.json` | 27 点（按渠道模型过滤后） |
| yyt.im | 1 | **待补** | 无 `/jk`，`/api/v1/usage` 需 token |
| ddl.ddpapi.top | 1 | **待补** | 无 `/jk`，同上 |
| pub.els.edu.rs | 1 | **需登录态** | 同上，登录后可读监控页 |
| beiluoxi.top | 2 | **需代理** | 同价格：加 `UPSTREAM_PROXY` 后 `/api/v1/usage` 可用（401 表示要 token） |

## 三处「待补」怎么补

### pub.els.edu.rs（浏览器实测：登录墙）
它是 new-api，`/api/perf-metrics` 与 `/api/pricing` 都要求「模型广场」模块的
导航鉴权，中转密钥进不去。浏览器能打开域名，但 `/pricing` 直接重定向到
`/login`（页面标题「炉边酒馆」）。可选：
1. 用 `browser` 档 + 一次性人工登录的 `storage_state`（见 `references/browser-fallback.md` 结局 B）；
2. 找上游要一个能读这两个模块的账号；
3. 放弃这一家，靠本地真实请求。

### beiluoxi.top（需换出口 IP）
用完整 Chromium 打开首页，返回 Cloudflare 的 `Sorry, you have been blocked`——
这是**按出口 IP 的封锁**，换 UA/无头参数都没用，但**换一个非境内出口就能过**。

做法：在 `secrets.env` 加 `UPSTREAM_PROXY=http://127.0.0.1:7890`，
脚本与浏览器都会走这个代理。代理通了之后再看面板类型：
`/v1/models` 返回 `API_KEY_REQUIRED` 说明网关活着，`/api/v1/usage` 401 说明
要用户 token——那就配 `UPSTREAM_TOKEN_beiluoxi_top` 或账号密码。

### yyt.im / ddl.ddpapi.top 的可用率
它们有公开的 `/api/v1/model-plaza`（价格能拿），但没有 `/jk`。
要么配 `UPSTREAM_TOKEN_<渠道id>` 走 `/api/v1/usage`，要么走 `browser` 登录监控页。

## 一个必须知道的口径差异

- **new-api** 的 `/api/pricing` 里，`model_ratio` 是**未乘分组倍率**的基准价；
  真正价格还要乘响应顶层的 `group_ratio[组名]`。当前 spec 抓的是基准价 +
  `enable_groups` 名单，没有折叠倍率。
- **sub2api** 的 `/api/v1/model-plaza` 是**组内已带倍率语境**的价格，
  spec 已经把 `group` 和 `rate_multiplier` 一并抓出来。

所以跨软件比价时，new-api 那几家要额外乘一次组倍率，否则会低估实际成本。
