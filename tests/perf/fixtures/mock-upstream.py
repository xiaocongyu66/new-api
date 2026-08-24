#!/usr/bin/env python3
"""
Mock upstream server for state machine stress tests.
Simulates OpenAI-compatible /v1/chat/completions endpoint with controllable behavior.
"""
import os
import json
import asyncio
from datetime import datetime
from typing import Optional
from aiohttp import web

# Configuration via environment
PORT = int(os.getenv("PORT", "8080"))
FAILURE_MODE = os.getenv("FAILURE_MODE", "healthy")  # healthy, timeout, 500, 401, 429
FAILURE_RATE = float(os.getenv("FAILURE_RATE", "0.0"))  # 0.0-1.0
LATENCY_MS = int(os.getenv("LATENCY_MS", "10"))
DELAY_STARTUP = os.getenv("DELAY_STARTUP", "false").lower() == "true"

request_count = 0
start_time = datetime.utcnow()


async def health(request):
    return web.json_response({
        "status": "ok",
        "uptime_seconds": (datetime.utcnow() - start_time).total_seconds(),
        "requests": request_count,
        "failure_mode": FAILURE_MODE,
        "failure_rate": FAILURE_RATE,
    })


async def models(request):
    return web.json_response({
        "object": "list",
        "data": [
            {"id": "gpt-4", "object": "model", "owned_by": "mock"},
            {"id": "gpt-3.5-turbo", "object": "model", "owned_by": "mock"},
            {"id": "claude-3-opus", "object": "model", "owned_by": "mock"},
        ],
    })


async def chat_completions(request):
    global request_count
    request_count += 1

    # Simulate latency
    if LATENCY_MS > 0:
        await asyncio.sleep(LATENCY_MS / 1000.0)

    # Determine if this request should fail
    import random
    should_fail = random.random() < FAILURE_RATE

    # Also check for forced failure mode (non-rate based)
    force_fail = FAILURE_MODE != "healthy"

    if should_fail or force_fail:
        if FAILURE_MODE == "timeout":
            # Hang until client times out
            await asyncio.sleep(3600)
        elif FAILURE_MODE == "500":
            return web.json_response(
                {"error": {"message": "Internal server error", "type": "server_error"}},
                status=500,
            )
        elif FAILURE_MODE == "401":
            return web.json_response(
                {"error": {"message": "Invalid API key", "type": "authentication_error"}},
                status=401,
            )
        elif FAILURE_MODE == "429":
            return web.json_response(
                {"error": {"message": "Rate limit exceeded", "type": "rate_limit_error"}},
                status=429,
                headers={"Retry-After": "60"},
            )

    # Success response
    body = await request.json()
    model = body.get("model", "gpt-3.5-turbo")
    return web.json_response({
        "id": f"chatcmpl-mock-{request_count}",
        "object": "chat.completion",
        "created": int(datetime.utcnow().timestamp()),
        "model": model,
        "choices": [{
            "index": 0,
            "message": {"role": "assistant", "content": "Mock response"},
            "finish_reason": "stop",
        }],
        "usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
    })


async def init_app():
    app = web.Application()
    app.router.add_get("/health", health)
    app.router.add_get("/v1/models", models)
    app.router.add_post("/v1/chat/completions", chat_completions)
    return app


if __name__ == "__main__":
    if DELAY_STARTUP:
        import time
        time.sleep(5)
    web.run_app(init_app(), port=PORT)