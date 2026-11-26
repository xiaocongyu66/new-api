#!/usr/bin/env python3
"""Playwright 适配器：读 stdin 的取数任务，写 stdout 的结构化结果。

契约与 Go 侧 internal/upstreamfetch 的 browser 档一致：

  入参 {"url","rows","key","values","timeout_seconds","storage_state"}
  出参 {"rows":[{"key":"...","values":{"字段":"..."}}]}

为什么用外部进程：主程序不必背一个 Chromium；登录态（storage_state）留在
这里，不会进任何数据库。适配器崩溃时用非零退出码 + stderr 说明原因，
调用方据此整次放弃，不会拿到半截数据。
"""

from __future__ import annotations

import json
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from lib import load_secrets, load_ua, proxy_url  # noqa: E402

# 用完整 Chromium 而非 headless shell：后者更容易被指纹识别。
CHROMIUM_CANDIDATES = sorted(
    (Path.home() / ".cache" / "ms-playwright").glob("chromium-*/chrome-linux64/chrome")
)


def chromium_path() -> str | None:
    for candidate in CHROMIUM_CANDIDATES:
        if candidate.exists():
            return str(candidate)
    return None


def main() -> None:
    try:
        job = json.load(sys.stdin)
    except json.JSONDecodeError as exc:
        print(f"bad job json: {exc}", file=sys.stderr)
        raise SystemExit(2) from None

    url = job.get("url", "")
    if not url:
        print("job has no url", file=sys.stderr)
        raise SystemExit(2)

    try:
        from playwright.sync_api import sync_playwright
    except ImportError:
        print(
            "playwright 未安装。执行: pip install playwright && playwright install chromium",
            file=sys.stderr,
        )
        raise SystemExit(3) from None

    timeout_ms = int(job.get("timeout_seconds", 45)) * 1000
    rows_selector = job["rows"]
    key_selector = job["key"]
    value_selectors: dict[str, str] = job.get("values") or {}

    with sync_playwright() as playwright:
        proxy = proxy_url(load_secrets())
        launch_kwargs = {"headless": True, "executable_path": chromium_path()}
        if proxy:
            launch_kwargs["proxy"] = {"server": proxy}
        browser = playwright.chromium.launch(**launch_kwargs)
        context_kwargs = {}
        if job.get("storage_state"):
            context_kwargs["storage_state"] = job["storage_state"]
            # 复用登录态时必须沿用签发时的 UA，否则会话绑定校验会判定
            # 会话被窃取并吊销它（bnd = IP + UA 哈希）。
            context_kwargs["user_agent"] = load_ua(job["storage_state"])
        context = browser.new_context(**context_kwargs)
        page = context.new_page()
        try:
            page.goto(url, wait_until="domcontentloaded", timeout=timeout_ms)
            # 等真正的数据节点出现，而不是等 load——SPA 的 load 早于数据到达。
            page.wait_for_selector(rows_selector, timeout=timeout_ms)

            rows = []
            for node in page.query_selector_all(rows_selector):
                # key 选择器允许写成相对选择器；也允许直接用节点自身（"."）
                key_node = node if key_selector.strip() in (".", "self") else node.query_selector(key_selector)
                if key_node is None:
                    continue
                key = (key_node.inner_text() or "").strip()
                if not key:
                    continue
                values = {}
                for name, selector in value_selectors.items():
                    target_selector, _, attr = selector.partition("@")
                    target = node if target_selector.strip() in (".", "self") else node.query_selector(target_selector)
                    if target is None:
                        continue
                    text = target.get_attribute(attr) if attr else target.inner_text()
                    text = (text or "").strip()
                    if text:
                        values[name] = text
                rows.append({"key": key, "values": values})
        finally:
            context.close()
            browser.close()

    if not rows:
        print(f"selector {rows_selector!r} matched no rows", file=sys.stderr)
        raise SystemExit(4)

    json.dump({"rows": rows}, sys.stdout, ensure_ascii=False)


if __name__ == "__main__":
    main()
