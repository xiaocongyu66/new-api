#!/usr/bin/env python3
"""定时任务入口：导出渠道 → 采集健康度 → 出报告。

systemd timer 每小时跑这个脚本，agent 不参与例行采集。
"""

from __future__ import annotations

import argparse
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
    run("collect_health.py", "--channels", str(out / "channels.json"), "--out", str(out / "health.json"))
    run("report_health.py", "--health", str(out / "health.json"), "--out", str(out / "report.md"))

    print("\n" + (out / "report.md").read_text(encoding="utf-8"))


if __name__ == "__main__":
    main()
