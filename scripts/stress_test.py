#!/usr/bin/env python3
"""OpenAI-compatible LLM stress runner for local and k8s smoke/load tests.

No external deps beyond aiohttp. Measures non-streaming latency and streaming TTFT/ITL/TPS.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import math
import statistics
import time
from dataclasses import dataclass, field
from typing import Any

import aiohttp
from aiohttp import web

SHORT_PROMPT = "Reply with OK."
LONG_PROMPT = "Write a detailed but concise explanation of API gateway retry behavior, streaming behavior, and failure isolation."

PREFERRED_AUTO_MODELS = ("mock-fast", "mock-slow", "kimi-k3", "agnes-2.5-flash", "deepseek-v4-flash", "fast-l", "good-m")
EXCLUDED_AUTO_MODEL_PREFIXES = ("ox", "gpt", "opus", "gemini")
EXCLUDED_AUTO_MODEL_KEYWORDS = ("image", "imagine", "edit", "video", "tts", "stt", "speech", "embedding", "rerank", "moderation")


@dataclass
class Metrics:
    name: str
    latencies: list[float] = field(default_factory=list)
    ttfts: list[float] = field(default_factory=list)
    itls: list[float] = field(default_factory=list)
    output_chars: int = 0
    chunks: int = 0
    status: dict[int, int] = field(default_factory=dict)
    errors: list[str] = field(default_factory=list)
    records: list[dict[str, Any]] = field(default_factory=list)
    started: float = 0.0
    ended: float = 0.0

    def record_status(self, code: int) -> None:
        self.status[code] = self.status.get(code, 0) + 1

    def record_request(self, *, stream: bool, status: int, latency: float, ttft: float | None, request_id: str) -> None:
        t_offset = time.perf_counter() - self.started if self.started else 0.0
        self.records.append({
            "t": round(t_offset, 3),
            "stream": stream,
            "status": status,
            "latency": round(latency, 3),
            "ttft": round(ttft, 3) if ttft is not None else None,
            "request_id": request_id,
        })

    def timeline(self, bucket: int = 5) -> list[dict[str, Any]]:
        buckets: dict[int, list[int]] = {}
        for r in self.records:
            b = int(r["t"] // bucket) * bucket
            buckets.setdefault(b, []).append(r["status"])
        return [
            {"t_sec": k, "completed": len(v), "ok": sum(1 for s in v if s == 200)}
            for k, v in sorted(buckets.items())
        ]

    def sample_error(self, msg: str) -> None:
        if len(self.errors) < 8:
            self.errors.append(msg[:200])

    @staticmethod
    def pct(values: list[float], p: float) -> float | None:
        if not values:
            return None
        vals = sorted(values)
        idx = max(0, min(len(vals) - 1, math.ceil(len(vals) * p / 100) - 1))
        return vals[idx]

    def summary(self) -> dict[str, Any]:
        total = sum(self.status.values())
        duration = max(self.ended - self.started, 0.001)
        return {
            "name": self.name,
            "requests": total,
            "success": self.status.get(200, 0),
            "success_rate": round(self.status.get(200, 0) * 100 / total, 2) if total else 0,
            "duration_sec": round(duration, 2),
            "rps": round(total / duration, 2),
            "status": self.status,
            "lat_avg": round(statistics.mean(self.latencies), 3) if self.latencies else None,
            "ttft_avg": round(statistics.mean(self.ttfts), 3) if self.ttfts else None,
            "lat_p50": round(self.pct(self.latencies, 50), 3) if self.latencies else None,
            "lat_p90": round(self.pct(self.latencies, 90), 3) if self.latencies else None,
            "lat_p99": round(self.pct(self.latencies, 99), 3) if self.latencies else None,
            "ttft_p50": round(self.pct(self.ttfts, 50), 3) if self.ttfts else None,
            "ttft_p90": round(self.pct(self.ttfts, 90), 3) if self.ttfts else None,
            "ttft_p99": round(self.pct(self.ttfts, 99), 3) if self.ttfts else None,
            "itl_avg": round(statistics.mean(self.itls), 3) if self.itls else None,
            "chunks": self.chunks,
            "chars_per_sec": round(self.output_chars / duration, 2),
            "errors": self.errors,
            "timeline": self.timeline(),
            "records": self.records,
        }


class MockUpstream:
    def __init__(self, port: int):
        self.port = port
        self.app = web.Application()
        self.app.router.add_get("/v1/models", self.models)
        self.app.router.add_post("/v1/chat/completions", self.chat)
        self.runner: web.AppRunner | None = None

    async def models(self, request: web.Request) -> web.Response:
        return web.json_response({"object": "list", "data": [
            {"id": "mock-fast"}, {"id": "mock-slow"}, {"id": "mock-flaky"}, {"id": "mock-bad"},
        ]})

    async def chat(self, request: web.Request) -> web.StreamResponse:
        body = await request.json()
        model = body.get("model", "mock-fast")
        stream = bool(body.get("stream"))
        max_tokens = int(body.get("max_tokens") or 16)
        if "bad" in model:
            # 401 mimics a dead key so the key verification / cascade path gets exercised.
            return web.json_response({"error": {"message": "invalid mock api key", "type": "invalid_request_error"}}, status=401)
        if "flaky" in model:
            import random
            if random.random() < 0.3:
                return web.json_response({"error": {"message": "mock transient failure"}}, status=500)
        if "slow" in model:
            await asyncio.sleep(0.8)
        if not stream:
            return web.json_response({
                "id": "mock",
                "object": "chat.completion",
                "created": int(time.time()),
                "model": model,
                "choices": [{"index": 0, "message": {"role": "assistant", "content": "OK " * max(1, min(max_tokens, 64))}, "finish_reason": "stop"}],
                "usage": {"prompt_tokens": 10, "completion_tokens": max_tokens, "total_tokens": max_tokens + 10},
            })
        resp = web.StreamResponse(status=200, headers={"Content-Type": "text/event-stream"})
        await resp.prepare(request)
        for i in range(max(1, min(max_tokens, 64))):
            chunk = {"choices": [{"delta": {"content": "x"}, "index": 0, "finish_reason": None}]}
            await resp.write(f"data: {json.dumps(chunk)}\n\n".encode())
            await asyncio.sleep(0.02)
        await resp.write(b"data: [DONE]\n\n")
        await resp.write_eof()
        return resp

    async def start(self) -> None:
        self.runner = web.AppRunner(self.app)
        await self.runner.setup()
        await web.TCPSite(self.runner, "127.0.0.1", self.port).start()

    async def stop(self) -> None:
        if self.runner:
            await self.runner.cleanup()


async def one_non_stream(session: aiohttp.ClientSession, args: argparse.Namespace, metrics: Metrics, prompt: str, max_tokens: int) -> None:
    start = time.perf_counter()
    try:
        async with session.post(
            f"{args.target_url.rstrip('/')}/chat/completions",
            headers={"Authorization": f"Bearer {args.api_key}", "Content-Type": "application/json", **host_header(args)},
            json={"model": args.model, "messages": [{"role": "user", "content": prompt}], "max_tokens": max_tokens, "stream": False},
            timeout=aiohttp.ClientTimeout(total=args.request_timeout),
        ) as resp:
            text = await resp.text()
            metrics.record_status(resp.status)
            latency = time.perf_counter() - start
            rid = resp.headers.get("X-Oneapi-Request-Id", "")
            metrics.latencies.append(latency)
            if resp.status == 200:
                metrics.ttfts.append(latency)
                metrics.output_chars += len(text)
            else:
                metrics.sample_error(f"[{rid}] {resp.status}: {text[:150]}")
            metrics.record_request(stream=False, status=resp.status, latency=latency, ttft=latency if resp.status == 200 else None, request_id=rid)
    except Exception as exc:  # noqa: BLE001 - stress harness must survive any per-request error
        latency = time.perf_counter() - start
        metrics.record_status(599)
        metrics.latencies.append(latency)
        metrics.record_request(stream=False, status=599, latency=latency, ttft=None, request_id="")
        metrics.sample_error(type(exc).__name__ + ": " + str(exc))


async def one_stream(session: aiohttp.ClientSession, args: argparse.Namespace, metrics: Metrics, prompt: str, max_tokens: int) -> None:
    start = time.perf_counter()
    first = None
    try:
        async with session.post(
            f"{args.target_url.rstrip('/')}/chat/completions",
            headers={"Authorization": f"Bearer {args.api_key}", "Content-Type": "application/json", **host_header(args)},
            json={"model": args.model, "messages": [{"role": "user", "content": prompt}], "max_tokens": max_tokens, "stream": True},
            timeout=aiohttp.ClientTimeout(total=args.request_timeout),
        ) as resp:
            rid = resp.headers.get("X-Oneapi-Request-Id", "")
            # Record the terminal status exactly once, after the body has been
            # fully read or has failed — never on header arrival.
            terminal_status = resp.status
            buffer = ""

            def feed(buf: str, now: float) -> tuple[str, int, int]:
                """Consume complete SSE frames; return (remainder, chunks, chars)."""
                chunks = chars = 0
                while "\n\n" in buf:
                    frame, buf = buf.split("\n\n", 1)
                    if not frame.startswith("data:") or "[DONE]" in frame:
                        continue
                    chunks += 1
                    chars += len(frame) - 5  # payload only, minus "data:"
                    if last_holder[0] is not None:
                        metrics.itls.append(now - last_holder[0])
                    last_holder[0] = now
                return buf, chunks, chars

            last_holder = [None]
            async for raw in resp.content:
                now = time.perf_counter()
                if first is None:
                    first = now
                    metrics.ttfts.append(first - start)
                buffer += raw.decode(errors="ignore")
                buffer, chunks, chars = feed(buffer, now)
                metrics.chunks += chunks
                metrics.output_chars += chars
            latency = time.perf_counter() - start
            metrics.latencies.append(latency)
            if terminal_status != 200:
                metrics.sample_error(f"[{rid}] stream status {terminal_status}")
            ttft = (first - start) if first is not None else None
            metrics.record_status(terminal_status)
            metrics.record_request(stream=True, status=terminal_status, latency=latency, ttft=ttft, request_id=rid)
    except Exception as exc:  # noqa: BLE001 - stress harness must survive any per-request error
        latency = time.perf_counter() - start
        # Headers may have been 200 but the stream aborted: count ONE failure.
        metrics.record_status(599)
        metrics.latencies.append(latency)
        metrics.record_request(stream=True, status=599, latency=latency, ttft=None, request_id="")
        metrics.sample_error(type(exc).__name__ + ": " + str(exc))


def host_header(args: argparse.Namespace) -> dict[str, str]:
    return {"Host": args.host_header} if args.host_header else {}


async def resolve_model(session: aiohttp.ClientSession, args: argparse.Namespace) -> None:
    args.requested_model = args.model
    if args.model != "auto":
        try:
            async with session.get(
                f"{args.target_url.rstrip('/')}/models",
                headers={"Authorization": f"Bearer {args.api_key}", **host_header(args)},
                timeout=aiohttp.ClientTimeout(total=args.request_timeout),
            ) as resp:
                if resp.status == 200:
                    args.available_models = [m.get("id", "") for m in (await resp.json()).get("data", []) if isinstance(m, dict)]
        except Exception:  # noqa: BLE001 - model list is best-effort
            args.available_models = []
        return
    async with session.get(
        f"{args.target_url.rstrip('/')}/models",
        headers={"Authorization": f"Bearer {args.api_key}", **host_header(args)},
        timeout=aiohttp.ClientTimeout(total=args.request_timeout),
    ) as resp:
        text = await resp.text()
        if resp.status != 200:
            raise RuntimeError(f"model discovery failed: {resp.status}: {text[:200]}")
        payload = json.loads(text)
    models = [m.get("id", "") for m in payload.get("data", []) if isinstance(m, dict) and m.get("id")]
    if not models:
        raise RuntimeError("model discovery returned no models")
    args.available_models = models
    by_name = {m.lower(): m for m in models}
    candidates = ordered_unique(
        [by_name[p] for p in PREFERRED_AUTO_MODELS if p in by_name]
        + [m for m in models if is_chat_candidate(m)]
        + models
    )
    failures = []
    for model in candidates[:args.model_probe_limit]:
        ok, detail = await probe_chat_model(session, args, model)
        if ok:
            args.model = model
            print(f"resolved auto model: {args.model}")
            return
        failures.append(f"{model}: {detail}")
    raise RuntimeError("no chat-capable model found: " + "; ".join(failures[:8]))


def ordered_unique(values: list[str]) -> list[str]:
    seen = set()
    result = []
    for value in values:
        if value not in seen:
            seen.add(value)
            result.append(value)
    return result


async def probe_chat_model(session: aiohttp.ClientSession, args: argparse.Namespace, model: str) -> tuple[bool, str]:
    try:
        async with session.post(
            f"{args.target_url.rstrip('/')}/chat/completions",
            headers={"Authorization": f"Bearer {args.api_key}", "Content-Type": "application/json", **host_header(args)},
            json={"model": model, "messages": [{"role": "user", "content": SHORT_PROMPT}], "max_tokens": 1, "stream": False},
            timeout=aiohttp.ClientTimeout(total=min(args.request_timeout, 20)),
        ) as resp:
            text = await resp.text()
            return resp.status == 200, f"{resp.status} {text[:120]}"
    except Exception as exc:  # noqa: BLE001 - stress harness must survive any per-request error
        return False, f"{type(exc).__name__}: {exc}"


def is_chat_candidate(model: str) -> bool:
    normalized = model.lower()
    return not normalized.startswith(EXCLUDED_AUTO_MODEL_PREFIXES) and not any(k in normalized for k in EXCLUDED_AUTO_MODEL_KEYWORDS)


async def worker(session: aiohttp.ClientSession, args: argparse.Namespace, metrics: Metrics) -> None:
    end = time.perf_counter() + args.duration
    while time.perf_counter() < end:
        long_case = random_ratio(args.long_ratio)
        stream_case = random_ratio(args.stream_ratio)
        prompt = LONG_PROMPT if long_case else SHORT_PROMPT
        max_tokens = args.long_tokens if long_case else args.short_tokens
        if stream_case:
            await one_stream(session, args, metrics, prompt, max_tokens)
        else:
            await one_non_stream(session, args, metrics, prompt, max_tokens)
        await asyncio.sleep(args.pacing_ms / 1000)


def random_ratio(ratio: float) -> bool:
    # local import avoids global RNG surprises in forked runners; stdlib only.
    import random
    return random.random() < ratio


async def run(args: argparse.Namespace) -> list[dict[str, Any]]:
    mock = MockUpstream(args.mock_port) if args.use_mock else None
    if mock:
        await mock.start()
    try:
        results = []
        scenarios = [(args.scenario, args.concurrency, args.duration)]
        if args.scenario == "matrix":
            scenarios = [
                ("short_non_stream", max(1, args.concurrency), max(5, args.short_duration)),
                ("long_stream", max(1, args.stream_concurrency), max(5, args.long_duration)),
                ("mixed_stream_non_stream", max(1, args.concurrency), max(5, args.duration)),
            ]
        connector = aiohttp.TCPConnector(limit=max(args.concurrency, args.stream_concurrency) * 3)
        async with aiohttp.ClientSession(connector=connector) as session:
            await resolve_model(session, args)
            mixed_model = args.mixed_model
            if mixed_model == "auto":
                mixed_model = "mock-flaky" if "mock-flaky" in getattr(args, "available_models", []) else args.model
            elif not mixed_model:
                mixed_model = args.model
            for name, concurrency, duration in scenarios:
                local = argparse.Namespace(**vars(args))
                local.concurrency = concurrency
                local.duration = duration
                if name == "short_non_stream":
                    local.stream_ratio = 0.0
                    local.long_ratio = 0.0
                elif name == "long_stream":
                    local.stream_ratio = 1.0
                    local.long_ratio = 1.0
                elif name == "mixed_stream_non_stream":
                    local.model = mixed_model
                metrics = Metrics(name=name)
                metrics.started = time.perf_counter()
                await asyncio.gather(*(worker(session, local, metrics) for _ in range(concurrency)))
                metrics.ended = time.perf_counter()
                summary = metrics.summary()
                summary["model"] = local.model
                results.append(summary)
        return results
    finally:
        if mock:
            await mock.stop()


def write_report(results: list[dict[str, Any]], args: argparse.Namespace) -> None:
    lines = [
        "# new-api K8s/Local LLM 压测报告",
        "",
        f"- target_url: `{args.target_url}`",
        f"- model: `{args.model}`",
    ]
    if getattr(args, "requested_model", args.model) != args.model:
        lines.append(f"- requested_model: `{args.requested_model}`")
    lines += [
        f"- stream_ratio: `{args.stream_ratio}` long_ratio: `{args.long_ratio}`",
        f"- short_tokens/long_tokens: `{args.short_tokens}/{args.long_tokens}`",
        "",
        "| 场景 | 模型 | 请求 | 成功率 | RPS | Lat avg/P50/P90/P99(s) | TTFT avg/P50/P90/P99(s) | ITL avg(s) | chunks | chars/s | 状态码 |",
        "|---|---|---:|---:|---:|---|---|---:|---:|---:|---|",
    ]
    for r in results:
        lines.append(
            f"| {r['name']} | {r.get('model', args.model)} | {r['requests']} | {r['success_rate']}% | {r['rps']} | "
            f"{r.get('lat_avg')}/{r['lat_p50']}/{r['lat_p90']}/{r['lat_p99']} | "
            f"{r.get('ttft_avg')}/{r['ttft_p50']}/{r['ttft_p90']}/{r['ttft_p99']} | "
            f"{r['itl_avg']} | {r['chunks']} | {r['chars_per_sec']} | `{r['status']}` |"
        )
    lines += [
        "",
        "## 完成时间线（5s 桶：窗口内完成的请求数/成功数）",
        "| 场景 | 窗口(s) | 完成 | 成功 |",
        "|---|---:|---:|---:|",
    ]
    for r in results:
        for t in r.get("timeline", []):
            lines.append(f"| {r['name']} | {t['t_sec']}-{t['t_sec'] + 5} | {t['completed']} | {t['ok']} |")
    errors = [e for r in results for e in r.get("errors", [])]
    if errors:
        lines += ["", "## Error samples", *[f"- `{e}`" for e in errors[:10]]]
    if args.requests_file:
        with open(args.requests_file, "w", encoding="utf-8") as rf:
            rf.write("scenario,model,t_offset_s,stream,status,latency_s,ttft_s,request_id\n")
            for r in results:
                rf.writelines(f"{r['name']},{r.get('model', args.model)},{rec['t']},{rec['stream']},"
                        f"{rec['status']},{rec['latency']},{rec['ttft']},{rec['request_id']}\n" for rec in r.get("records", []))
        lines += ["", f"- 请求级明细 CSV: `{args.requests_file}`"]
    safe_args = dict(vars(args))
    if safe_args.get("api_key"):
        safe_args["api_key"] = "***"
    data = {"args": safe_args, "results": results}
    with open(args.report_file, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")
    with open(args.json_file, "w", encoding="utf-8") as f:
        json.dump(data, f, ensure_ascii=False, indent=2)


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser()
    p.add_argument("--scenario", default="matrix", choices=["matrix", "short_non_stream", "long_stream", "mixed_stream_non_stream"])
    p.add_argument("--target-url", default="http://127.0.0.1:3182/v1")
    p.add_argument("--host-header", default="")
    p.add_argument("--api-key", default="")
    p.add_argument("--model", default="auto")
    p.add_argument("--concurrency", type=int, default=30)
    p.add_argument("--stream-concurrency", type=int, default=10)
    p.add_argument("--duration", type=float, default=60)
    p.add_argument("--short-duration", type=float, default=20)
    p.add_argument("--long-duration", type=float, default=60)
    p.add_argument("--short-tokens", type=int, default=16)
    p.add_argument("--long-tokens", type=int, default=256)
    p.add_argument("--stream-ratio", type=float, default=0.5)
    p.add_argument("--long-ratio", type=float, default=0.5)
    p.add_argument("--pacing-ms", type=int, default=10)
    p.add_argument("--request-timeout", type=float, default=60)
    p.add_argument("--use-mock", action="store_true")
    p.add_argument("--mock-port", type=int, default=8099)
    p.add_argument("--report-file", default="report.md")
    p.add_argument("--json-file", default="metrics.json")
    p.add_argument("--min-success-rate", type=float, default=1.0)
    p.add_argument("--model-probe-limit", type=int, default=80)
    p.add_argument("--mixed-model", default="auto", help="mixed 场景使用的模型；auto 优先 mock-flaky")
    p.add_argument("--requests-file", default="", help="请求级明细 CSV 输出路径")
    return p.parse_args()


if __name__ == "__main__":
    args = parse_args()
    result = asyncio.run(run(args))
    write_report(result, args)
    failed = [r for r in result if r["success_rate"] < args.min_success_rate]
    if failed:
        names = ", ".join(f"{r['name']}={r['success_rate']}%" for r in failed)
        raise SystemExit(f"success rate below {args.min_success_rate}%: {names}")
