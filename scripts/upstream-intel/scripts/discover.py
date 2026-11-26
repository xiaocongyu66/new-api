#!/usr/bin/env python3
"""探一个上游到底有什么可取数的地方。

给「待补」的上游用：它把候选接口逐个打一遍，只报状态码与返回体形状，
不猜选择器。拿到结果后再决定走 api / scrape / browser。

    python3 scripts/discover.py https://unknown.example
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from lib import Redactor, http_get_json  # noqa: E402

CANDIDATES = (
    "/api/pricing",
    "/api/v1/model-plaza",
    "/api/perf-metrics/summary?hours=24",
    "/api/ratio_config",
    "/v1/models",
    "/jk/status.json",
)


def shape(payload) -> str:
    if isinstance(payload, list):
        return f"list[{len(payload)}]"
    if isinstance(payload, dict):
        data = payload.get("data", payload)
        if isinstance(data, list):
            keys = sorted(data[0])[:8] if data and isinstance(data[0], dict) else []
            return f"{{data: list[{len(data)}] keys={keys}}}"
        if isinstance(data, dict):
            return f"{{data: dict keys={sorted(data)[:8]}}}"
        return f"{{keys={sorted(payload)[:8]}}}"
    return type(payload).__name__


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("base")
    args = parser.parse_args()
    base = args.base.rstrip("/")
    redact = Redactor({})

    for path in CANDIDATES:
        try:
            payload = http_get_json(base + path, token=None, timeout=20, redact=redact)
            print(f"{path:42} OK   {shape(payload)}")
        except Exception as exc:  # noqa: BLE001
            print(f"{path:42} FAIL {str(exc)[:110]}")


if __name__ == "__main__":
    main()
