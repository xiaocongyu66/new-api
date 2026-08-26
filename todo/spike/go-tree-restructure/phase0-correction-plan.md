# Phase 0 矫正计划：对齐 plan.md 目标结构

> 日期：2026-08-27
> 基线：`epic/phase0` @ `cb55edf02`
> 设计稿：`todo/spike/go-tree-restructure/plan.md`（v2，2026-08-19）
> 上游 epic：#278（Phase 0~6 总路线），#281（当前 Phase 0）
> 前置审计：`phase0-coupling-audit.md`（函数级解耦度）、`phase0-deviation-analysis.md`（文件位置偏差）

## 一、问题定性

Phase 0 已完成的 gin 解耦和用例层迁移是**正确且有价值的工作**。但它做的是一个中间态——先建 `internal/` 骨架做 gin 边界隔离，没有完成 plan.md 要求的两件核心事：

1. **顶层散包收顶**：`apps/api/` 根目录应只剩 `main.go` + `web/` + `internal/` + `modules/`。当前 12 个平铺包仍在根目录。
2. **域名对齐 + 文件命名行为化**：plan.md 要求 `internal/identity/`，实际是 `internal/capabilities/identity/`（多了一层）；文件名要求行为动词，实际仍是结构体名。

矫正不是推倒重来。已完成的 173 个 `internal/` 文件、gin contract 边界、relay 解耦全部保留。矫正工作是"在已有骨架上做收顶 + 改名 + 归域"。

## 二、当前状态（精确数字）

### 已完成（保留不动）

| 项 | 状态 |
|---|---|
| transport contract + ginadapter | ✅ 16 文件，gin 仅在此层 |
| gateway 执行链 | ✅ 8 文件，relay/ gin 引用清零 |
| 7 capability 用例层 | ✅ 113 文件 |
| controller contract 化 | ✅ 234 handler 走 contract.Context，0 gin.Context |
| relaykit 独立性 | ✅ 0 个 relaykit 文件 import 根包 |
| 路由快照/契约测试 | ✅ 23 用例 |

### 未完成（本次矫正目标）

| 项 | 量 | 性质 |
|---|---|---|
| 顶层散包收进 internal/ | 12 包 / ~610 文件 | 机械 git mv + import 替换 |
| 去掉 capabilities/ 中间层 | ~115 文件 | 纯路径 rename |
| egress 域新建 | 13 文件 | 从 service/ 移出 |
| controller 归域 | 47 文件（22 有逻辑） | 逻辑归域 |
| service 归域 | 60 文件 | 逐文件归域 |
| setting 子包下沉 | 27 文件 / 9 子包 | 部分按 plan.md 下沉到域 |
| 文件命名行为化 | ~100 文件 | 机械改名 |

## 三、矫正路线（4 阶段，可拆 issue）

### 执行顺序与依赖

```
S1 域名对齐 → S2 基础设施收顶 → S3 业务包归域 → S4 文件命名行为化
```

S1 是纯 rename，先做。S2 是纯路径移动，跟在 S1 后面。S3 是 controller/service/model/setting 归域，需要逐文件决策归属。S4 最后做，纯改名。

---

### S1：域名对齐（去掉 capabilities/ 层）

**目标**：把 `internal/capabilities/{domain}/` 改为 plan.md 要求的 `internal/{domain}/`。

**为什么先做**：所有后续 S2/S3 的 import 路径都依赖最终域名。先做这步，后续移动的目标路径就确定了。

**操作清单**：

| 当前路径 | 目标路径 | 文件数 |
|---|---|---|
| `internal/capabilities/identity/` | `internal/identity/` | 19 |
| `internal/capabilities/channel/` | `internal/catalog/` | 16 |
| `internal/capabilities/billing/` | `internal/billing/` | 46 |
| `internal/capabilities/usage/` | `internal/usage/` | 6 |
| `internal/capabilities/administration/` | `internal/ops/` | 10 |
| `internal/capabilities/task/` | `internal/task/` | 13 |
| `internal/capabilities/integration/` | 合入 `internal/ops/` | 4 |

**import 路径替换**（脚本）：
```
github.com/QuantumNous/new-api/internal/capabilities/identity → .../internal/identity
github.com/QuantumNous/new-api/internal/capabilities/channel  → .../internal/catalog
github.com/QuantumNous/new-api/internal/capabilities/billing  → .../internal/billing
github.com/QuantumNous/new-api/internal/capabilities/usage    → .../internal/usage
github.com/QuantumNous/new-api/internal/capabilities/administration → .../internal/ops
github.com/QuantumNous/new-api/internal/capabilities/task    → .../internal/task
github.com/QuantumNous/new-api/internal/capabilities/integration → .../internal/ops
```

**触及**：~115 文件 git mv + ~250 处 import 路径替换（main.go、controller/、service/、middleware/、relay/、gateway/、transport/ 都 import capabilities/）

**别名处理**：当前代码中有 `channelcap "github.com/.../capabilities/channel"` 等别名。rename 后改为 `catalog "github.com/.../internal/catalog"` 或去掉别名直接用包名。

**验证**：
```bash
cd apps/api && GOWORK=off go build ./...
cd apps/api/modules/relaykit && GOWORK=off go build ./...
go vet ./...
```

**风险**：低。纯路径替换，无逻辑改动。go build 编译器会报出所有遗漏的 import。

---

### S2：基础设施收顶（纯路径移动）

**目标**：把 6 个基础设施平铺包收进 `internal/`，不改任何逻辑。

| 平铺包 | 目标 | 文件数 | 复杂度 |
|---|---|---|---|
| `common/` | `internal/common/` | 61 | 中（全域 import common，~16 处 internal→common 边） |
| `constant/` | `internal/constant/` | 13 | 低 |
| `dto/` | `internal/dto/` | 4 | 低 |
| `types/` | `internal/types/` | 3 | 低 |
| `logger/` | `internal/logger/` | 1 | 低 |
| `i18n/` | `internal/i18n/` | 2 | 低 |

**注意**：`common/` 是被引用最多的包（internal 16 处 + controller/service/model/relay/middleware 全引用）。移动它需要一次性替换所有 import 路径。

**relaykit 边界**：relaykit 不 import `common/`（已验证），所以移动 common/ 不影响 relaykit。

**执行顺序**：先移动低复杂度的（constant/dto/types/logger/i18n），最后移动 common/。

**验证**：
```bash
cd apps/api && GOWORK=off go build ./...
cd apps/api/modules/relaykit && GOWORK=off go build ./...
go test ./... -count=1
```

**触及**：~84 文件 git mv + ~300 处 import 路径替换

**风险**：低。`common/` 移动面大但纯路径，编译器兜底。

---

### S3：业务包归域（核心难点）

这是最大的一步。controller/、service/、model/、setting/、middleware/、relay/、pkg/ 七个平铺包需要归域。

#### S3a：middleware/ → internal/http/

| 当前 | 目标 | 文件数 | 说明 |
|---|---|---|---|
| `middleware/` | `internal/http/` | 19（非测试） | 4 个已在 transport/middleware/，19 个还在顶层 |

已有 `internal/transport/middleware/`（cors/gzip/logger/trusted_proxies）。plan.md 要求 `internal/http/`。

**决策点**：`transport/middleware/`（4 个引擎级）和 `middleware/`（19 个业务级）是合并为 `internal/http/` 还是保持分离？

**建议**：合并到 `internal/http/`。plan.md 没有区分引擎级和业务级 middleware，统一收进 `internal/http/`。`transport/ginadapter` 只保留 engine 构建逻辑，middleware 本身移到 `http/`。

#### S3b：relay/ → internal/relay/

| 当前 | 目标 | 文件数 |
|---|---|---|
| `relay/` 顶层 | `internal/relay/` | 14（顶层 handler） |
| `relay/channel/` | `internal/relay/channel/` | 45 子目录 |
| `relay/common/` | `internal/relay/common/` | — |
| `relay/constant/` | `internal/relay/constant/` | — |
| `relay/helper/` | `internal/relay/helper/` | — |

**relaykit 边界**：relay/ 有 147 个文件 import relaykit/。relaykit 不 import relay/（已验证）。移动 relay/ 不影响 relaykit 独立编译。

**风险**：relay/ 移入 internal/ 后，relaykit 模块的 `go.mod` replace 指向需验证是否需更新。relaykit 的 module path 是 `github.com/QuantumNous/new-api/relaykit`（不在 relay/ 下），移动 relay/ 不改变 relaykit 的 module path。

#### S3c：controller/ → 各域 handler

controller/ 47 个文件（非测试）。17 个已是薄委托（<30行），直接移入对应域。22 个有逻辑，需归域。

**归域映射**（基于 analysis.md 域划分 + plan.md 域名）：

| controller 文件 | 目标域 | 行数 | 说明 |
|---|---|---|---|
| channel.go | catalog/ | 2230 | 渠道 CRUD + 管理 |
| channel-test.go | catalog/ | 1100 | 渠道测试 |
| channel_upstream_update.go | catalog/ | 1108 | 上游更新 |
| channel_affinity_cache.go | catalog/ | 88 | 亲和缓存 |
| channel_authz.go | catalog/ | 136 | 渠道权限 |
| model.go | catalog/ | 356 | 模型管理 |
| model_sync.go | catalog/ | 634 | 模型同步 |
| missing_models.go | catalog/ | 28 | 缺失模型 |
| group.go | catalog/ | 52 | 分组 |
| prefill_group.go | catalog/ | 89 | 预填组 |
| pricing.go | catalog/ | 14 | 定价（薄委托） |
| ratio_config.go | catalog/ | 10 | 倍率配置（薄委托） |
| ratio_sync.go | catalog/ | 14 | 倍率同步（薄委托） |
| codex_usage.go | catalog/ | 164 | codex 用量 |
| playground.go | catalog/ | 56 | playground |
| image.go | catalog/ | 9 | 图片（薄委托） |
| proxy.go | ops/ | 77 | 代理配置 |
| relay.go | task/ | 759 | relay 执行入口 |
| task.go | task/ | 87 | 任务 |
| midjourney.go | task/ | 329 | midjourney |
| video_proxy.go | task/ | 11 | 视频代理（薄委托） |
| user.go | identity/ | 86 | 用户管理 |
| auth_session.go | identity/ | 26 | 会话（薄委托） |
| token.go | identity/ | 54 | API token |
| oauth.go | identity/ | 14 | OAuth（薄委托） |
| custom_oauth.go | identity/ | — | 自定义 OAuth |
| passkey.go | identity/ | — | passkey |
| twofa.go | identity/ | — | 2FA |
| wechat.go | identity/ | 14 | 微信（薄委托） |
| telegram.go | identity/ | 18 | telegram（薄委托） |
| secure_verification.go | identity/ | 89 | 安全验证 |
| billing.go | billing/ | — | 计费 |
| topup*.go (5文件) | billing/ | — | 充值（薄委托） |
| subscription*.go (5文件) | billing/ | — | 订阅 |
| redemption.go | billing/ | — | 兑换码 |
| checkin.go | billing/ | — | 签到 |
| log.go | usage/ | — | 日志（薄委托） |
| usedata.go | usage/ | 26 | 用量（薄委托） |
| rankings.go | usage/ | 24 | 排名（薄委托） |
| perf_metrics.go | usage/ | 14 | 性能指标（薄委托） |
| performance.go | usage/ | — | 性能 |
| misc.go | identity/ | 367 | 杂项（状态检查等） |
| setup.go | ops/ | 14 | 初始化（薄委托） |
| option.go | ops/ | 14 | 系统选项（薄委托） |
| system_info.go | ops/ | 65 | 系统信息 |
| system_task.go | ops/ | 116 | 系统任务 |
| system_task_handlers.go | ops/ | 165 | 系统任务处理 |
| karmada_dashboard.go | ops/ | — | karmada（薄委托） |
| uptime_kuma.go | ops/ | 10 | uptime（薄委托） |
| authz.go | identity/ | 26 | 权限路由 |

**注意**：controller 文件归域后，包名从 `package controller` 改为 `package catalog` / `package identity` 等。handler 函数签名不变（已用 contract.Context）。

#### S3d：service/ → 各域

60 个 service 文件（非测试）。按域归类：

| service 文件 | 目标域 | 说明 |
|---|---|---|
| auth_session.go, auth_token.go, auth_cleanup.go | identity/ | 认证会话 |
| authz/ (9 文件) | identity/ | 权限策略 |
| passkey/ (3 文件) | identity/ | passkey 服务 |
| channel.go, group.go, gateway_config_outbox.go | catalog/ | 渠道服务 |
| codex_*.go (6 文件) | catalog/ | codex 渠道 |
| proxy_config.go, proxy_node.go, proxy_node_parser.go, proxy_node_probe.go | catalog/ | 代理节点 |
| epay.go, webhook.go, task_billing.go | billing/ | 支付/计费 |
| task.go, task_polling.go, midjourney.go | task/ | 任务 |
| rankings.go, usage_helpr.go, token_counter.go, token_estimator.go, tokenizer.go, log_info_generate.go | usage/ | 用量/日志 |
| karmada_dashboard_session.go, notify-limit.go, user_notify.go | ops/ | 运维 |
| **singbox_*.go (6), http*.go (4), download.go, protected_fetch_client.go** | **egress/** (新建) | 出口网络 |
| convert.go, error.go, file_*.go, image.go, audio.go, str.go, openai_chat_responses*.go, request_converter.go, return_path.go | internal/common/ | 基础设施 |
| sensitive*.go (6 文件) | internal/common/ 或独立 | 敏感词 |

#### S3e：model/ → 保留 + store 下沉

plan.md 明确允许 GORM record 留在 model/（Go 不能跨包定义 method）。model/ 的 52 个文件大部分是 struct 定义 + 持久化助手。

**需要做的**：
1. model/ 目录保留，但改名为 `internal/model/`（收进 internal/）。
2. 各域的 store 文件已在 internal/{domain}/ 里（user_store.go 在 identity/、channel store 在 catalog/），保持不变。
3. model/ 中的业务方法（非 GORM 方法）已在之前迁入 capability，不再回迁。

**触及**：model/ 52 文件 git mv 到 `internal/model/` + import 路径替换。

#### S3f：setting/ → internal/settings/ + 子包下沉

plan.md 要求 setting 子包下沉到各域：

| setting 子包 | plan.md 去向 | 说明 |
|---|---|---|
| `ratio_setting/` | catalog/configure_ratio | 倍率配置 |
| `model_setting/` | catalog/manage_models | 模型设置 |
| `system_setting/` | egress/fetch_url | fetch/worker/ssrf |
| `operation_setting/` | catalog/manage_channels | 自动禁言等 |
| `console_setting/` | usage/record_perf | |
| `performance_setting/` | usage/record_perf | |
| `perf_metrics_setting/` | usage/record_perf | |
| `billing_setting/` | billing/lib.rs | 计费设置 |
| `config/` | internal/settings/ | 保留 |
| `reasoning/` | billing/ | 推理设置 |
| setting/ 顶层散文件 (auto_group, chat, midjourney, payment_*, rate_limit, sensitive, sentence_filter, user_*) | internal/settings/ | 通用配置 |

**触及**：27 文件拆分 + 归域 + import 路径替换

#### S3g：pkg/ → 各域下沉

| pkg 子包 | 去向 |
|---|---|
| `pkg/billingexpr/` | billing/price_expression |
| `pkg/cachex/` | internal/common/ |
| `pkg/perf_metrics/` | usage/record_perf |

**触及**：12 文件

**S3 总验证**：
```bash
cd apps/api && GOWORK=off go build ./...
cd apps/api/modules/relaykit && GOWORK=off go build ./...
go vet ./...
go test ./... -count=1
# 顶层散包清零：
ls apps/api/ | grep -v main.go | grep -v web | grep -v internal | grep -v modules | grep -v go.mod | grep -v go.sum | grep -v README
# 预期：无输出
```

**S3 风险**：
- controller 归域改包名后，route 注册中的 import 要全量更新
- service 跨域引用需逐个处理（task_billing 引 billing、log_info_generate 引 usage）
- model/ 改为 internal/model/ 后，所有 import model 的地方都要改路径

---

### S4：文件命名行为化

plan.md 要求文件名 = 动词（做什么）。当前全是结构体名。

**规则**（plan.md 原文）：
- `user.go` → `manage_users.go`
- `channel.go` → `manage_channels.go`
- `token.go` → `manage_tokens.go`
- `authenticate.go` / `terminate_session.go` / `enroll_passkey.go`
- 禁止 `util.go` `helper.go` `common.go` `model.go` `handler.go`

**触及**：~100 文件改名 + import 路径替换（Go import 用包路径不用文件名，所以改名不需要改 import，但如果有 `//go:build` tag 或 IDE 引用需要更新）

**风险**：低。Go 不按文件名 import，改名不影响编译。但需 `gofmt` + `go vet` 兜底。

---

## 四、Issue 拆分建议

放到 #278 下，作为 Phase 0 矫正 sub-issues。每个 S 是一个独立 issue + PR：

| Issue 标题 | 对应阶段 | 文件量 | 依赖 | 估时 |
|---|---|---|---|---|
| Phase 0 矫正 S1：域名对齐 | S1 | ~115 | 无 | 0.5d |
| Phase 0 矫正 S2：基础设施收顶 | S2 | ~84 | S1 | 0.5d |
| Phase 0 矫正 S3a：middleware 归域 | S3a | ~23 | S2 | 0.5d |
| Phase 0 矫正 S3b：relay 归域 | S3b | ~197 | S2 | 1d |
| Phase 0 矫正 S3c：controller 归域 | S3c | ~47 | S1, S2 | 1.5d |
| Phase 0 矫正 S3d：service 归域 | S3d | ~60 | S2, S3c | 1.5d |
| Phase 0 矫正 S3e：model 收顶 | S3e | ~52 | S2 | 0.5d |
| Phase 0 矫正 S3f：setting 下沉 | S3f | ~27 | S3c, S3d | 1d |
| Phase 0 矫正 S3g：pkg 下沉 | S3g | ~12 | S3d | 0.25d |
| Phase 0 矫正 S4：文件命名行为化 | S4 | ~100 | S3 全部 | 1d |
| **合计** | | **~680** | | **~8d** |

**可并行的波次**：
- 波 1：S1（独立）
- 波 2：S2 + S3e + S3b（S2 依赖 S1 完成后，S3e 和 S3b 只依赖 S2 的 common/ 收顶）
- 波 3：S3a + S3c（controller 和 middleware 归域）
- 波 4：S3d + S3g（service 和 pkg 归域，依赖 controller 归域确定域边界）
- 波 5：S3f（setting 下沉，依赖 controller 和 service 归域确定消费方）
- 波 6：S4（命名，最后做）

## 五、与 #281 已完成工作的关系

| #281 子 issue | 完成的部分 | 矫正阶段保留/调整 |
|---|---|---|
| #288 行为基线 | 路由快照 + 契约测试 | 保留不动 |
| #289 Transport 边界 | contract + ginadapter | 保留不动 |
| #290 Identity | capability 用例层 | S1 改路径 `capabilities/identity/` → `identity/` |
| #291 Channel | capability 用例层 | S1 改路径 `capabilities/channel/` → `catalog/` |
| #292 Billing | capability 用例层 | S1 改路径 |
| #384 Usage | capability 用例层 | S1 改路径 |
| #385 Task | capability 用例层 | S1 改路径 |
| #386 Administration | capability 用例层 | S1 改路径 `administration/` → `ops/` |
| #387 Integration | capability 用例层 | S1 合入 `ops/` |
| #388 Gateway | gateway 执行链 | 保留不动 |
| #389 Transport 收口 | router → compose | S3a 合并到 `http/`，compose 改名 `bootstrap/` |
| #390 验证 | 契约测试 | 保留不动 |

## 六、plan.md 待拍板项的处理

| 决策点 | plan.md 选项 | 建议 | 理由 |
|---|---|---|---|
| handler 命名 | `HandleCreate(c)` vs `Create(c)` | `HandleCreate` | HTTP 入口显式，避免和域内业务函数混淆 |
| oauth 命名 | `brokeroauth` vs `oauth` | `internal/oauth/` | 已有 `internal/security/oauth/`，保名更简单 |
| bootstrap 命名 | `bootstrap` vs `app` vs `router` | `bootstrap` | plan.md 推荐 |
| proxynode 归属 | egress vs catalog | egress | 类型归 egress，管理 handler 引用 egress |
| 分支 | 新分支 | 基于 `epic/phase0` 继续 | 不新建分支，在 epic 上累积 |

## 七、验收标准（复用 plan.md 原文 + 补充）

```bash
# 编译
cd apps/api && GOWORK=off go build ./...
cd apps/api/modules/relaykit && GOWORK=off go build ./...

# 测试
cd apps/api && GOWORK=off go vet ./... && GOWORK=off go test ./... -count=1

# 顶层散包清零
ls apps/api/ | grep -vE 'main\.go|web|internal|modules|go\.(mod|sum)|README'
# 预期：无输出

# 无旧目录残留
test ! -d apps/api/controller
test ! -d apps/api/service
test ! -d apps/api/middleware
test ! -d apps/api/router
test ! -d apps/api/oauth
test ! -d apps/api/relay
test ! -d apps/api/model
test ! -d apps/api/common
test ! -d apps/api/setting
test ! -d apps/api/constant
test ! -d apps/api/dto
test ! -d apps/api/types
test ! -d apps/api/logger
test ! -d apps/api/i18n
test ! -d apps/api/pkg

# 无 capabilities/ 中间层
test ! -d apps/api/internal/capabilities

# gin 只在 transport/ginadapter
grep -rln 'gin-gonic/gin' apps/api/ --include='*.go' | grep -v _test | grep -v internal/transport/ginadapter
# 预期：无输出

# relaykit 独立
cd apps/api/modules/relaykit && GOWORK=off go build ./... && GOWORK=off go test ./...

# 路由快照不变
cd apps/api && go test ./internal/transport/... -run TestRegisteredRoutesMatchSnapshot -count=1
```

## 八、风险与缓解

| 风险 | 级别 | 缓解 |
|---|---|---|
| controller 改包名导致 route 注册全量改 | 中 | S3c 一个 PR 做完，编译器兜底 |
| service 跨域引用成环 | 高 | 逐文件分析引用方向；跨域 service 保留在 common/ 或用 port 反转 |
| relay/ 移入 internal/ 后 relaykit replace 指向 | 中 | relaykit module path 不变（`/relaykit` 不在 `/relay` 下），验证 go.mod |
| model 改 internal/model/ 后全域 import 路径 | 中 | 一次性替换，编译器兜底 |
| setting 子包下沉后引用方改路径 | 中 | S3f 集中处理，35 处引用一次性更新 |
