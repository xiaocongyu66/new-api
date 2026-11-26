# 导出的 JSON 长什么样

`scripts/export_upstream_model_json.py` 产出的文件，schema 固定为
`upstream-model-catalog/v1`，可以直接喂给比价/告警/入库脚本。

```json
{
  "schema": "upstream-model-catalog/v1",
  "source": "https://upstream.example",
  "generated_at": "2026-11-24T03:00:00+08:00",
  "window_hours": 24,
  "model_count": 48,
  "models": [
    {
      "model": "gemini-4-hydrogen",
      "quota_type": 1,
      "model_ratio": 0,
      "completion_ratio": 0,
      "model_price": 0.04,
      "enable_groups": ["default", "vip"],
      "success_rate": 82.14,
      "avg_latency_ms": 72780,
      "p90_latency_ms": 101820,
      "observed_hours": 24
    }
  ]
}
```

## 字段含义

| 字段 | 说明 |
|---|---|
| `model` | 上游的模型名（未做任何映射，原样保留） |
| `quota_type` | `0` 按量计费、`1` 按次计费 |
| `model_ratio` | 按量计费的倍率（**未乘分组倍率**） |
| `completion_ratio` | 输出相对输入的倍率 |
| `model_price` | 按次计费的单次价格 |
| `enable_groups` | 该模型在哪些分组可用 |
| `success_rate` | 窗口内平均成功率（%） |
| `avg_latency_ms` | 平均延迟（毫秒） |
| `p90_latency_ms` | p90 延迟（毫秒），长尾指标 |
| `observed_hours` | 参与统计的小时桶数量；`0` 表示没有性能数据 |

## 三个必须知道的口径

1. **`model_ratio` 不是最终价格。** new-api 的实际售价 = `model_ratio` ×
   分组倍率（`GroupRatio`）。跨站比价时必须把分组倍率算进去，否则会
   低估成本。
2. **`success_rate` 为 `null` 不等于 0。** 它表示这个模型在窗口内没有
   性能样本（可能没人调用，也可能上游没开性能接口）。把它当 0 会导致
   误报故障。
3. **按次计费的模型 `model_ratio` 恒为 0。** 判断该用哪个字段看
   `quota_type`，不要看哪个字段非零。

## 导入到本站前的自查

```bash
python3 - <<'PY'
import json
d = json.load(open('out/upstream-models.json'))
priced = [m for m in d['models'] if m['model_price'] or m['model_ratio']]
print('有价格的模型:', len(priced), '/', d['model_count'])
print('有成功率的模型:', sum(1 for m in d['models'] if m['success_rate'] is not None))
assert d['schema'] == 'upstream-model-catalog/v1'
PY
```

价格全为 0 通常意味着上游把定价放在分组倍率里、或者该账号看不到价格，
这时不要导入——先把分组倍率取到。
