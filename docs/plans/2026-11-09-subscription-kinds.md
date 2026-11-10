# 订阅种类改造（Subscription Kinds）任务说明

> **执行约定：** 本仓库禁止在本机运行 `go build` / `go test` / `bun run build` / `bun install`。
> 所有编译与测试由 PR CI 完成；本地只做静态审阅与 `gofmt -l`。子代理同样遵守。

## 背景

当前订阅只有一种形态：套餐同时携带「额度池 + 单一升级分组 + 使用期」，过期只由
`end_time` 驱动（`ExpireDueSubscriptions`）。额度扣减由 `PreConsumeUserSubscription`
在任意活跃订阅中挑第一个剩余额度足够的订阅完成，不校验请求实际使用的分组。

这带来三个问题：

1. 订阅无法表达「按额度耗尽即过期」「按指定分组模型的请求次数」这两类商品。
2. 订阅额度对所有分组生效，跨分组使用无法阻止。
3. 请求次数型订阅缺少一套可复用的请求结果判定（空回、上游断流、用户断流、正常完成）。

## 目标

1. 给订阅引入种类（kind）：`time`（按使用期过期，现行行为）、`quota`（额度耗尽即过期）、
   `request`（按分组模型的请求次数）。
2. 订阅可设置多个升级分组；订阅额度只允许在升级后的分组内使用。
3. 请求次数型订阅：
   - 创建时按「分组 + 多选模型 + 次数」配置；
   - 请求命中时扣 1 次；
   - 计数判定统一重构为可复用能力：
     - 空回不扣；
     - 上游断流且已交付输出超过 1000（completion tokens）才扣，少于等于 1000 不扣；
     - 用户断流（客户端主动断开）扣；
     - 正常完成扣。
4. 前端：
   - 订阅创建页新增「订阅种类」下拉菜单，按种类动态显示表单；
   - 请求次数型订阅在用户侧以「列表横条」展示：每条两行，一行颜色进度条，一行数值；
   - 升级分组改为多选。

## 现状关键代码

- 套餐与订阅模型：`apps/api/internal/billing/store_subscription.go`
  - `SubscriptionPlan`：`duration_*`、`total_amount`、`quota_reset_*`、`upgrade_group`、`downgrade_group`。
  - `UserSubscription`：`amount_total/amount_used`、`start_time/end_time`、`upgrade_group/prev_user_group/downgrade_group`。
  - `CreateUserSubscriptionFromPlanTx`：购买时快照套餐并升级用户分组。
  - `PreConsumeUserSubscription` / `RefundSubscriptionPreConsume` / `PostConsumeUserSubscriptionDelta`：额度预扣、退款、结算。
  - `ExpireDueSubscriptions` / `ResetDueSubscriptions`：过期与周期重置。
- 计费会话：`apps/api/internal/billing/open_billing_session.go`、`resolve_funding_source.go`。
- 文本计费结果判定：`apps/api/internal/billing/consume_text_quota.go`
  - `isUndeliveredTextResponse`（空回）、`streamCutKind`（断流归因）、
    `truncatedResponseMultiplierForTokens`（上游断流按 token 分档）。
- 流式状态：`apps/api/internal/relay/common/track_stream_status.go`、`build_relay_info.go`
  （`StreamStatus`、`StreamSawTextContent`、`StreamSawFinishReason` 等）。
- 请求分组：`relayInfo.UsingGroup`（`apps/api/internal/relay/common/build_relay_info.go`）。
- 路由：`apps/api/internal/transport/compose/register_api.go`。
- 前端：`apps/web/src/features/subscriptions/**`、`apps/web/src/features/wallet/components/subscription-plans-card.tsx`。

## 数据模型

### SubscriptionPlan 新增字段

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `Kind` | `varchar(16) not null default 'time'` | `time` / `quota` / `request` |
| `UpgradeGroups` | `text` | JSON 数组，升级分组集合；第一项为主升级分组。空数组表示不限制分组（兼容旧数据） |
| `RequestRules` | `text` | JSON 数组，仅 `request` 种类使用 |

`RequestRules` 结构：

```json
[
  { "group": "vip", "models": ["gpt-4o", "claude-3-5-sonnet"], "count": 100 }
]
```

语义：该分组下每个被选中的模型各自拥有 `count` 次请求额度（独立计数，便于前端逐条展示）。

兼容：保留现有 `UpgradeGroup` 列作为「主升级分组」，写入时取 `UpgradeGroups[0]`；
读取旧套餐时若 `UpgradeGroups` 为空则回退为 `[UpgradeGroup]`（空字符串则视为空数组）。

### UserSubscription 新增字段

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `Kind` | `varchar(16) not null default 'time'` | 购买时快照 |
| `UpgradeGroups` | `text` | 购买时快照的 JSON 数组 |

### 新表 SubscriptionRequestCounter

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `Id` | int PK | |
| `UserSubscriptionId` | int，索引 | |
| `GroupName` | varchar(64) | |
| `ModelName` | varchar(128) | |
| `Total` | bigint | 总次数 |
| `Used` | bigint | 已用次数 |
| `CreatedAt/UpdatedAt` | bigint | |

唯一索引：`(user_subscription_id, group_name, model_name)`。
购买 `request` 套餐时按规则展开为逐模型计数行。

## 语义

### time（默认）

保持现行行为：`end_time` 到期；额度可无限（`total_amount = 0`）；按 `quota_reset_*` 周期重置。

### quota

- `total_amount` 必须大于 0。
- `end_time = 0` 表示不设时间上限；额度耗尽（`amount_used >= amount_total`）即过期。
- 强制 `quota_reset_period = never`（否则重置会让「耗尽即过期」永不发生）。
- `ExpireDueSubscriptions` 增加一条扫描：`kind='quota' AND amount_total>0 AND amount_used>=amount_total`。

### request

- `end_time = 0` 表示不设时间上限；也可设置时间上限，取二者先到者。
- 全部计数行都 `used >= total` 时过期。
- 请求命中条件：`using_group` 与 `model_name` 与某计数行匹配且 `used < total`，且订阅 `active`。
- 命中后同一事务内：计数行 `used += 1`（准入与展示账本），并走标准订阅额度预扣；
  实际消耗按当前模型价格 × 分组倍率结算到订阅额度池，改价无需迁移历史数据。
- 空回 / 短断流：标准结算退回额度的同时，`WaiveSubscriptionRequestConsume` 退回计数；
  请求失败退款同样双退。订阅层 `AmountTotal` 可设 0 = 不限（只限次数）。

## 分组限制

`PreConsumeUserSubscription` 增加 `usingGroup` 参数：

- 订阅快照 `UpgradeGroups` 非空时，仅当 `usingGroup` 在集合内才可消费该订阅；
- 为空时维持旧行为（不限制），保证历史订阅不会突然失效；
- 新套餐若配置了升级分组，则强制写入 `UpgradeGroups`，从源头保证「额度只给升级后的分组使用」。

`NewBillingSession` 在选择订阅时也要做同样的可用性判断，避免 `subscription_first`
选中一个不可用订阅后报错而不是回退钱包。为此新增
`HasUsableUserSubscription(userId, usingGroup, modelName)`，同时覆盖 request 命中判断。

## 计费接入

`SubscriptionFunding` 增加 `kind` 与请求计数上下文。预扣路径：

- `time` / `quota`：沿用现有额度预扣（按金额）。
- `request`：调用新的 `PreConsumeSubscriptionRequest`，对 `SubscriptionRequestCounter`
  对应行加锁并 `used += 1`，返回计数行 ID。结算/退款按计数行处理，不再动 `amount_used`。

结算：

- 请求成功且判定「应计数」→ 提交（计数保持）。
- 请求成功但判定「不应计数」→ 退回 1 次。
- 请求失败退款 → 退回 1 次。

## 计数判定（统一重构）

在 billing 包内提取可复用能力（建议新文件 `request_outcome.go`）：

```go
type RequestOutcome string

const (
    RequestOutcomeNormal          RequestOutcome = "normal"
    RequestOutcomeEmpty           RequestOutcome = "empty"
    RequestOutcomeClientAbort     RequestOutcome = "client_abort"
    RequestOutcomeUpstreamCut     RequestOutcome = "upstream_cut"
    RequestOutcomeFailed          RequestOutcome = "failed"
)

const RequestCountMinOutputTokens = 1000

// ClassifyRequestOutcome 复用 consume_text_quota.go 的既有信号：
// isUndeliveredTextResponse、streamCutKind、StreamStatus。
func ClassifyRequestOutcome(relayInfo *relaycommon.RelayInfo, completionTokens int, adminRejectReason string) RequestOutcome

// CountsAsRequest 返回该结果是否应消耗一次请求次数。
func (o RequestOutcome) CountsAsRequest() bool
```

规则：

| 结果 | 是否计数 |
| --- | --- |
| 正常完成 | 是 |
| 用户断流（客户端断开 / ping 失败） | 是 |
| 上游断流且输出 tokens > 1000 | 是 |
| 上游断流且输出 tokens <= 1000 | 否 |
| 空回（无可见输出、无工具调用、无媒体） | 否 |
| 请求失败（错误 / 退款） | 否 |

输出规模：以 `completion_tokens` 计量（与现有上游断流折扣分档 `truncatedResponseMultiplierForTokens`
的口径一致），阈值 `RequestCountMinOutputTokens = 1000`。

现有 `isUndeliveredTextResponse` / `streamCutKind` 改为复用该分类器的中间信号，避免两套判定漂移。

## 接口与前端

### 后端接口

- `GET/POST/PUT /api/subscription/admin/plans`：携带 `kind`、`upgrade_groups`、`request_rules`。
- `GET /api/subscription/plans`、`GET /api/subscription/self`：返回同上字段；
  请求次数型订阅额外返回 `request_counters`（每条：group、model、total、used）。
- 管理端用户订阅列表同样返回计数行。

校验：

- `kind` 仅允许三种值；
- `quota`：`total_amount > 0`；
- `request`：至少一条规则；每条规则分组存在、模型非空、count > 0；
- `upgrade_groups` 中每个分组必须存在于分组倍率配置。

### 前端

- `subscriptions-mutate-drawer.tsx`：顶部增加「订阅种类」下拉；按 kind 显示：
  - time：时长 + 额度 + 重置；
  - quota：额度；
  - request：请求规则编辑器（分组下拉 + 模型多选 + 次数），隐藏额度/时长/重置。
- 升级分组由单选改为多选（`MultiSelect`），第一项作为主升级分组。
- 用户侧订阅展示（`subscription-plans-card.tsx`）与管理员用户订阅弹窗：
  request 类型渲染计数横条组件，每条两行：颜色进度条 + `used/total` 数值。
- 新增 i18n key 必须走 `apps/web/scripts/add-missing-keys.mjs` + `bun run i18n:sync`，
  覆盖 en / zh / zh-TW / fr / ja / ru / vi。

## 合并兼容与迁移

- 新列通过既有 AutoMigrate 与 SQLite 手工 DDL（`bootstrap_subscription_plan.go`）补齐，默认值保证旧数据可读。
- `end_time = 0` 需要在所有「活跃订阅」查询中按「无时间上限」处理：
  `WHERE (end_time = 0 OR end_time > ?)`。涉及：
  `GetAllActiveUserSubscriptions`、`HasActiveUserSubscription`、`UserActiveSubscriptionsAllowWalletOverflow`、
  `PreConsumeUserSubscription`、`downgradeUserGroupForSubscriptionTx`、管理端重置查询。
- 前端判断过期时同样把 `end_time = 0` 视为不过期。

## 任务拆分

1. **后端模型与套餐 API**：字段、JSON 快照、校验、DDL、计数表与迁移。
2. **后端计费与计数**：FundingSource 扩展、预扣/结算/退款、计数表原子操作、
   统一结果分类器、`completion_tokens` 阈值判定、过期扫描扩展。
3. **前端**：种类下拉、多分组、规则编辑器、计数横条、i18n。
4. **测试**：分类器表驱动测试、分组限制与计数预扣/退款测试、过期语义测试。

## 验证

- 仅通过 PR CI：`go vet` / `go build` / `go test`、`bun run typecheck` / `bun test`。
- 本地允许：`gofmt -l <changed files>`、静态审阅、`git diff` 检查。
- 手工验收（由维护者在开发环境执行）：
  1. 创建三种 kind 的套餐，确认创建页下拉与字段联动；
  2. 购买 request 套餐，调用命中模型与未命中模型，确认计数增减；
  3. 模拟空回 / 上游断流（>1000 与 <=1000 tokens）/ 用户断流，确认计数结果；
  4. 用非升级分组调用，确认订阅额度不被消费。
