# 上游价格对比（渠道）问题汇总 — 待确认

分支：`docs/price-comparison-audit`，基线 `2f2ff22bb`（含 `511928059 feat(pricing): upstream price comparison and profit analysis`）。
**本文只是问题清单，未改任何代码。**

涉及代码：

| 位置 | 职责 |
|---|---|
| `apps/api/internal/ops/manage_model_metadata_sync.go` | `/api/ratio_sync/fetch`：拉上游 `/api/pricing`、归一化、存快照、出差异 |
| `apps/api/internal/ops/upstream_price_snapshot.go` | 快照落库 / 裁剪 |
| `apps/api/internal/ops/model_pricing_analysis.go` | `/api/ratio_sync/analysis`：本地价 vs 上游价、成本/利润/利润率 |
| `apps/web/src/features/system-settings/models/upstream-ratio-sync.tsx` | 页面装配、窗口参数、缓存 |
| `apps/web/src/features/system-settings/models/pricing-analysis-table.tsx` / `-dialog.tsx` | 展示 |

数据来源（服务器 `paid-newapi-1`，已核对）：`upstream_price_snapshots`、`channels`、`abilities`、`options`、`logs`、`quota_data`。

---

## P0-1 按次（固定价）模型被当成按量：成本按 token 线性放大

`model_pricing_analysis.go:206-223`：

```go
if cwUsd := evalUsd(upstreamExpr, canonicalWindow); cwUsd != nil && canonicalWindow > 0 {
    if strings.Contains(upstreamExpr, "fixed(") {
        chCost = *cwUsd * float64(ug.Requests)
    } else {
        unit := *cwUsd / float64(canonicalWindow+canonicalWindow/4)
        chCost = unit * float64(ug.Tokens)
    }
```

`evalUsd` 在 `canonical_window=131072` 求值，`fixed()` 的返回值与 token 数无关，所以按次分支本身是对的。
问题在**上游快照里根本没有 `fixed(` 表达式**（`upstream_price_snapshots` 中 `field in ('model_price','billing_mode','billing_expr','billing_context_tier','billing_context_tier_per_call')` 计数为 **0**；只有 `model_ratio` / `completion_ratio` / `cache_ratio`）。

根因（已由代码 + 服务器数据双向确认）：

- `pricingSyncFields`（`manage_model_metadata_sync.go:79-90`）里**没有** `quota_type`；解析上游 `/api/pricing` 时 `item.QuotaType` 只用来决定「填 `modelPriceMap` 还是 `modelRatioMap`」（:877-882），之后整个字段被丢弃。
- 快照表里 `field in ('model_price','billing_mode','billing_expr','billing_context_tier','billing_context_tier_per_call')` 计数 = **0**，只有 `model_ratio` / `completion_ratio` / `cache_ratio`。上游是 new-api 时它确实按 `quota_type=1` 返回了 `model_price`，但本站快照没存下来。
- 于是 analysis 侧只能靠 `strings.Contains(upstreamExpr, "fixed(")` 猜「是不是按次」，而 `modelExpr` 对「只有 `model_ratio` 没有 `model_price`」的字段集永远生成 `p * X + c * Y`，永远不含 `fixed(`。
- 更上游的一层：**本地**把「按次」和「按量」编码在 `billing_mode` 里的方式不一致——`options.billing_setting.billing_mode` 里 `claude-fable-5[次]=per-request`、`claude-opus-4.6[次]=per-request`，但 `claude-opus-5-5[次]` / `claude-opus-5[次]` / `claude-sonnet-5[次]` / `claude-opus-4-8[次]` 都是 `per-token`，尽管它们同样有 `ModelPrice=0.15` 且 logs 里 quota 恒定 75000。上游取的是本地这套 mode 的镜像，所以连「哪个是按次」这个事实在快照里都不存在。

服务器实证（`claude-opus-5-5[次]`，`ModelPrice=0.15`，logs 里 `model_price=0.15` 恒定）：

```
quota_data: 27|claude-opus-5-5[次]|token_used=285455|count=6|quota=398250
           27|claude-opus-5-5[次]|token_used=69216 |count=3|quota=225000
logs:       prompt_tokens 3104~24787, completion 1~8904, quota 恒为 75000
            75000 / 500000 = $0.15 —— 与 token 数无关
```

按现在的公式（无 `fixed(` → 走 per-token 分支）会算成：

```
upstream model_ratio 0.44995 → inPrice=0.44995*500=224.975 $/1M
unit = (224.975*131072 + 224.975*5*32768) / (131072+32768) / 1e6 ... ≈ 逐 token
```

即：**$0.15/次 的真实成本被算成随 token 增长的金额**。`claude-opus-5-5[次]` 这类 `[次]` 别名在快照里**一条都没有**（`model_name like '%[次]%'` 计数 0），实际全部落到 reference 兜底，价格来源本身就是错的。

同理 `gemini-4-argon`（`quota_type=1`、`ModelPrice=0.16`、logs 恒定 `quota=112500`）在快照里只有 `model_ratio=0.3`。

**纠正方向**：fetch 阶段要把「上游按次」这一事实落进快照。`/api/pricing` 的 `quota_type` 必须存（现在整个字段被丢弃），analysis 里按 `quota_type==1` 走 `fixed(model_price)`，而不是靠 `strings.Contains(expr,"fixed(")` 猜。`[次]`/`[套餐]` 这类纯本地别名要么在 fetch 时按 mapping 归一到上游真名，要么显式标成「无上游报价」。

---

## P0-2 分组倍率（GroupRatio）完全没参与，成本与利润全错

计费链路（`internal/relay/helper/compute_price.go`）：所有模式最后都乘 `groupRatioInfo.GroupRatio`。

- 按量：`ratio := modelRatio * groupRatioInfo.GroupRatio`（:121）
- 按次：`modelPrice * common.QuotaPerUnit * groupRatioInfo.GroupRatio`（:172、:219）
- 阶梯：`quotaBeforeGroup * groupRatioInfo.GroupRatio`（`settle_tiered.go:28`、`compute_price.go` modelPriceHelperTiered）

对比链路（`model_pricing_analysis.go`）：`revenue` 来自 `quota_data.quota`（**已含分组倍率**），`cost` 来自上游表达式（**不含任何分组倍率**）。两者不同量纲。

服务器实证（`options.GroupRatio`）：

```
{"vip":2,"福利":0,"default":1,"清水rp":0.8,"Gemini套餐":1,"Gemini周卡":0.9,"Gemini月卡":0.75}
GroupGroupRatio: {"vip":{"default":0.9}}   ← 用户组 vip + 渠道组 default → 0.9
```

`logs` 里实际出现的 `group_ratio`：`1 / 0.45 / 0.9 / 2 / 0.75 / 0.5 / 0.1 / 0 / 0.4 / 0.8 / 0.869565`。

具体错例（`gemini-3.8-flash`，ModelPrice=0.023，default 组）：

```
logs: model_price=0.023, group_ratio=0.9, count=71, quota 1035~10350
      0.023 * 0.9 * 500000 = 10350   ← 0.9 是「vip 用户走 default 组」的 group_group_ratio
analysis: cost 用 model_ratio=0.246251 直接算，不乘 0.9
```

结果：**分组倍率把实收整体缩放，analysis 完全看不见**。`vip` 组一个请求实收是 default 的两倍（`GroupRatio.vip=2`）；`group_ratio=0` 的「福利」组实收为 0（成本照算）；`0.45` / `0.1` / `0.4` 这类折扣或活动组同样被当成 default。`logs` 里实测 `group_ratio` 有 11 种取值，成本却只用一种口径。

> **修正（实施时核实）**：成本是上游对我们的收费，本身**不应**乘本站分组倍率；收入 `quota` 已经含倍率，所以 `利润 = 收入 − 成本` 的骨架是对的。真正的缺口是**可见性**：表格与对话框无法体现「这个渠道实际按什么倍率在卖」，折扣组被当成原价、加价组看不出溢价。实施改为在窗口与渠道行上补 token 加权的 `group_ratio` 列，并在窗口有日志时用**真实收入**（quota）而不是模拟单价算毛利率。

同一模型在不同渠道/分组下有效单价完全不同（`quota_data` 实测）：

```
gemini-3.8-flash  default|225 行 | quota 9567357 | tokens 80911256
gemini-3.8-flash  vip    | 17 行 | quota  706600 | tokens  3900202   ← 单 token 收入是 default 的 ~2.3 倍
gemini-3.8-flash  清水rp | ... group_ratio 0.8
gemini-4-argon    default vs vip 实收差 2 倍
```

**纠正方向**：成本要按「渠道 × 分组」拆开，或至少按该渠道在窗口内的真实分组分布加权。`quota_data` 有 `use_group` 列，`logs.other` 有 `group_ratio` / `user_group_ratio`——analysis 目前只按 `(channel_id, model_name)` 聚合（`usageAggregates`），把分组信息丢掉了。最小改动是聚合键加上分组、成本乘对应 `group_ratio`；更彻底的是把「渠道可服务分组」与「用户实际所在分组」都算进去。

---

## P1-1 上下文分档（context_tier）本地有、上游无，对比无意义

本地 `billing_setting.billing_mode` 里有 59 个模型是 `tiered_expr`，`billing_setting.billing_expr` 有 47 条，其中大量是上下文分档（`len <= 10000 ? tier("t1", fixed(...)) : ...`）。但快照里：

- `billing_mode` / `billing_expr` / `billing_context_tier` / `billing_context_tier_per_call` 行数 = **0**
- `billing_context_tier` / `billing_context_tier_per_call` 两个 option 本身为**空**（服务器上没启用这两种模式）

所以 analysis 对分档模型只能拿 `model_ratio` 单值算「上游价」，与本地分档价对比 → `diff_pct` / `margin_pct` 是「单档 vs 分档」的伪差异。

窗口（`windows: [8192, 32768, 131072, 200000, 1000000]`）与本地档位边界（10000/100000/400000）也不对齐，`channelWindows` 逐窗口取 `evalUsd` 只是「在该上下文长度上单点求值」，对 `fixed()` 阶梯是正确的，但对「本地分档、上游单值」的组合仍然得出无意义百分比。

**纠正方向**：上游没有分档数据时，`diff_pct` 应该标「不可比」而不是给数字；或者 fetch 阶段支持从上游渠道的价格页/订阅页抓分档（需要确认上游是否提供）。至少要给 `AnalysisChannelWindow` 加一个 `comparable: false`。

---

## P1-2 阶梯成本用「canonical window」单点近似，不反映真实档位分布

`model_pricing_analysis.go:206-213`：

```go
unit := *cwUsd / float64(canonicalWindow+canonicalWindow/4)
chCost = unit * float64(ug.Tokens)
```

`canonicalWindow` 是 `closestWindow(windows, 131072)`，永远 = 131072（窗口列表里就有）。于是无论真实请求落在哪个档，成本都按「131072 上下文的单价」× 总 token 数。

服务器实证：`claude-opus-5-5` 的**本地** `billing_setting.billing_expr` 是五档 `fixed()`（`len<=100000 → 0.084`、`<=256000 → 0.215`、`<=500000 → 0.42`、`<=700000 → 0.588`、else `0.84`），logs 里同一模型同时出现 `matched_tier=100K/256K/500K/700K`，quota 分别 42000 / 96000 / 43200…，档位分布很散。

但**上游快照里这个模型只有 `model_ratio=0.44995 / completion_ratio=5.000111 / cache_ratio=0.050006`**（`fixed(` 表达式一条都没有）。于是 analysis 拿单值 ratio 在 131072 处求值 × 总 token 数当成本，与「本地五档固定价、实际落在 100K/256K/500K/700K」完全不是一回事。

注意 `channelWindows` 里其实已经有一套**精确**算法（`windowCostUSD` 逐 log 用真实 prompt/completion 求值），但只用于窗口明细；行级/渠道级汇总走的却是单点近似。两套口径并存。

**纠正方向**：行级成本直接复用 `windowCostUSD` 的逐 log 求和（数据已在 `tierUsageMap` 里），删掉 canonical-window 单点路径。

---

## P1-3 收入与成本口径不一致：收入含订阅抵扣，成本是现金

`logs.other` 里大量字段：`billing_source`(wallet/subscription)、`billing_preference`(subscription_first/wallet_only/...)、`subscription_consumed`、`wallet_quota_deducted`、`subscription_*_display`。

`revenue` 直接取 `sum(quota)`，把「订阅抵扣的那部分」也算成现金收入。`cost` 是真实美元支出。于是有订阅用户时利润率虚高。

服务器实证：

```
billing_source | billing_preference   | count
wallet         | (空)                 | 2927
subscription   | (空)                 |  958
subscription   | subscription_first   |  682
wallet         | subscription_first   |   71
...
```

**纠正方向**：明确「收入」定义（现金 / 现金+订阅抵扣 / 仅账单口径），analysis 里分开显示；或按 `billing_source` 拆分 revenue。

---

## P2-1 上游价来源单一：`models.dev` 最低价当「上游价」

`convertModelsDevToRatioData`（`manage_model_metadata_sync.go:330-411`）+ `shouldReplaceModelsDevCandidate`：

```go
if nextNonZero && !nearlyEqual(next.Input, current.Input) {
    return next.Input < current.Input   // 取最低价
}
```

`models.dev/api.json` 一个模型名会对应多个 provider。`convertModelsDevToRatioData` 用 `provider/model` 复合键把每个 provider 的报价都写进快照（实测 10 条 `gemini-4-argon` 记录），但**不带 provider 维度**；`analysis` 查找时只用裸模型名（`gemini-4-argon`），正好命中那条最低的 0.3，且无法区分它来自哪个 provider。

也就是说：跨 provider 取最低价的语义是**查找层的副作用**（`shouldReplaceModelsDevCandidate` 的 `next.Input < current.Input` 决定了裸键上留下谁），而不是显式声明的规则。

服务器实证（`gemini-4-argon` 的 model_ratio）：

```
gemini-4-argon                | 0.3
gemini/gemini-4-argon         | 0.8
google/gemini-4-argon         | 1
google-ai-studio/...          | 1
google-vertex/...             | 1
gemini-4-argon@eu             | 1.1
gemini-4-argon-contributor    | 2
vertex/gemini-4-argon         | 2
```

取 0.3 作为「上游价」，对 `https://www.apiloop.top`（渠道 20/34/36）等实际按 0.16/次 或 0.125/次 结算的渠道毫无意义。

另外 `cacheRatio` 用 `cacheRead/input` 比值、`model_ratio` 用 `input*USD/1000`（`USD=500`），这套换算与本地 `ratio_setting` 的 `USD=500`、`USD2RMB=7.3` 混用，口径需要一次对齐审计（本次未逐条验证）。

**纠正方向**：只有关联到具体渠道的上游才算「该渠道的上游价」；`models.dev` 应作为「参考价」单独一列，不混入 cost/profit。

---

## P2-2 本地价 vs 上游价 名称解析靠前缀剥离，别名一多就错配

`upstreamAliasCandidates`（:331-343）只处理三种情况：原名、`[套餐]` 前缀、`a/` 前缀、以及 `channel.model_mapping` 精确命中。

服务器实证：`models` 表里 `[套餐]gemini-4-argon` 存在、`[无甲]-deepseek-v4.1-flash-1017` 存在、`[次]` 后缀系列存在；`model_mapping` 里映射的是 `哈基米3.1f` 这类**渠道内部别名**，不是 `models.dev` 的 provider 名。

`resolveUpstreamFields` 找不到就回落到 `referenceFields`（models.dev 最低价），于是「找不到」被静默当成「有报价」。

**纠正方向**：解析失败必须显式标 `upstream_missing`，让 UI 显示「无上游报价」而不是套用参考价。

---

## P2-3 渠道级差异（`differences`）把本地价与上游价逐字段对比，但两边语义不同

`buildDifferences`（:413-565）把 `model_ratio` / `completion_ratio` / `cache_ratio` / `model_price` / `billing_mode` / `billing_expr` 逐字段比。上游 `/api/pricing` 给的是**那个上游自己的定价**，本地给的是**本站定价**，两者本就不该相等；页面却把它们当「差异」列出来。

且 `confidenceMap` 用「`model_ratio==37.5 && completion_ratio==1.0` 视为不可信」这种启发式，与 P0-1 的 `quota_type` 缺失是同一类问题——都是靠猜。

**纠正方向**：明确这张表的用途（是「对本对账」还是「发现可套利价差」）。若是后者，应只展示「本地成本 > 上游报价」的项，并带上分组倍率换算。

---

## P2-4 快照保留 12 个批次，fluctuation 只比最近两批

`upstream_price_snapshot.go:73`：`snapshotRetentionBatches = 12`，`computeFluctuation` 只取 `DISTINCT fetched_at DESC LIMIT 2`。

页面每 5 分钟自动刷新（`ANALYSIS_REFRESH_MS`），一次手动 fetch 就是一批。12 批 ≈ 1 小时窗口，超出就看不到波动。且 `fetched_at` 是秒级，同秒两次 fetch 会**替换**（`persistUpstreamPriceSnapshots` 里先 `Delete where fetched_at=?`）——并发或双击时批次会被吞。

**纠正方向**：确认期望的观察窗口；若需要按天看波动，保留策略要按天而不是按批。

---

## P3 其它已确认的小问题

1. `model_pricing_analysis.go:211` 的 `unit` 计算把 `canonicalWindow+canonicalWindow/4` 当分母，这是「prompt+completion」的假设（completion=prompt/4），与 `evalUsd` 内部 `evalUsdWithTokens(expr, ctx, ctx/4)` 一致，但对 `fixed()` 分支无意义（已由 `strings.Contains` 兜住），属脆弱耦合。
2. `usageAggregates` 用 `dbx.DB`（主库），`tierUsageByWindow` 用 `dbx.LogDB`（日志库）。日志库与主库分离部署时（本机就是 `newapi-postgres-prod` 单库，但代码支持分离），行级 `requests/tokens/revenue` 与窗口级 `cost` 会来自不同库，可能不一致。
3. `tierUsageByWindow` 把 `matched_tier` 读进 `pricingSample.Tier` 但**从未使用**（表达式自己重选档位）。要么用它做交叉校验，要么删字段。
4. `PricingAnalysis` 里 `row.PriceModel` 用「token 数最多的 matched model」代表整行价格（:231-234），通配别名（`name_rule != exact`）会把多个模型的价格混在一行。
5. `resolveUpstreamFields` 的 `sourceModels` 为空时（该渠道 fetch 失败）仍会回落到 `referenceFields`，成本照算——**渠道报价拉取失败不会让成本变成 null**，会静默用 models.dev 最低价。
6. `channelSourceKey(name, id)` 拼 `"name(id)"`，而 `GetSyncableChannels` 里预设备用 ID 是 `-100/-101`，真实渠道 id>0；两者混在同一张快照表里，靠 `strings.HasPrefix(source, "models.dev")` 区分，脆弱。

---

## 建议的修复顺序（待确认）

1. **P0-2 分组倍率** — 影响所有数字，改 `usageAggregates` 聚合键 + 成本乘 `group_ratio`。
2. **P0-1 按次 vs 按量** — fetch 存 `quota_type`、analysis 按它选公式；补 `[次]`/`[套餐]` 别名归一。
3. **P1-2 阶梯成本单点近似** — 行级成本改走 `windowCostUSD` 逐 log 求和，删 canonical 路径。
4. **P1-1 上下文分档不可比** — 加 `comparable` 标记，UI 显示「无上游分档数据」。
5. **P1-3 订阅收入口径** — 先定义口径，再拆 revenue。
6. **P2-1 / P2-2 / P2-3** — 上游价来源与命名解析重做（工作量最大，需要先定「什么算某个渠道的上游价」）。
7. **P2-4 / P3** — 保留策略与清理。

---

## 需要你确认的问题

1. **「上游价」的定义**：是「该渠道 `/api/pricing` 报出的价」还是「该渠道背后的真实成本」？现在混用了两者（还有 models.dev 参考价）。这决定 P0-1 / P2-1 怎么改。
2. **分组倍率怎么处理**：成本按「该渠道实际服务分组」分别算，还是按「当前用户在用的分组」算？analysis 是全局视图，没有单一用户上下文。
3. **`[次]` / `[套餐]` 这类本地别名**：要在 fetch 时归一到上游真名，还是在 analysis 里做别名表？前者要改 fetch 协议，后者要维护一张映射。
4. **收入口径**：订阅抵扣的那部分算不算收入？
5. **修复范围**：先只修 P0（数字正确性），还是连 P2（上游价来源/别名解析）一起重做？

---

## 实施结果（分支 `fix/price-comparison-accuracy`）

已实现：

1. **逐条日志精确计价**：渠道成本改为逐条消费日志按其真实 prompt/completion 求值（`fixed()` 按次与按量走同一求值路径），窗口内无日志时才退回代表窗口估算。
2. **移除静默参考价兜底**：渠道没有自身快照时不借用 models.dev 价格，`price_mode` 为空 → UI 显示「无上游报价」，成本为 `—`。
3. **分组倍率可见**：窗口/渠道两级新增 token 加权 `group_ratio`；窗口有日志时毛利率改用真实收入计算，并给出订阅抵扣份额（`subscription_revenue`）。
4. **别名解析**：`[次]` 后缀可剥离；`model_mapping` **优先于**前缀/后缀剥离（管理员显式声明的售卖名压过上游目录通用行）；abilities 独有模型（如 `[次]` 按次别名）单独成行。
5. **快照竞态**：确认选渠道会触发两次 `/fetch`（confirm 一次 + analysis 预取一次），同秒 `fetched_at` 按秒替换会清掉先落库的渠道；加了 5 秒内不重复预取的守卫。
6. **选择状态持久化**：渠道选择原先只存在组件内 state，刷新即回落到 models.dev 预设、成本全变 `—`；改为 sessionStorage 持久化。

未做（需要你定的口径）：

- **P2-1** models.dev 仍然按「跨 provider 取最低价」落库；只作为可选的参考渠道，不再自动兜底。
- **P1-1** 未加 `comparable` 标记：改为真实逐日志经济学后，窗口内没有日志的格子显示 `—`，不再伪造百分比。
- **P2-4** 快照保留仍为 12 个批次。


### 第二轮（分组口径）

用户指出两点，均已核实并修复：

1. **阶梯计费没区分按次/按量** —— 原来一律显示「阶梯表达式」。改为 `Tiered (per request)` / `Tiered (per token)`，依据 `price_mode` 的 `per_call_tiered` / `per_token_tiered`。
2. **分组倍率没有真正参与** —— 参考 `yaojin2meng/ops-collector` 的口径（`USD/1M = 倍率 × 2 × 分组倍率`，按次 `model_price × 分组倍率`），确认上游 `/api/pricing` 同时给出站点级 `group_ratio` 与模型级 `enable_groups`。本站原先只存了模型字段，把这两个丢了。现在：
   - fetch 把 `group_ratio`（存在空模型名下）与 `enable_groups` / 模型级 `group_ratio` 一并入库；
   - 分析时按上游分组倍率放大上游单价：单分组精确；多分组取最高倍率并标记 `upstream_group_ambiguous`（保守估成本）；
   - abilities 的 `group` 折进渠道绑定（同渠道同模型不再重复成多行），响应里返回站内分组列表与 `GroupRatio` 表；
   - 前端在渠道块里给站内分组做 chip（`default ×1.00` / `vip ×2.00` / `清水rp ×0.80`），切换即重算「本站 = 单价 × 倍率」与毛利率；上游侧显示 `Upstream group Gemini 按次 ×1.00`。

顺带修掉两个会污染对比的缺陷：

3. **渠道映射优先级** —— 上游目录里同名模型（如 `gemini-4-hydrogen` 的按量行）会盖住渠道映射指向的按次模型（`哈基米4h`）。改为 `model_mapping` 优先，同名目录行只作无映射时的回退。
4. **本地残留表达式覆盖按次价** —— `billing_mode=per-request` 且 `ModelPrice` 已配置时，残留的旧 `billing_expr` 仍被当作有效价格，导致「本站」显示按量分档价而 relay 实际按次收费。改为只有完全没有 mode 时才回退到表达式。
