#!/usr/bin/env python3
"""把本次抓取与上一份快照对比，产出 Markdown 报告。

只出报告，不改价。价格改动风险高，交给人看。
"""

from __future__ import annotations

import argparse
import json
from datetime import datetime
from pathlib import Path

SIGNIFICANT = 20.0  # 变动超过 20% 要单独标出来


def index_rows(payload: list[dict]) -> dict[tuple, float]:
    out: dict[tuple, float] = {}
    for channel in payload:
        for row in channel.get("rows") or []:
            for field, raw in (row.get("values") or {}).items():
                try:
                    value = float(str(raw).lstrip("$").replace(",", "").rstrip("%"))
                except ValueError:
                    continue
                out[(channel["channel_id"], row["key"], field)] = value
    return out


def format_number(value: float) -> str:
    text = f"{value:.12f}".rstrip("0").rstrip(".")
    return text or "0"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--prices", default="out/prices.json")
    parser.add_argument("--baseline", default="out/prices.prev.json")
    parser.add_argument("--out", default="out/report.md")
    args = parser.parse_args()

    current = json.loads(Path(args.prices).read_text(encoding="utf-8"))
    baseline_path = Path(args.baseline)
    baseline = json.loads(baseline_path.read_text(encoding="utf-8")) if baseline_path.exists() else None

    ok = [c for c in current if c.get("rows")]
    failed = [c for c in current if not c.get("rows")]

    lines: list[str] = []
    stamp = datetime.now().strftime("%Y-%m-%d %H:%M")
    lines.append(f"# 上游价格变动报告 {stamp}")
    lines.append("")

    if baseline is None:
        lines.append("## 摘要")
        lines.append(f"- 首次运行，仅生成当前快照：{len(ok)}/{len(current)} 个渠道取到价格")
        lines.append("")
        lines.append("## 当前价格")
        lines.append("| 渠道 | 模型 | 字段 | 值 |")
        lines.append("|---|---|---|---|")
        for channel in ok:
            for row in channel["rows"]:
                for field, value in row["values"].items():
                    lines.append(f"| {channel['channel_name']} | {row['key']} | {field} | {value} |")
    else:
        before = index_rows(baseline)
        after = index_rows(current)
        changes = []
        for key, new_value in after.items():
            old_value = before.get(key)
            if old_value is None or old_value == new_value:
                continue
            delta = (new_value - old_value) / old_value * 100 if old_value else float("inf")
            changes.append((key, old_value, new_value, delta))
        changes.sort(key=lambda item: abs(item[3]), reverse=True)

        up = sum(1 for c in changes if c[3] > 0)
        down = sum(1 for c in changes if c[3] < 0)
        lines.append("## 摘要")
        lines.append(f"- 扫描渠道 {len(current)} 个，成功 {len(ok)} 个，失败 {len(failed)} 个")
        lines.append(f"- 价格变动 {len(changes)} 项（涨 {up} / 降 {down}）")
        lines.append("")
        lines.append("## 变动明细")
        if changes:
            lines.append("| 渠道 | 模型 | 字段 | 旧值 | 新值 | 幅度 |")
            lines.append("|---|---|---|---|---|---|")
            names = {c["channel_id"]: c["channel_name"] for c in current}
            for (channel_id, model, field), old_value, new_value, delta in changes:
                mark = " **[重点]**" if abs(delta) >= SIGNIFICANT else ""
                lines.append(
                    f"| {names.get(channel_id, channel_id)} | {model} | {field} | "
                    f"{format_number(old_value)} | {format_number(new_value)} | {delta:+.1f}%{mark} |"
                )
        else:
            lines.append("无变动。")

    lines.append("")
    lines.append("## 抓取失败")
    if failed:
        lines.append("| 渠道 | 上游 | 原因 | 建议 |")
        lines.append("|---|---|---|---|")
        for channel in failed:
            reason = (channel.get("error") or "").replace("|", "/")[:160]
            advice = "写 specs/<host>.json，先试 scrape 再试 browser" if channel.get("needs_spec") else "检查凭据或上游状态"
            lines.append(f"| {channel['channel_name']} | {channel['host']} | {reason} | {advice} |")
    else:
        lines.append("全部渠道取数成功。")

    Path(args.out).parent.mkdir(parents=True, exist_ok=True)
    Path(args.out).write_text("\n".join(lines) + "\n", encoding="utf-8")
    print(f"报告已写入 {args.out}")


if __name__ == "__main__":
    main()
