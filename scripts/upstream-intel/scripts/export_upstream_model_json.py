#!/usr/bin/env python3
"""把一个 new-api 上游的「模型价格 + 成功率/延迟」导出成可导入的 JSON。

数据来源（都来自 new-api 自身的公开/半公开接口）：

    GET /api/pricing                       模型清单 + 倍率/按次价
    GET /api/perf-metrics/summary?hours=24 每个模型的成功率
    GET /api/perf-metrics?model=X&hours=24 该模型的逐小时序列

合并键是 model_name。价格字段原样保留（model_ratio / model_price /
completion_ratio / quota_type），另外算出「每百万 token 美元价」方便比价：

    quota_type=0（按量）:  input = model_ratio × 2 / 1e6  …… 以 new-api 的
                           quota 口径换算，具体见下方说明
    quota_type=1（按次）:  直接用 model_price

成功率口径：只统计有明确结论的样本，缺失不算成功也不算失败。

用法：

    python3 scripts/export_upstream_model_json.py --base https://upstream.example \
        --state ~/.config/newapi-upstream-intel/state/upstream.example.json \
        --out out/upstream-models.json

没有登录态时，也可以只抓公开部分（价格），成功率会留空。
"""

from __future__ import annotations

import argparse
import json
import statistics
import sys
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from lib import Redactor, http_get_json, percentile, token_from_state  # noqa: E402


def fetch_all(base: str, token: str | None, state_file: str | None, redact: Redactor) -> tuple[dict, dict, dict]:
    """返回 (pricing, perf_summary, perf_series_by_model)。"""
    bearer = token
    if state_file and not bearer:
        bearer = token_from_state(state_file, base, redact)

    pricing = http_get_json(base.rstrip("/") + "/api/pricing", bearer, timeout=30, redact=redact)

    summary: dict = {}
    series: dict[str, list] = {}
    try:
        summary = http_get_json(
            base.rstrip("/") + "/api/perf-metrics/summary?hours=24", bearer, timeout=30, redact=redact
        )
    except Exception as exc:  # noqa: BLE001 - 没登录时这段允许缺失
        print(redact(f"成功率接口不可用（可能未登录）：{str(exc)[:120]}"))
        return pricing, {}, {}

    models = ((summary.get("data") or {}).get("models")) or []
    for entry in models:
        name = (entry.get("model_name") or "").strip()
        if not name:
            continue
        try:
            from urllib.parse import quote

            doc = http_get_json(
                base.rstrip("/") + f"/api/perf-metrics?model={quote(name, safe='')}&hours=24",
                bearer,
                timeout=30,
                redact=redact,
            )
            series[name] = ((doc.get("data") or {}).get("series")) or []
        except Exception:  # noqa: BLE001 - 单个模型失败不影响整体
            series[name] = []
    return pricing, summary, series


def hourly_stats(series: list[dict]) -> tuple[float | None, int | None, int | None, int]:
    """把逐小时序列压成 (平均成功率, 平均延迟, p90 延迟, 样本桶数)。"""
    rates = [p.get("success_rate") for p in series if p.get("success_rate") is not None]
    latencies = sorted(
        float(p["avg_latency_ms"])
        for p in series
        if isinstance(p.get("avg_latency_ms"), (int, float)) and p["avg_latency_ms"] > 0
    )
    if not rates:
        return None, None, None, 0
    avg_rate = round(sum(rates) / len(rates), 2)
    avg_lat = int(sum(latencies) / len(latencies)) if latencies else None
    p90 = int(percentile(latencies, 0.9)) if latencies else None
    return avg_rate, avg_lat, p90, len(rates)


def build_rows(pricing: dict, summary: dict, series: dict[str, list]) -> list[dict]:
    rows = ((pricing.get("data") or [])) if isinstance(pricing.get("data"), list) else []
    if not rows:
        return []

    summary_by_model = {
        (m.get("model_name") or "").strip(): m
        for m in ((summary.get("data") or {}).get("models")) or []
    }

    out = []
    for row in rows:
        name = (row.get("model_name") or "").strip()
        if not name:
            continue
        quota_type = row.get("quota_type", 0)
        model_ratio = row.get("model_ratio")
        model_price = row.get("model_price")
        rate, lat, p90, buckets = hourly_stats(series.get(name, []))

        item = {
            "model": name,
            "quota_type": quota_type,
            "model_ratio": model_ratio,
            "completion_ratio": row.get("completion_ratio"),
            "model_price": model_price,
            "enable_groups": row.get("enable_groups"),
            "success_rate": rate,
            "avg_latency_ms": lat,
            "p90_latency_ms": p90,
            "observed_hours": buckets,
        }
        if name in summary_by_model and rate is None:
            item["success_rate"] = summary_by_model[name].get("success_rate")
        out.append(item)
    return out


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", required=True)
    parser.add_argument("--token", help="new-api 访问令牌（与 --state 二选一）")
    parser.add_argument("--state", help="Playwright storage_state.json（登录后导出）")
    parser.add_argument("--out", default="out/upstream-models.json")
    args = parser.parse_args()

    redact = Redactor({})
    pricing, summary, series = fetch_all(args.base, args.token, args.state, redact)
    rows = build_rows(pricing, summary, series)
    if not rows:
        sys.exit("没有取到任何模型：检查登录态是否有效、或该站是否开放 /api/pricing")

    payload = {
        "schema": "upstream-model-catalog/v1",
        "source": args.base.rstrip("/"),
        "generated_at": datetime.now(timezone.utc).astimezone().isoformat(timespec="seconds"),
        "window_hours": 24,
        "model_count": len(rows),
        "models": rows,
    }
    out = Path(args.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")

    with_rate = sum(1 for r in rows if r["success_rate"] is not None)
    print(f"导出 {len(rows)} 个模型（其中 {with_rate} 个带成功率）-> {out}")


if __name__ == "__main__":
    main()
