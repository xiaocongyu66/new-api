---
name: performance-test-evidence
summary: 要求性能、压力、容量、稳定性或统计分布测试具有可复算的场景规格、原始证据、系统资源观测和 PR 审计记录。用户提到压测、负载、压力、benchmark、性能报告、k6、统计置信区间、吞吐、延迟、CPU、内存、磁盘、波动或稳定性时，即使只是补一个场景或工作流，也必须使用。
---

# 性能压测证据工作流

性能测试的结论来自可复算数据，不来自脚本退出码或一段 Summary。先定义实验，再实现；每个场景必须能回答：压了什么、为什么压、压了多久/多少、系统实际做了什么、资源是否饱和、结论是否由原始数据支持。

## 范围与隔离

1. 先读关联 issue、现有压测目录、workflow 和生产路径。明确被测行为、非目标、已有测试不能覆盖的运行时风险。
2. 新压测不得悄悄改变旧压测的 seed、断言、资源或结论。不同子系统、聚合身份或成功条件不同，使用独立目录、独立 workflow 和独立 artifact 名称。
3. 复用 HTTP 客户端、artifact 上传或主机采样代码可以；不得复用已经不符合当前数据模型的 route/channel seed 或断言。
4. 先做小规模数据链 smoke：验证每个请求能关联到 gateway 选择记录、upstream/mock 实收记录和结果记录。数据链不完整时停止，不开始统计大样本。

## 场景规格：实现前写入 Issue 正文

每个场景必须在 Issue 正文有以下字段；不能只放 comment：

```markdown
### S<N> 名称

- 目标：要证伪的实现风险和通过后能说明的结论。
- 拓扑：副本数、group/alias、route 四元组、权重、key/upstream mapping、状态机和 option 设置。
- 上游模型：每条 route 的成功/失败/429/Retry-After/TTFT/TPS/长尾行为；mock 如何用 request id 和 key identity 证明它收到预期路由。
- 负载：预热请求数、统计请求数、并发/到达率、最大运行时间、pacing、流式比例和请求体大小。
- 统计：目标点值、容差、置信水平、统计量、最小有效样本；预热和失败/重试如何从统计窗口处理。
- 收集：请求、route、上游、资源、过程窗口和配置快照的精确字段。
- 判定：通过、数据不完整、环境失败、产品行为失败各自的机器可判定条件。
- 风险：该场景能抓到的错误实现，以及不覆盖什么。
```

样本量必须由目标精度或 power 推导。提高样本量时同时提高预热以保持每 route 的 EWMA 收敛；多副本时按副本数放大全局请求数。

## 必采原始证据

每次运行必须上传原始 artifact。只上传 Markdown 汇总或截图视为无证据。

### 请求和选路

每条客户端请求和每次上游 attempt 都必须有可关联的 `request_id`。至少记录：

```text
request_id, attempt, timestamp, scenario, pod_id,
alias, group, channel_id, key_index, upstream_model,
route_id_or_route_key, HTTP_status, outcome,
latency_ms, ttft_ms, stream, completion_tokens,
retry_index, client_error
```

上游 mock 还必须记录：

```text
request_id, attempt, received_key_identity, received_upstream_model,
configured_fault, HTTP_status, first_byte_ms, completion_ms
```

按 `(request_id, attempt)` 对账。报告必须给出总记录数、匹配数、缺失 gateway 记录、缺失 upstream 记录、重复记录和身份不一致数。任何非零不一致都使场景结论为 `DATA_INVALID`，不能标 PASS。

### 调度与统计

每条 route / 每个统计窗口输出：

```text
group, alias, channel_id, key_index, upstream_model,
selected_count, attempt_count, opportunity_count,
expected_share, observed_share, Wilson_95_CI_low, Wilson_95_CI_high,
base_weight, ewma_quality, health_multiplier, share_correction,
final_score, sample_count
```

多副本必须同时输出 per-pod 和合并后的全局计数；全局份额只能从原始四元组计数重新聚合，不能平均各 pod 百分比。

### 黑盒服务质量

按成功与失败分别记录：请求量、完成量、RPS、成功率、失败率、HTTP 状态分布、超时/取消/重试数、P50/P90/P95/P99/最大 latency、TTFT、流式 ITL、吞吐、dropped iterations 和连接错误。平均值只能辅助，不能替代 tail percentile。

### 白盒资源与饱和度

按固定短窗口采样（默认 1 秒；无法做到时记录实际间隔），并输出 min/mean/p50/p95/p99/max 以及时间序列：

```text
CPU: user/system/iowait/steal %, load average, run queue, cgroup throttle count/time
Memory: RSS, heap_alloc/inuse, cgroup usage/limit, available memory, GC pause/count, OOM/restart
Disk: filesystem used/free/inodes, read/write B/s and IOPS, device util %, await, queue depth, I/O errors
Network: RX/TX B/s, retransmits/drops/errors, open connections
Process/runtime: goroutines, file descriptors, DB/Redis connections or pool waits when exposed
Kubernetes: requested/limit, pod restart count, CPU/memory throttling, pod/node placement
```

不能采集的字段必须列成 `NOT_AVAILABLE` 并说明原因；不能静默缺失。CPU、内存和磁盘只报起止快照不够，必须提供波动期时间序列，与吞吐/延迟/错误同一时间基准对齐。

## 过程窗口与稳定性

对长跑、阶跃、反馈或资源测试，必须保存固定窗口（默认 10 秒）的时间序列：到达/完成 RPS、成功/失败、P50/P95/P99、各 route share、corr、EWMA、CPU、RSS/heap、磁盘 I/O、网络错误。报告必须标出预热、稳态、故障注入、恢复和冷却窗口，避免将预热或故障切换混进稳态结论。

稳定性场景必须声明具体上界，例如滚动份额标准差、连续窗口偏差、corr p99 到 clamp 的余量、恢复时间，不能只写“没有震荡”。

## 资源解释

遵循 Google SRE 的 latency、traffic、errors、saturation 四信号，以及 USE 的 utilization、saturation、errors：

- 成功与失败延迟分开；快失败不能掩盖慢成功或慢错误。
- CPU、磁盘、网络高利用必须同时看队列、throttle、drop 或 error，避免平均值掩盖短尖峰。
- 延迟或错误退化若与饱和指标无关，报告不得归因为资源瓶颈，只能列为未证实关联。
- 测试发生环境故障、容量不足、观测断流或 generator 饱和时，分类为 `ENVIRONMENT_INVALID`，保留数据但不得形成产品通过结论。

参考：Google SRE《Monitoring Distributed Systems》四个 golden signals；Brendan Gregg USE Method；Grafana k6 built-in metrics reference。

## Artifact 和报告契约

每个场景 artifact 至少包含：

```text
manifest.json                 # commit、镜像 digest、配置、拓扑、时钟、工具版本、随机种子
requests.ndjson               # 客户端请求级数据
gateway-attempts.ndjson       # gateway route 四元组数据
upstream-received.ndjson      # mock/upstream 实收数据
route-before.json             # route rows、options、health/EWMA 初始快照
route-after.json              # 完成快照
shares.csv                    # 份额、Wilson CI、因素分解和 per-pod/global 行
windows.csv                   # 固定窗口的服务与资源时间序列
resources.csv                 # 节点/pod/进程资源时间序列
summary.json                  # 机器可读判定、样本、对账和阈值结果
report.md                     # 人可读报告
```

`summary.json` 需将每个场景标为恰好一个：`PASS`、`PRODUCT_FAIL`、`DATA_INVALID`、`ENVIRONMENT_INVALID`。`report.md` 要解释指标、偏差、资源关联和未覆盖风险，但不得把无数据或环境失败写成通过。

## PR 交付记录

压测 PR 的 `## Delivery record` 和首条验证评论必须链接完整 workflow run 与 artifact，并逐场景记录：

1. 场景版本、commit、镜像 digest、拓扑、负载、预热/统计样本与运行时间；
2. 四元组对账数和所有不匹配数；
3. 份额点估计、95% CI、目标、容差、判定；
4. 成功/失败分离的服务质量指标；
5. CPU、内存、磁盘、网络、throttle/restart 的峰值及波动期证据；
6. `PASS`/`PRODUCT_FAIL`/`DATA_INVALID`/`ENVIRONMENT_INVALID` 与原始 artifact 路径；
7. 已知限制和未覆盖项。

PR 只在每个勾选项都有上述可下载数据和可复跑命令时标记完成。工作流退出 0、只含 Summary、或仅有日志摘要都不能替代原始数据。
