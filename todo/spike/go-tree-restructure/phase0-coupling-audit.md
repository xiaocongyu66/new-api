# Phase 0 真实解耦度审计

> 日期：2026-08-27
> 基线：`epic/phase0` @ `cb55edf02`（worktree `.wt/epic400`）
> 方法：`go list -json` 包级依赖图 + codegraph 全量构建（2069 文件 / 34170 nodes / 91586 edges）
> 设计稿：`todo/spike/go-tree-restructure/plan.md`

## 一、函数级解耦已基本完成

plan.md 要求的核心是"业务代码不感知 gin"。这条已经做到：

| 指标 | 数值 |
|---|---|
| controller handler 用 `contract.Context` | 234 个函数 |
| controller handler 用 `gin.Context` | 0 个生产函数（2 个仅测试） |
| relay/ 生产代码用 `gin.Context` | 0 |
| relay/ 44 provider adapter | 全部走 `contract.Context` / `IngressContext` |
| 生产代码 gin import | 16 文件，**全部在 `internal/transport/` 下** |
| controller/ 导入 internal/ | 37/47 文件（78%），剩 10 个纯 infra |

**结论：gin 解耦 = 100%。** 所有 handler 和 relay adapter 已切到 framework-neutral contract，gin 只在 transport/ginadapter 适配层。

## 二、未对齐 plan.md 的三件事

### 2.1 顶层散包没收顶（129 条 internal→flat 依赖边）

internal 仍 import 29 个顶层平铺包。按类别：

| 类别 | 边数 | 说明 |
|---|---|---|
| **基础设施** (common/logger/constant/i18n/types/dto) | 45 | 纯路径移动即可 |
| **Settings** (setting/* 9 子包) | 35 | 部分需按 plan.md 下沉到域 |
| **Model/Service/Controller/Middleware** | 23 | model=10, service=8, 其余=5 |
| **Relay/Relaykit** | 21 | relaykit 是独立 module 不能动；relay/common+constant 收进 internal/relay |
| **Pkg** | 5 | billingexpr→billing, perf_metrics→usage, cachex→common |

### 2.2 域名映射偏差

| plan.md | 实际 | 差距 |
|---|---|---|
| `internal/identity/` | `internal/capabilities/identity/` | 多一层 `capabilities/` |
| `internal/catalog/` | `internal/capabilities/channel/` | 改名 + 多一层 |
| `internal/billing/` | `internal/capabilities/billing/` | 多一层 |
| `internal/usage/` | `internal/capabilities/usage/` | 多一层 |
| `internal/ops/` | `internal/capabilities/administration/` | 改名 + 多一层 |
| `internal/task/` | `internal/capabilities/task/` | 多一层 |
| `internal/egress/` | **不存在** | singbox/http/proxy 仍在 service/ |
| `internal/http/` | `internal/transport/middleware/`（4个）+ `middleware/`（23个） | 部分 |
| `internal/bootstrap/` | `internal/transport/compose/` | 改名 |
| `internal/brokeroauth/` | `internal/security/oauth/` | 改名 |
| `internal/common/` | `common/`（顶层） | 未收 |
| `internal/settings/` | `internal/settings/`（1文件）+ `setting/`（27文件） | 大部分未收 |

### 2.3 文件命名未行为化

plan.md 要求 `user.go` → `manage_users.go` / `authenticate.go`。实际仍是结构体命名。

## 三、实际未解耦的文件清单（按归域分类）

### 3.1 model/ → 归域（52 文件，但大部分是 GORM record 不需动）

plan.md 说"GORM record 留 model/，store 下沉到域"。实际 store 已部分下沉（user_store.go, channel store 等已在 capabilities/）。真正需动的是：

| 域 | 需归域的 model 文件 | 说明 |
|---|---|---|
| identity | user.go, twofa.go, passkey.go, user_session.go, user_cache.go, user_auth_cache.go, user_oauth_binding.go, auth_flow.go, authz_role.go, casbin_rule.go, external_identity_claim.go, custom_oauth_provider.go | GORM struct 留 model/，业务方法已部分迁出 |
| catalog(channel) | channel.go, ability.go, pricing.go, pricing_default.go, pricing_refresh.go, model_meta.go, model_extra.go, proxy_node.go, vendor_meta.go, gateway_config_revision.go, prefill_group.go, missing_models.go | 同上 |
| billing | topup.go, redemption.go, subscription.go, checkin.go, quota_reserve.go | 同上 |
| task | task.go, midjourney.go | 同上 |
| usage | log.go, usedata.go, usedata_flow.go, usedata_rankings.go, perf_metric.go | 同上 |
| ops(administration) | system_instance.go, system_task.go | 同上 |
| infra | main.go, setup.go, utils.go, errors.go, locking.go, db_time.go, gorm_logger.go, frontend_option_migration.go, token.go, token_cache.go | token.go 属 identity，其余留 infra |

**关键：model/ 不需要删。** plan.md 说的是 record 留在 model/（Go 不能跨包定义 method），业务方法迁入 capability store。**这一步已大量完成。** model/ 的 52 个文件中，大部分只剩 GORM struct 定义 + 被 model 内部方法共享的持久化助手。

### 3.2 service/ → 归域（60 文件）

| 归属 | 文件数 | 文件 |
|---|---|---|
| **egress** (需新建) | 12 | singbox_dialer, singbox_registry*, http*, download, protected_fetch_client |
| **infra** (→ internal/common) | 11 | convert, error, file_decoder, file_service, image, audio, str, openai_chat_responses*, request_converter, return_path |
| **sensitive** (→ internal/common 或独立) | 6 | sensitive* |
| **identity** | 3+authz/9+passkey/3 = 15 | auth_session, auth_token, auth_cleanup, authz/*, passkey/* |
| **catalog(channel)** | 13 | channel, group, gateway_config_outbox, codex_*, proxy_* |
| **billing** | 3 | epay, webhook, task_billing |
| **task** | 3 | task, task_polling, midjourney |
| **usage** | 6 | rankings, usage_helpr, token_counter, token_estimator, tokenizer, log_info_generate |
| **ops(administration)** | 3 | karmada_dashboard_session, notify-limit, user_notify |

### 3.3 controller/ → 归域（47 文件）

| 状态 | 文件数 | 说明 |
|---|---|---|
| 已薄委托（<30行，直接删移到域） | 17 | image, ratio_config, uptime_kuma, video_proxy, oauth, option, perf_metrics, pricing, ratio_sync, setup, wechat, telegram, rankings, auth_session, authz, usedata, missing_models |
| 有逻辑但已用 contract（需迁入域） | 22 | channel.go(2230行), relay.go(759行), model_sync.go(634行), misc.go, model.go, midjourney.go, system_task_handlers, codex_usage, channel_authz, system_task, secure_verification, prefill_group, channel_affinity_cache, task, user, proxy, system_info, playground, token, group, channel-test, channel_upstream_update |
| 测试文件 | ~25 | 随主体迁移 |

### 3.4 middleware/ → internal/http（23 文件）

已有 4 个迁入 `internal/transport/middleware/`（cors/gzip/logger/trusted_proxies）。剩 23 个在顶层 middleware/。

### 3.5 setting/ → 各域下沉（27 文件，9 子包）

plan.md 要求各子包下沉到域：
- ratio_setting → catalog/configure_ratio
- model_setting → catalog/manage_models
- system_setting → egress/fetch_url
- operation_setting → catalog/manage_channels
- console_setting, performance_setting, perf_metrics_setting → usage/record_perf
- billing_setting, reasoning, config → billing/lib.rs

## 四、真实完成度修正

| 维度 | 之前估计 | 修正后 | 依据 |
|---|---|---|---|
| gin 解耦 | ~30% | **100%** | 234 handler 全走 contract，0 gin.Context 生产代码 |
| 用例层迁移 | ~30% | **~85%** | 37/47 controller 已导入 capability，7 域用例建成 |
| 顶层散包收顶 | 0/12 | **0/12** | 仍需做（plan.md S-06 核心） |
| 文件命名行为化 | 0% | **0%** | 未开始 |
| egress 域 | 不存在 | **未建** | 12 个 service 文件等归域 |
| model 归域 | 0% | **~50%** | store 已下沉，record 留 model/（plan.md 允许） |

**实际完成度：约 70%，不是 30%。**

gin 解耦 + 用例层 + gateway 迁移是重活（3w 行代码改了），这部分已完成。剩余的是机械性收顶（git mv + import 路径替换 + 域名调整）。

## 五、对齐 plan.md 的工作量

### Phase 1: 域名对齐 + capabilities/ 层去掉（低风险，纯 rename）

| 操作 | 触及文件 | 风险 |
|---|---|---|
| `internal/capabilities/identity/` → `internal/identity/` | ~20 文件 + import 更新 | 低（纯路径） |
| `internal/capabilities/channel/` → `internal/catalog/` | ~16 文件 + import 更新 | 低 |
| `internal/capabilities/billing/` → `internal/billing/` | ~46 文件 + import 更新 | 低 |
| `internal/capabilities/usage/` → `internal/usage/` | ~6 文件 + import 更新 | 低 |
| `internal/capabilities/administration/` → `internal/ops/` | ~10 文件 + import 更新 | 低 |
| `internal/capabilities/task/` → `internal/task/` | ~13 文件 + import 更新 | 低 |
| `internal/capabilities/integration/` → 合入 `internal/ops/` 或 `internal/integration/` | ~4 文件 | 低 |

**~115 文件 rename + import 路径替换，脚本可做。**

### Phase 2: 收顶（plan.md S-06，中风险）

| 操作 | 文件数 | 复杂度 |
|---|---|---|
| `common/` → `internal/common/` | 61 | 低（纯路径） |
| `constant/` → `internal/constant/` | 13 | 低 |
| `dto/` → `internal/dto/` | 4 | 低 |
| `types/` → `internal/types/` | 3 | 低 |
| `logger/` → `internal/logger/` | 1 | 低 |
| `i18n/` → `internal/i18n/` | 2 | 低 |
| `middleware/` → `internal/http/` | 23 | 中（依赖 model/service） |
| `setting/` → `internal/settings/`（部分下沉到域） | 27 | 中高（9 子包，35 条引用） |
| `controller/` → 各域 handler | 47 | 高（22 个有逻辑需归域） |
| `service/` → 各域 | 60 | 高（跨域引用需处理） |
| `pkg/` → 各域下沉 | 14 | 低 |
| `relay/` → `internal/relay/` | 197 | 高（relaykit 独立性需验证） |

**~450 文件移动。大部分是 git mv + sed 替换，controller 和 service 归域是难点。**

### Phase 3: egress 域新建（中风险）

| 操作 | 文件数 | 说明 |
|---|---|---|
| `service/singbox_*` → `internal/egress/` | 6 | sing-box 拨号 |
| `service/http*` → `internal/egress/` | 5 | HTTP 客户端/传输策略 |
| `service/download.go` → `internal/egress/` | 1 | SSRF 防护 |
| `service/protected_fetch_client.go` → `internal/egress/` | 1 | |

**13 文件，纯移动。**

### Phase 4: 文件命名行为化（低风险，机械）

plan.md 要求 `user.go` → `manage_users.go` 等。约 100 文件改名。

### 总量修正

| 项 | 之前估计 | 修正后 | 理由 |
|---|---|---|---|
| Phase 1 域名对齐 | — | ~115 文件 | 纯 rename + import |
| Phase 2 收顶 | ~660 | ~450 文件 | model/ 不需全移（record 留），service 分域即可 |
| Phase 3 egress | — | ~13 文件 | 纯移动 |
| Phase 4 改名 | ~100 | ~100 文件 | 机械 |
| **合计** | **~960** | **~680 文件** | gin 解耦已完成省掉大量工作 |

**真实工作量约 680 文件触及，不是 960。** 且大部分是 git mv + import 路径替换的机械操作，真正的逻辑决策只在 controller 归域（22 文件有逻辑）和 service 跨域引用处理。
