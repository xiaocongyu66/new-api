#!/usr/bin/env python3
"""导出 new-api 的渠道清单。

关键点：GET /api/channel/ 在服务端就 Omit("key")，所以这个清单天然不含
密钥。也正因如此，它才可以放心让 agent 读。
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
from lib import Redactor, cache_value, http_get_json, load_secrets, write_json  # noqa: E402


def unwrap(payload: dict) -> list:
    data = payload.get("data", payload)
    if isinstance(data, list):
        return data
    if isinstance(data, dict):
        for key in ("items", "records", "channels", "list"):
            if isinstance(data.get(key), list):
                return data[key]
    return []


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", default="out/channels.json")
    parser.add_argument("--page-size", type=int, default=100)
    args = parser.parse_args()

    secrets = load_secrets()
    redact = Redactor(secrets)
    base = cache_value(secrets, "NEWAPI_BASE").rstrip("/")
    token = cache_value(secrets, "NEWAPI_ADMIN_TOKEN")

    channels: list[dict] = []
    page = 1
    while True:
        url = f"{base}/api/channel/?page={page}&page_size={args.page_size}"
        payload = http_get_json(url, token, redact=redact)
        batch = unwrap(payload)
        if not batch:
            break
        channels.extend(batch)
        if len(batch) < args.page_size:
            break
        page += 1

    trimmed = [
        {
            "id": item.get("id"),
            "name": item.get("name"),
            "base_url": (item.get("base_url") or "").rstrip("/"),
            "models": item.get("models") or "",
            "group": item.get("group") or "",
            "status": item.get("status"),
            "type": item.get("type"),
        }
        for item in channels
    ]
    trimmed.sort(key=lambda row: row["id"] or 0)
    write_json(Path(args.out), trimmed)

    enabled = [row for row in trimmed if row["status"] == 1]
    print(f"导出 {len(trimmed)} 个渠道，其中启用 {len(enabled)} 个 -> {args.out}")
    print("（清单不含密钥：/api/channel/ 服务端已省略 key 字段）")


if __name__ == "__main__":
    main()
