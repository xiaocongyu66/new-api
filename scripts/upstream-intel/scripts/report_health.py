#!/usr/bin/env python3
"""把健康数据转成 Markdown 报告：概览 + 异常 + 无数据。

默认阈值只是起点，跑几次后按实际分布调整。
"""

from __future__ import annotations

import argparse
import json
from collections import defaultdict
from datetime import datetime
from pathlib import Path

import sys

sys.path.insert(0, str(Path(__file__).parent))
from lib import percentile  # noqa: E402

RATE_FLOOR = 95.0      # 成功率低于此值算异常
P90_CEILING_MS = 30_000  # p90 高于此值算异常


def summarize(rows: list[dict]) -> dict:
    """按样本数加权，而不是对各小时的比率做算术平均。

    一个只有 1 个样本的小时和一个有 200 个样本的小时，权重不该相同——
    算术平均会让深夜的零星样本把白天的真实水平冲淡。
    """
    weighted_sum = 0.0
    weight_total = 0.0
    latencies = sorted(r["latency_ms"] for r in rows if r.get("latency_ms"))
    p90_values = sorted(r["latency_p90_ms"] for r in rows if r.get("latency_p90_ms"))
    for row in rows:
        rate = row.get("success_rate")
        if rate is None:
            continue
        weight = row.get("samples") or 1
        weighted_sum += rate * weight
        weight_total += weight
    return {
        "points": len(rows),
        "rate": round(weighted_sum / weight_total, 2) if weight_total else None,
        "p50": latencies[len(latencies) // 2] if latencies else None,
        "p90": (int(percentile(p90_values, 0.9)) if p90_values else (int(percentile(latencies, 0.9)) if latencies else None)),
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--health", default="out/health.json")
    parser.add_argument("--out", default="out/report.md")
    args = parser.parse_args()

    payload = json.loads(Path(args.health).read_text(encoding="utf-8"))
    healthy = [c for c in payload if c.get("rows")]
    missing = [c for c in payload if not c.get("rows")]

    lines: list[str] = []
    lines.append(f"# 上游健康度 {datetime.now().strftime('%Y-%m-%d %H:%M')}")
    lines.append("")

    lines.append("## 概览")
    lines.append("| 渠道 | 数据点 | 成功率 | 延迟 p50 | 延迟 p90 |")
    lines.append("|---|---|---|---|---|")
    for channel in healthy:
        stat = summarize(channel["rows"])
        lines.append(
            f"| {channel['channel_name']} | {stat['points']} | "
            f"{stat['rate'] if stat['rate'] is not None else '-'}% | "
            f"{stat['p50'] or '-'} ms | {stat['p90'] or '-'} ms |"
        )

    lines.append("")
    lines.append("## 异常")
    anomalies = []
    for channel in healthy:
        for row in channel["rows"]:
            rate = row.get("success_rate")
            p90 = row.get("latency_p90_ms") or row.get("latency_ms")
            if (rate is not None and rate < RATE_FLOOR) or (p90 and p90 > P90_CEILING_MS):
                anomalies.append((channel, row))
    if anomalies:
        lines.append("| 渠道 | 模型 | 成功率 | 延迟 | 小时桶 |")
        lines.append("|---|---|---|---|---|")
        for channel, row in anomalies[:50]:
            bucket = datetime.fromtimestamp(row["bucket_ts"]).strftime("%m-%d %H:%M")
            lines.append(
                f"| {channel['channel_name']} | {row['model']} | {row.get('success_rate')}% | "
                f"{row.get('latency_p90_ms') or row.get('latency_ms') or '-'} ms | {bucket} |"
            )
    else:
        lines.append(f"无（阈值：成功率 < {RATE_FLOOR}% 或 p90 > {P90_CEILING_MS} ms）")

    lines.append("")
    lines.append("## 无数据渠道")
    if missing:
        lines.append("| 渠道 | 上游 | 原因 | 建议 |")
        lines.append("|---|---|---|---|")
        for channel in missing:
            reason = (channel.get("error") or "").replace("|", "/")[:160]
            advice = "写 specs/<host>.json 或补 jk_group_id" if channel.get("needs_spec") else "检查凭据"
            lines.append(f"| {channel['channel_name']} | {channel['host']} | {reason} | {advice} |")
    else:
        lines.append("全部渠道有数据。")

    Path(args.out).parent.mkdir(parents=True, exist_ok=True)
    Path(args.out).write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"报告已写入 {args.out}")


if __name__ == "__main__":
    main()
