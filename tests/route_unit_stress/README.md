# Route-Unit EWMA Stress Tests (S1–S3)

## 目录结构
```
tests/route_unit_stress/
├── fixtures/mock_upstream.py      # 无依赖 OpenAI mock (MOCK_PORT/MOCK_NDJSON)
├── lib_reconcile.py               # 审计/上游四元组对账
├── lib_stats.py                   # Wilson CI、EWMA 质量分、健康度乘数
├── lib_resources.py               # 10s 窗口资源采样
├── lib_report.py                  # CSV/JSON/MD 产物写入
├── run_smoke.py                   # S11 数据链冒烟
├── run_scenario.py                # S1/S2/S3 场景运行器
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
```bash
# 1. 启动 mock
MOCK_PORT=8099 MOCK_NDJSON=/tmp/upstream.ndjson python3 fixtures/mock_upstream.py

# 2. 启动网关 + 建拓扑 (subject route: ewma-stress-subject)
#    channels 90001/90002/90003, weights 100/50/10, models: mock-ok

# 3. S11 冒烟
python3 run_smoke.py --gateway-url URL --token sk-... --admin-token JWT --mock-url http://127.0.0.1:8099 --requests 100 --concurrency 8 --out-dir runtime/smoke

# 4. S1/S2/S3 (各 13000 请求，预热 44)
python3 run_scenario.py --scenario S1 --gateway-url URL --token sk-... --admin-token JWT --alias ewma-stress-subject --mock-url http://127.0.0.1:8099 --requests 13000 --warmup 44 --concurrency 64 --stream-ratio 0.2 --out-dir runtime/S1
python3 run_scenario.py --scenario S2 ... --stream-ratio 1.0 --out-dir runtime/S2
python3 run_scenario.py --scenario S3 ... --stream-ratio 1.0 --out-dir runtime/S3
```

## 环境前提
| 变量/设置 | 必须值 | 说明 |
|-----------|--------|------|
| MEMORY_CACHE_ENABLED | true | 网关启用内存缓存 |
| channel_affinity_setting.enabled | **false** | true 则 runner 退出码 2 (ENVIRONMENT_INVALID) |
| header_override | 透传 X-Request-Id | 审计/对账依赖请求 ID |
| GOTOOLCHAIN | go1.26.4 | 编译工具链固定 |
| MOCK_PORT/MOCK_NDJSON | 见上 | mock fixture 环境变量 |

## 产物清单 (每场景独立目录)
requests.ndjson、warmup.ndjson、gateway-attempts.ndjson、upstream-received.ndjson、route-before.json、route-after.json、shares.csv、windows.csv、resources.csv、summary.json、report.md

## 判定优先级与退出码
1. ENVIRONMENT_INVALID (2) — affinity=true/网关不可达/mock未就绪
2. DATA_INVALID (1) — 四元组对账失败
3. UNDERPOWERED (1) — matched_pairs < 12800
4. PRODUCT_FAIL (1) — subject route CI 未完全含 target±2pp
5. PASS (0) — 全通过
> artifact 优先：非 0 退出码前必须上传 artifact

## 样本量推导 (n≈12,800 → 13,000)
目标：observed_share Wilson 95% CI 完全落在 [target-0.02, target+0.02]。
最大方差 p=0.5 → 半宽 ≤0.02 解得 n≥12,800，保守取 13,000。
S1 流式 20%，S2/S3 全流式；预热 44 请求剔除。