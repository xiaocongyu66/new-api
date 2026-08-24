# #392 状态机场景资产

本目录补齐 #392 尚未闭环的场景 3、5、6、7、8。资产已写入分支，**本次未运行云端压测**；请在新会话审阅拓扑后，从 GitHub Actions 手动触发。

## 场景映射

| #392 | k6 脚本 | 模型 | 断言证据 |
|---|---|---|---|
| 3 坏 Key 连坐 | `bad-key-cascade.js` | `mock-bad` | 指定坏 Key RouteKey 为 disabled；同 channel 的兄弟 key 不 disabled；`key verification failed` 日志 |
| 5 池压防御 | `pool-pressure.js` | `mock-flaky` | `emergency recover` 或 pool-pressure 日志；健康表有记录 |
| 6 灰度故障 | `gray-failure.js` | `mock-flaky` | healthy 与 calm/dormant 共存；后续 `route isolation ... healthy` 恢复日志；拒绝 disabled-only 稳态 |
| 7 权重拟合 | `weight-distribution.js` | 预置模型 | worker `record consume log` 的 `admin_info.use_channel`，1000+ 样本，62.5/31.25/6.25% ±10pp |
| 8 超时分类 | `timeout-classification.js` | `mock-slow` | `channel_model_health.local_failure_count` 与 `upstream_failure_count` 均独立大于 0 |

## 拓扑前置条件

工作流会部署 `tests/perf/fixtures/mock-upstream.py` 为 `stress-mock` 服务，但**不会擅自改测试集群的渠道、Key、权重或状态机配置**。触发前必须准备：

1. 目标模型渠道的 `base_url` 指向 `http://stress-mock`，并包含对应 `mock-*` model。
2. 场景 3：一个多 Key 渠道。坏 Key index 对应 `bad-key`，兄弟 index 对应 `good-key`；同一个 channel 至少映射两个模型，触发后用 workflow 的 `bad_key_channel`、`bad_key_index` 指定该 RouteKey。
3. 场景 5/6：至少 3 个 `mock-flaky` 调度单元，`RetryTimes=5`，无限额度；默认 30% 500。 
4. 场景 7：三个目标单位已预置权重/健康状态，使最终有效权重为 100:50:10；压测期间不能混入其它同模型渠道。
5. 场景 8：既要有触发 `mock-slow` 的本地超时路径，也要同时注入一个上游 5xx 路径；断言读取的是实际两类 failure counter。

## 手动触发

GitHub Actions 选择 **State Machine Scenario Test**。该 workflow 只有 `workflow_dispatch`，不会被 push、PR、merge 或 schedule 自动执行。

常用输入：

```text
scenario: gray-failure
target_url: http://<ingress-or-nodeport>/v1
api_key: <test token>
model: mock-flaky
vus: 50
duration: 120s
k8s_image_tag: <optional test image>
rollback_after_test: true
```

结果 artifact 包含 `k6-summary.json`、worker 全量日志、`channel_model_health.csv`、`distribution.json` 与 `assertion-report.md`。

## 本地语法检查

```bash
python3 -m py_compile tests/perf/fixtures/mock-upstream.py tests/perf/assertions/state_machine.py
node --check tests/perf/scenarios/bad-key-cascade.js
python3 -c 'import yaml; d=yaml.safe_load(open(".github/workflows/state-machine-test.yml")); assert list((d.get("on") or d[True]).keys()) == ["workflow_dispatch"]'
```

`state_machine.py` 也支持只从已有日志导出分布：

```bash
python3 tests/perf/assertions/state_machine.py \
  --derive-distribution --worker-log worker.log --distribution-json distribution.json
```
