#!/usr/bin/env python3
"""定时任务入口：导出渠道 → 抓价格 → 出报告。

systemd timer 直接跑这个脚本，agent 不参与例行采集。
失败时以非零码退出，交给告警去叫人或叫 agent。
"""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
from pathlib import Path

HERE = Path(__file__).parent


def run(script: str, *args: str) -> None:
    proc = subprocess.run([sys.executable, str(HERE / script), *args])
    if proc.returncode != 0:
        raise SystemExit(f"{script} 失败（exit {proc.returncode}）")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", default="out")
    args = parser.parse_args()
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)

    run("export_channels.py", "--out", str(out / "channels.json"))

    # 上一份快照留作基线，本次结果成为下一轮的基线
    current = out / "prices.json"
    if current.exists():
        shutil.copy2(current, out / "prices.prev.json")

    run("collect_prices.py", "--channels", str(out / "channels.json"), "--out", str(current))
    run("diff_prices.py", "--prices", str(current), "--baseline", str(out / "prices.prev.json"), "--out", str(out / "report.md"))

    report = (out / "report.md").read_text(encoding="utf-8")
    print("\n" + report)


if __name__ == "__main__":
    main()
