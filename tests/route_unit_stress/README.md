# Route-Unit EWMA Stress Tests (S1–S6)

## 目录结构
```
tests/route_unit_stress/
├── fixtures/mock_upstream.py      # 无依赖 OpenAI mock (MOCK_PORT/MOCK_NDJSON/MOCK_FORCE_MODE)
├── lib_reconcile.py               # 审计/上游四元组对账
├── lib_stats.py                   # Wilson CI、EWMA 质量分、健康度乘数、场景判定
├── lib_resources.py               # 10s 窗口资源采样
├── lib_report.py                  # CSV/JSON/MD 产物写入
├── run_smoke.py                   # S11 数据链冒烟
├── run_scenario.py                # S1–S6 场景运行器
├── *_test.py                      # 单元测试
└── README.md
```

## 与 #392 `tests/perf` 隔离
| 维度 | tests/perf | route_unit_stress |
|------|------------|-------------------|
| 关注点 | k6 全链路压测、状态机 | 单路由 EWMA 收敛、数据链完整性 |
| 运行器 | k6 JS | Python stdlib 无依赖 |
| 产物 | k6 summary、channel_model_health.csv | shares.csv、windows.csv、resources.csv、summary.json、report.md |

## 本地运行

### S1/S2/S3 (两路由同权重，收敛到 50/50 或延迟敏感目标)
```bash
# 1. 启动 mock (单实例)
MOCK_PORT=8099 MOCK_NDJSON=/tmp/upstream.ndjson python3 fixtures/mock_upstream.py

# 2. 启动网关 + 建拓扑 (subject route: ewma-stress-subject)
#    channels 90001/90002, weights 100/100, models: mock-ok

# 3. S11 冒烟
python3 run_smoke.py --gateway-url URL --token sk-... --admin-token JWT --mock-url http://127.0.0.1:8099 --requests 100 --concurrency 8 --out-dir runtime/smoke

# 4. S1/S2/S3 (各 13000 请求，预热 44)
python3 run_scenario.py --scenario S1 --gateway-url URL --token sk-... --admin-token JWT --alias ewma-stress-subject --mock-url http://127.0.0.1:8099 --requests 13000 --warmup 44 --concurrency 64 --stream-ratio 0.2 --out-dir runtime/S1
python3 run_scenario.py --scenario S2 ... --stream-ratio 1.0 --out-dir runtime/S2
python3 run_scenario.py --scenario S3 ... --stream-ratio 1.0 --out-dir runtime/S3
```

### S4 (限流场景：B 返回 429，验证 Retry-After 是否计入 TTFT)
```bash
# 1. 启动 2 个 mock 实例 (端口 18200/18201)，共享同一 NDJSON
MOCK_NDJSON=/tmp/upstream.ndjson MOCK_FORCE_MODE=ok MOCK_PORT=18200 python3 fixtures/mock_upstream.py &
MOCK_NDJSON=/tmp/upstream.ndjson MOCK_FORCE_MODE=ratelimit_missing MOCK_PORT=18201 python3 fixtures/mock_upstream.py &  # S4_NORETRY
# 或
MOCK_NDJSON=/tmp/upstream.ndjson MOCK_FORCE_MODE=ratelimit_5s MOCK_PORT=18201 python3 fixtures/mock_upstream.py &      # S4_RETRY5
# 或
MOCK_NDJSON=/tmp/upstream.ndjson MOCK_FORCE_MODE=ratelimit_10s MOCK_PORT=18201 python3 fixtures/mock_upstream.py &    # S4_RETRY10

# 2. 网关拓扑: 两条 route 同 alias，分别指向 port 18200 (A/ok) 和 18201 (B/ratelimit_*)
#    channel A -> 127.0.0.1:18200, channel B -> 127.0.0.1:18201, 同 static_weight=100

# 3. 等待 /healthz 就绪
for p in 18200 18201; do for i in {1..30}; do curl -fsS http://127.0.0.1:$p/healthz && break; sleep 1; done; done

# 4. 运行 (5400 请求，预热 44)
python3 run_scenario.py --scenario S4_NORETRY --gateway-url URL --token sk-... --admin-token JWT --alias ewma-stress-subject --mock-url http://127.0.0.1:18200 --requests 5400 --warmup 44 --concurrency 64 --stream-ratio 1.0 --out-dir runtime/S4_NORETRY
# S4_RETRY5 / S4_RETRY10 同理

# 关键判定: B 路由 observed_share=38% ±3pp (CI⊂[35%,41%])
# - 若 observed ≈ 41.2% (throttle_only_share)，说明 Retry-After 未计入 TTFT → PRODUCT_FAIL
```

### S5 (池化路由：同 alias 下 2/4/8 条渠道，一个 BAD 质量 0.5)
```bash
# 1. 启动 N 个 mock 实例 (端口 18200..18207)
# S5_POOL2:  2 个实例 (q05, ok)
# S5_POOL4:  4 个实例 (q05, ok, ok, ok)
# S5_POOL8:  8 个实例 (q05, ok×7)
MOCK_NDJSON=/tmp/upstream.ndjson
MOCK_FORCE_MODE=q05 MOCK_PORT=18200 python3 fixtures/mock_upstream.py &
MOCK_FORCE_MODE=ok MOCK_PORT=18201 python3 fixtures/mock_upstream.py &
# ... 根据 POOL 大小继续添加

# 2. 网关拓扑: N 条 route 同 alias (ewma-stress-subject)，分别指向 18200..1820N-1
#    所有 static_weight=100，同 model

# 3. 等待所有 /healthz
for p in 18200 18201 18202 18203; do for i in {1..30}; do curl -fsS http://127.0.0.1:$p/healthz && break; sleep 1; done; done

# 4. 运行 (最小样本: POOL2=5100, POOL4=2800, POOL8=3300)
python3 run_scenario.py --scenario S5_POOL2 --gateway-url URL --token sk-... --admin-token JWT --alias ewma-stress-subject --mock-url http://127.0.0.1:18200 --requests 5100 --warmup 44 --concurrency 64 --stream-ratio 1.0 --out-dir runtime/S5_POOL2
# S5_POOL4 (requests 2800), S5_POOL8 (requests 3300) 同理

# 关键判定: BAD 路由份额 = 0.5/(0.5+N-1) ±3pp(或±2pp for N=8)
# - POOL2: 33.33% [30.3%,36.3%]  |  POOL4: 14.29% [11.3%,17.3%]  |  POOL8: 6.67% [4.67%,8.67%]
```

### S6 (窗口化 EWMA 稳定性：4 路由，阶跃响应验证)
```bash
# 1. 启动 4 个 mock 实例 (端口 18200..18203)
MOCK_NDJSON=/tmp/upstream.ndjson
MOCK_FORCE_MODE=ok MOCK_PORT=18200 python3 fixtures/mock_upstream.py &      # STABLE-1
MOCK_FORCE_MODE=ok MOCK_PORT=18201 python3 fixtures/mock_upstream.py &      # STABLE-2
MOCK_FORCE_MODE=ttft_4000 MOCK_PORT=18202 python3 fixtures/mock_upstream.py &  # SLOW (2×TTFT)
MOCK_FORCE_MODE=ttft_4000 MOCK_PORT=18203 python3 fixtures/mock_upstream.py &  # STEP (4000→500 at 50%)

# 2. 网关拓扑: 4 条 route 同 alias，分别指向 18200..18203，static_weight=100
#    需在网关运行时 PUT /api/option/ 设置 RouteStatsShareWindowSize=50/200/1000

# 3. 等待 /healthz
for p in 18200 18201 18202 18203; do for i in {1..30}; do curl -fsS http://127.0.0.1:$p/healthz && break; sleep 1; done; done

# 4. 运行 (最小 90s 尾部观测，窗口样本≥100)
python3 run_scenario.py --scenario S6_W50 --gateway-url URL --token sk-... --admin-token JWT --alias ewma-stress-subject --mock-url http://127.0.0.1:18200 --requests 13000 --warmup 44 --concurrency 64 --stream-ratio 1.0 --out-dir runtime/S6_W50
# S6_W200 / S6_W1000 同理 (网关侧改 RouteStatsShareWindowSize)

# 关键判定: 无点估计 CI；评估 process_stability
# - corr_p99 headroom >20%  |  share stddev ≤6pp(W50)/≤3pp(W200/W1000)
# - 连续越界 ≤2 窗口  |  每窗口样本 ≥100  |  阶跃后 EWMA ±1σ 跟随
```

## 环境前提
| 变量/设置 | 必须值 | 说明 |
|-----------|--------|------|
| MEMORY_CACHE_ENABLED | true | 网关启用内存缓存 |
| channel_affinity_setting.enabled | **false** | true 则 runner 退出码 2 (ENVIRONMENT_INVALID) |
| header_override | 透传 X-Request-Id | 审计/对账依赖请求 ID |
| GOTOOLCHAIN | go1.26.4 | 编译工具链固定 |
| MOCK_PORT/MOCK_NDJSON/MOCK_FORCE_MODE | 见上 | mock fixture 环境变量 (S4/S5/S6 需多实例不同端口) |
| RouteStatsShareWindowSize | 50/200/1000 (S6) | 运行时 PUT /api/option/ 设置，S6 场景必需 |
| channel_affinity_setting.enabled | **false** (S5/S6) | 池化/稳定性场景对 sticky 更敏感 |
## 产物清单 (每场景独立目录)
requests.ndjson、warmup.ndjson、gateway-attempts.ndjson、upstream-received.ndjson、route-before.json、route-after.json、shares.csv、windows.csv、resources.csv、summary.json、report.md

## 判定优先级与退出码
1. ENVIRONMENT_INVALID (2) — affinity=true/网关不可达/mock未就绪/拓扑路由数不符
2. DATA_INVALID (1) — 四元组对账失败 / S6 窗口样本 <100
3. UNDERPOWERED (1) — matched_pairs < 场景 min_samples (S1/S2/S3: 12800, S4: 5400, S5_POOL2: 5100, S5_POOL4: 2800, S5_POOL8: 3300)
4. PRODUCT_FAIL (1) — subject route CI 未完全含 target±tol_pp
5. PASS (0) — 全通过
> artifact 优先：非 0 退出码前必须上传 artifact

## S4: 38% vs 41.2% 对照含义
S4 测试**限流时 Retry-After 头是否被正确计入 TTFT**。

| 场景 | B 路由行为 | 理论目标 | throttle_only_share |
|------|-----------|---------|---------------------|
| S4_NORETRY | 429 无 Retry-After | 38% ±3pp | 41.18% |
| S4_RETRY5  | 429 + Retry-After: 5 | 38% ±3pp | 41.18% |
| S4_RETRY10 | 429 + Retry-After: 10 | 38% ±3pp | 41.18% |

**判定逻辑**：
- `throttle_only_share = q/(1+q) = 0.7/1.7 ≈ 0.4118` — 这是**仅用 ThrottledObservation(0.7)** 时的理论份额，完全忽略了 Retry-After 的等待时间。
- 如果观测到的 B 路由份额 ≈ 38%（目标），说明 **Retry-After 等待时间被正确计入了 TTFT**，EWMA 感知到了完整的延迟惩罚 → **PASS**。
- 如果观测到的 B 路由份额 ≈ 41.2%（throttle_only_share），说明 **Retry-After 未计入 TTFT**，EWMA 只看到了 429 即时失败的 ThrottledObservation → **PRODUCT_FAIL**。
- 该对照是 S4 场景的核心产品判据，直接验证 #418 的限流语义实现。

## S5: 池化路由份额公式
同一 alias 下 `N` 条路由，`N-1` 条 quality=1.0 (ok)，1 条 quality=0.5 (q05)。
EWMA 质量分收敛后，BAD 路由预期份额：
```
target = bad_quality / (bad_quality + (N - 1) * 1.0) = 0.5 / (N - 0.5)
```
| 场景 | N | BAD 目标份额 | 容差 | CI 区间 | min_samples |
|------|---|-------------|------|---------|-------------|
| S5_POOL2 | 2 | 33.33% | ±3pp | [30.3%, 36.3%] | 5100 |
| S5_POOL4 | 4 | 14.29% | ±3pp | [11.3%, 17.3%] | 2800 |
| S5_POOL8 | 8 | 6.67%  | ±2pp | [4.67%, 8.67%] | 3300 |

**关键点**：
- 所有路由同 `static_weight=100`，仅 quality 不同 → 验证 EWMA 质量分在池化路由下的加权效果。
- `q05` 模式 = crc32 确定性 50% 失败率，等价 quality=0.5。
- 汇报会展示每条 healthy 路由的份额与总和（应接近 1 - BAD_target）。

## S6: 窗口化 EWMA 过程稳定性
4 路由同 alias：2×STABLE(ok)、1×SLOW(ttft_4000)、1×STEP(ttft_4000→ttft_500 at 50% 进度)。
**无点估计 CI 判定**，仅评估过程稳定性指标：

| 指标 | 阈值 | 说明 |
|------|------|------|
| corr_p99 headroom | > 20% | p99 与 corr_p99 的相对余量 |
| share stddev (pp) | ≤6pp (W50), ≤3pp (W200/W1000) | 路由份额窗口间标准差 |
| max consecutive breach | ≤2 | 连续窗口越界计数 |
| min window samples | ≥100 | 每窗口最小样本数，不足 = DATA_INVALID |
| corr_p99 max | ≤2.0 | 绝对上限 |

**阶跃响应**：STEP 路由在 50% 进度从 4000ms 降为 500ms，EWMA 应在 ±1σ 带内跟随。
**窗口大小**：通过 `RouteStatsShareWindowSize` 控制 (50/200/1000)，窗口越大稳定性要求越严 (stddev ≤3pp)。
**最小尾部**：最后 90 秒必须有数据覆盖阶跃后的稳态。

## 样本量推导 (n≈12,800 → 13,000)
目标：observed_share Wilson 95% CI 完全落在 [target-0.02, target+0.02]。
最大方差 p=0.5 → 半宽 ≤0.02 解得 n≥12,800，保守取 13,000。
S1 流式 20%，S2/S3 全流式；预热 44 请求剔除。

### S4 样本量 (n≥5,400)
目标：±3pp CI，target=0.38 → 方差 p(1-p) ≈ 0.2356，比 0.5 小 → n 可降低。
Wilson 95% CI 半宽 ≤0.03 解得 n≥5,400。

### S5 样本量
| 场景 | target | 方差 | 容差 | n≥ |
|------|--------|------|------|----|
| POOL2 | 0.333 | 0.222 | ±3pp | 5100 |
| POOL4 | 0.143 | 0.123 | ±3pp | 2800 |
| POOL8 | 0.067 | 0.062 | ±2pp | 3300 |

### S6 无固定样本量要求
由窗口大小、采样率、min_tail_seconds=90s 隐含决定。要求每窗口 ≥100 样本。