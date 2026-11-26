#!/usr/bin/env python3
"""采集上游模型的成功率与延迟。

三种来源，按代价排序：
  jk       公开监控 JSON，模型级 5 分钟采样（xcmapi 这类）
  newapi   /api/perf-metrics/summary + ?model=X（逐小时序列）
  browser  Playwright 渲染登录态页面

成功率口径：green/yellow 记为成功，red 记为失败，未知状态不计入分母。
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import quote, urlparse

sys.path.insert(0, str(Path(__file__).parent))
from lib import (  # noqa: E402
    Redactor,
    http_get_json,
    load_secrets,
    parse_number,
    percentile,
    write_json,
)

SPEC_DIR = Path(__file__).parent.parent / "specs"
SUCCESS_STATES = {"green", "yellow"}

# /jk/status.json 是整站快照（实测 13MB）。同一个上游往往挂多个渠道，
# 逐渠道重复下载既慢又会把连接读到一半就断。按 base 缓存一份即可。
_JK_CACHE: dict[str, tuple[float, dict]] = {}
_JK_TTL_SECONDS = 300.0


def jk_payload(base: str, redact: Redactor) -> dict:
    key = base.rstrip("/")
    now = time.monotonic()
    cached = _JK_CACHE.get(key)
    if cached and now - cached[0] < _JK_TTL_SECONDS:
        return cached[1]
    payload = http_get_json(key + "/jk/status.json", token=None, timeout=120, redact=redact)
    _JK_CACHE[key] = (now, payload)
    return payload


def bucket_hour(iso_text: str) -> int | None:
    """把上游的采样时间归到小时桶。上游可能给 +08:00，必须先规范化。"""
    try:
        stamp = datetime.fromisoformat(iso_text.replace("Z", "+00:00"))
    except (ValueError, AttributeError):
        return None
    return int(stamp.timestamp()) // 3600 * 3600


def collect_jk(base: str, channel: dict, spec: dict, secrets: dict[str, str], redact: Redactor) -> list[dict]:
    """xcmapi 这类上游的公开监控页。

    它按「上游的组」组织，组 id 必须显式配置或在模型集合唯一命中时反推。
    """
    payload = jk_payload(base, redact)
    history = payload.get("model_history") or {}
    if not history:
        raise RuntimeError("jk status has no model_history")

    group_id = str(spec.get("jk_group_id") or "").strip()
    if not group_id:
        # 用渠道密钥拉模型清单，落到唯一匹配的组
        key = secrets.get(f"UPSTREAM_KEY_{channel['id']}", "").strip()
        if not key:
            raise RuntimeError("jk 需要 jk_group_id，或配 UPSTREAM_KEY_<id> 供自动识别")
        models_payload = http_get_json(base.rstrip("/") + "/v1/models", token=key, redact=redact)
        key_models = {item.get("id") for item in (models_payload.get("data") or []) if item.get("id")}
        matches = [
            str(group["id"])
            for group in payload.get("groups") or []
            if set(group.get("supported_models") or []) == key_models
        ]
        if len(matches) != 1:
            raise RuntimeError(
                f"jk_group_id 未配置且自动识别不唯一（候选 {', '.join(sorted(matches)) or '无'}）"
            )
        group_id = matches[0]

    samples = history.get(group_id) or {}
    declared = {m.strip() for m in (channel.get("models") or "").split(",") if m.strip()}

    buckets: dict[tuple[str, int], dict] = defaultdict(lambda: {"ok": 0, "err": 0, "lat": []})
    for model, points in samples.items():
        if declared and model not in declared:
            continue
        for point in points:
            hour = bucket_hour(point.get("at", ""))
            if hour is None:
                continue
            status = str(point.get("status", "")).lower()
            if status not in SUCCESS_STATES and status != "red":
                continue  # 未知状态不计入分母
            slot = buckets[(model, hour)]
            if status in SUCCESS_STATES:
                slot["ok"] += 1
            else:
                slot["err"] += 1
            response_ms = point.get("response_ms")
            if isinstance(response_ms, (int, float)) and response_ms > 0:
                slot["lat"].append(float(response_ms))

    rows = []
    for (model, hour), slot in sorted(buckets.items()):
        total = slot["ok"] + slot["err"]
        if total == 0:
            continue
        latencies = sorted(slot["lat"])
        rows.append(
            {
                "model": model,
                "bucket_ts": hour,
                "samples": total,
                "success_rate": round(slot["ok"] / total * 100, 2),
                "latency_ms": int(sum(latencies) / len(latencies)) if latencies else None,
                "latency_p90_ms": (int(percentile(latencies, 0.9)) if latencies else None),
            }
        )
    return rows


def collect_newapi(base: str, channel: dict, secrets: dict[str, str], redact: Redactor) -> list[dict]:
    key = secrets.get(f"UPSTREAM_KEY_{channel['id']}", "").strip() or None
    summary = http_get_json(base.rstrip("/") + "/api/perf-metrics/summary?hours=24", token=key, redact=redact)
    models = ((summary.get("data") or {}).get("models")) or []
    declared = {m.strip() for m in (channel.get("models") or "").split(",") if m.strip()}

    rows = []
    for entry in models:
        name = (entry.get("model_name") or "").strip()
        if not name or (declared and name not in declared):
            continue
        # 模型名可能含中文，必须转义后再拼进 URL，否则 urllib 会抛
        # 'ascii codec can't encode'。
        series = http_get_json(
            base.rstrip("/") + f"/api/perf-metrics?model={quote(name, safe='')}&hours=24",
            token=key,
            redact=redact,
        )
        points = ((series.get("data") or {}).get("series")) or []
        # 上游的点不一定是整点，粒度也不固定（apiloop 18 点、aiwble 1416 点）。
        # 先统一归到小时桶，再算成功率的样本加权平均与延迟的 p90，
        # 否则两份数据的「一个点」含义不同，图表会不可比。
        buckets: dict[int, dict] = {}
        for point in points:
            ts = point.get("ts")
            rate = point.get("success_rate")
            if not ts or rate is None:
                continue
            hour = int(ts) // 3600 * 3600
            slot = buckets.setdefault(hour, {"rates": [], "lat": []})
            slot["rates"].append(float(rate))
            latency = point.get("avg_latency_ms")
            if isinstance(latency, (int, float)) and latency > 0:
                slot["lat"].append(float(latency))

        for hour, slot in sorted(buckets.items()):
            latencies = sorted(slot["lat"])
            rows.append(
                {
                    "model": name,
                    "bucket_ts": hour,
                    "samples": len(slot["rates"]),
                    "success_rate": round(sum(slot["rates"]) / len(slot["rates"]), 2),
                    "latency_ms": int(sum(latencies) / len(latencies)) if latencies else None,
                    "latency_p90_ms": (int(percentile(latencies, 0.9)) if latencies else None),
                }
            )
    return rows


def collect_channel(channel: dict, secrets: dict[str, str], redact: Redactor) -> dict:
    host = urlparse(channel["base_url"]).netloc
    spec_path = SPEC_DIR / f"{host}.json"
    spec = json.loads(spec_path.read_text(encoding="utf-8")) if spec_path.exists() else {}

    # spec 明确指定了某一档时只走那一档；没写 spec 才按 jk → newapi 依次试探。
    pinned = spec.get("strategy")
    order = [pinned] if pinned else ["jk", "newapi"]

    attempts: list[str] = []
    for strategy in order:
        try:
            if strategy == "jk":
                rows = collect_jk(channel["base_url"], channel, spec, secrets, redact)
            elif strategy == "newapi":
                rows = collect_newapi(channel["base_url"], channel, secrets, redact)
            else:
                attempts.append(f"未知 strategy={strategy}")
                continue
            if rows:
                return {
                    "channel_id": channel["id"],
                    "channel_name": channel["name"],
                    "host": host,
                    "strategy": strategy,
                    "rows": rows,
                }
            attempts.append(f"{strategy}: 0 行")
        except Exception as exc:  # noqa: BLE001
            attempts.append(redact(f"{strategy}: {exc}")[:300])

    return {
        "channel_id": channel["id"],
        "channel_name": channel["name"],
        "host": host,
        "strategy": "none",
        "rows": [],
        "error": " | ".join(attempts) or "无可用来源",
        "needs_spec": True,
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--channels", default="out/channels.json")
    parser.add_argument("--out", default="out/health.json")
    parser.add_argument("--only", type=int, default=0)
    args = parser.parse_args()

    secrets = load_secrets()
    redact = Redactor(secrets)
    channels = json.loads(Path(args.channels).read_text(encoding="utf-8"))
    channels = [c for c in channels if c.get("status") == 1 and c.get("base_url")]
    if args.only:
        channels = [c for c in channels if c.get("id") == args.only]

    results = []
    for index, channel in enumerate(channels, 1):
        result = collect_channel(channel, secrets, redact)
        summary = f"{len(result['rows'])} 个点" if result["rows"] else result.get("error", "无数据")
        print(f"[{index}/{len(channels)}] {channel['name']} ({channel['id']}) -> {result['strategy']}: {summary}")
        results.append(result)

    write_json(Path(args.out), results)
    ok = sum(1 for r in results if r["rows"])
    print(f"\n完成：{ok}/{len(results)} 个渠道取到健康数据 -> {args.out}")
    # 一个都没取到时必须非 0 退出：否则 systemd 认为成功，探活静默停止更新。
    if ok == 0:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
