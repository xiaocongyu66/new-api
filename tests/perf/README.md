# 性能压测目录

本目录包含 k6 性能压测场景库与 CI 工作流，用于对 OpenAI 兼容接口进行吞吐与延迟基准测试。

## 目录结构

```
tests/perf/
├── lib/
│   └── openai.js          # 纯 JS 工具函数（无 k6 依赖，可 `node --check` 校验）
├── scenarios/
│   ├── chat-throughput.js # 非流式吞吐场景
│   └── stream-latency.js  # 流式首包延迟场景
└── README.md              # 本文件
```

## 两个场景的区别

| 维度 | chat-throughput.js | stream-latency.js |
|------|-------------------|-------------------|
| 请求模式 | 非流式（`stream: false`） | 流式（`stream: true`） |
| 执行器 | `ramping-vus` 阶梯爬坡 | `constant-vus` 恒定并发 |
| 关注指标 | 吞吐量、p95 延迟、错误率 | TTFT（首包时间近似）、错误率 |
| `max_tokens` | 16 | 64 |
| 默认 VUS | 100 | 50 |
| 默认时长 | 120s | 60s |

### TTFT / ITL 说明

- k6 的 `http_req_waiting` 指标等于 TTFB（Time To First Byte），在流式场景下作为 **TTFT 近似值** 使用。
- **ITL（inter-token latency）无法在 k6 中测量**：k6 会缓冲完整响应体，无法获取每个 SSE chunk 的到达时间戳。
- 若需精确的 TTFT 与 ITL 分布，请使用仓库根目录下的 `scripts/stress_test.py`，该脚本直接解析 SSE 流并记录 chunk 级时间戳。

## 本地运行

### 前置条件

- 安装 k6（建议 v0.50+）：`https://grafana.com/docs/k6/latest/set-up/install-k6/`
- 目标服务已启动并可访问（提供 OpenAI 兼容的 `/chat/completions`）

### 非流式吞吐测试

```bash
k6 run \
  -e TARGET_URL=http://localhost:3000 \
  -e API_KEY=sk-xxx \
  -e MODEL=mock-fast \
  -e VUS=100 \
  -e DURATION=120s \
  -e RAMP_DURATION=30s \
  -e P95_MS=30000 \
  tests/perf/scenarios/chat-throughput.js
```

### 流式首包延迟测试

```bash
k6 run \
  -e TARGET_URL=http://localhost:3000 \
  -e API_KEY=sk-xxx \
  -e MODEL=mock-fast \
  -e VUS=50 \
  -e DURATION=60s \
  tests/perf/scenarios/stream-latency.js
```

### 导出 JSON 汇总

```bash
k6 run \
  -e SUMMARY_JSON=perf-result.json \
  ...其他参数...
  tests/perf/scenarios/chat-throughput.js
```

输出的 `perf-result.json` 可供 CI 归档或后续分析。

## GitHub Actions 触发

本仓库提供 `.github/workflows/perf-test.yml`，仅支持 `workflow_dispatch` 手动触发。

### 触发方式

1. 进入 GitHub 仓库的 **Actions** 标签页
2. 选择 **Performance Test** 工作流
3. 点击 **Run workflow**
4. 填写以下输入：
   - `scenario`：`chat-throughput` 或 `stream-latency`
   - `target_url`：目标服务地址（必填）
   - `api_key`：Bearer Token（必填，会被自动 mask）
   - `model`：模型名，默认 `mock-fast`
   - `vus`：并发数，默认 100 / 50
   - `duration`：持续时间，默认 `120s` / `60s`
   - `ramp_duration`：爬坡时间（仅 chat-throughput），默认 `30s`
   - `p95_ms`：p95 延迟预算 ms（仅 chat-throughput），默认 `30000`
5. 点击 **Run workflow** 启动

### 产出

- **Artifact**：`perf-result.json`（完整 k6 汇总，`always()` 上传）
- **Job Summary**：关键指标自动追加到 GitHub Step Summary，包含：
  - `http_req_duration` p(50)/p(95)/p(99)
  - `http_req_failed` 率
  - `ttft` p(50)/p(95)（仅 stream-latency）
  - VU 数、迭代数、总请求数

## 判读标准

### 通用阈值（两个场景共享）

- **http_req_failed rate < 0.05**：错误率不超过 5%。超过说明上游不稳定或限流。
- **http_req_duration p(95) < P95_MS**（仅 chat-throughput）：默认 30000ms，针对 mock 上游的宽松预算。生产环境建议收紧至 5000ms 或更低。

### chat-throughput.js 专用

- **p(95) 延迟**：反映非流式请求的尾部延迟。若显著高于预期，排查上游模型推理队列、网络抖动、连接池饱和。
- **吞吐量（iterations/s）**：结合 VUS 与 p(95) 判断系统饱和点。

### stream-latency.js 专用

- **ttft p(50)/p(95)**：首包延迟中位数与尾部。p(95) > 5000ms 提示首包排队严重。
- **错误率**：流式连接更易受中间件超时影响，关注 `http_req_failed` 突增。

## 环境变量速查表

| 变量 | 说明 | chat-throughput | stream-latency |
|------|------|-----------------|----------------|
| TARGET_URL | 目标基础 URL | ✅ | ✅ |
| API_KEY | Bearer Token | ✅ | ✅ |
| MODEL | 模型名称 | ✅ | ✅ |
| VUS | 目标并发数 | ✅ | ✅ |
| DURATION | 稳态/总时长 | ✅ | ✅ |
| RAMP_DURATION | 爬坡时长 | ✅ | ❌ |
| P95_MS | p95 预算 ms | ✅ | ❌ |
| SUMMARY_JSON | 汇总输出路径 | ✅ | ✅ |

## 扩展建议

- 新增场景：在 `scenarios/` 下创建 `.js`，遵循现有约定（`__ENV` 参数化、`handleSummary` 导出）。
- 复用 `lib/openai.js` 构建请求头与载荷，保持一致性。
- CI 工作流新增 `scenario` 选项即可自动支持。