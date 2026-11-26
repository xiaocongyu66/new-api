#!/usr/bin/env python3
"""按 接口 → 爬虫 → 浏览器 的阶梯，判定一个上游能走哪一档。

只做判定，不写 spec。输出一行结论 + 每档的失败原因，供人决定下一步。

    python3 scripts/probe_ladder.py https://upstream.example
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from lib import Redactor, http_get_json  # noqa: E402

API_CANDIDATES = (
    "/api/pricing",
    "/api/v1/model-plaza",
    "/api/perf-metrics/summary?hours=24",
    "/api/ratio_config",
    "/v1/models",
    "/jk/status.json",
)

NUMBER_HINT = re.compile(r"\d+\.\d+")


def try_api(base: str) -> list[str]:
    """第一档：找出哪些候选返回了可解析的 JSON。"""
    hits = []
    for path in API_CANDIDATES:
        try:
            payload = http_get_json(base + path, token=None, timeout=15, redact=Redactor({}))
        except Exception as exc:  # noqa: BLE001
            print(f"  api  {path:40} FAIL {str(exc)[:90]}")
            continue
        size_hint = "list" if isinstance(payload.get("data"), list) else "dict"
        print(f"  api  {path:40} OK   data={size_hint}")
        hits.append(path)
    return hits


def try_scrape(base: str, path: str = "/pricing") -> bool:
    """第二档：看 HTML 里有没有数字（有则服务端渲染，scrape 可行）。"""
    try:
        import urllib.request

        request = urllib.request.Request(base.rstrip("/") + path)
        request.add_header(
            "User-Agent",
            "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
            "(KHTML, like Gecko) Chrome/131.0 Safari/537.36",
        )
        with urllib.request.urlopen(request, timeout=15) as response:
            html = response.read().decode("utf-8", "replace")
    except Exception as exc:  # noqa: BLE001
        print(f"  scrape {path:38} FAIL {str(exc)[:90]}")
        return False

    numbers = len(NUMBER_HINT.findall(html))
    looks_spa = len(html) < 20_000 and numbers < 5
    print(f"  scrape {path:38} html={len(html)}B numbers={numbers} -> "
          f"{'SPA 壳，scrape 无用' if looks_spa else '可能可爬'}")
    return not looks_spa


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("base")
    args = parser.parse_args()
    base = args.base.rstrip("/")

    print(f"== 第一档：接口 {base}")
    hits = try_api(base)

    print(f"== 第二档：爬虫")
    scrape_ok = try_scrape(base)

    print("== 结论")
    if hits:
        print(f"  落在第一档（api）：{', '.join(hits)}")
        print("  下一步：照着命中的路径写 specs/<host>.json，跑 --only 单渠道验证")
    elif scrape_ok:
        print("  落在第二档（scrape）：页面 HTML 里有数字")
        print("  下一步：写 scrape spec，选择器只用 tag/.class/#id/[attr]/后代")
    else:
        print("  落在第三档（browser）")
        print("  下一步：渲染后先挂 page.on('response') 找隐藏接口，再考虑 DOM/截图")


if __name__ == "__main__":
    main()
