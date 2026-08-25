#!/usr/bin/env python3
"""Dependency-free OpenAI-compatible upstream mock for route-unit EWMA stress (#418 T1/T2).

Behavior is selected per request via the X-Mock-Mode header. Every received
request (including failures) is appended as one ndjson line to the file named
by MOCK_NDJSON so the stress client can reconcile against the gateway audit
endpoint. Intentionally independent of tests/perf/fixtures/mock-upstream.py.
"""
from __future__ import annotations

import json
import os
import time
import zlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(os.getenv("MOCK_PORT", "8099"))
NDJSON_PATH = os.getenv("MOCK_NDJSON", "")

TTFT_MS = {"ttft_500": 0.5, "ttft_2000": 2.0, "ttft_4000": 4.0}
RATELIMIT_AFTER = {"ratelimit_missing": None, "ratelimit_5s": "5", "ratelimit_10s": "10"}


class MockServer(ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 128  # #437: socketserver default backlog is too small


def record(request_id: str, model: str, status: int, mode: str) -> None:
    if not NDJSON_PATH:
        return
    line = json.dumps(
        {"ts": time.time(), "request_id": request_id, "upstream_model": model, "status": status, "mode": mode}
    )
    # Open fresh each call so external deletion/rotation is handled.
    with open(NDJSON_PATH, "a", encoding="utf-8") as f:
        f.write(line + "\n")
        f.flush()


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_args):
        return

    def _send_json(self, status: int, value: dict) -> None:
        body = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _send_429(self, retry_after: str | None) -> None:
        body = json.dumps({"error": {"message": "rate limited", "type": "rate_limit_error"}}).encode()
        self.send_response(429)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        if retry_after is not None:
            self.send_header("Retry-After", retry_after)
        self.end_headers()
        self.wfile.write(body)

    def _stream(self, model: str, tokens: int, pre_delay: float) -> None:
        if pre_delay:
            time.sleep(pre_delay)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()
        for _ in range(tokens):
            data = json.dumps({"choices": [{"delta": {"content": "x"}, "index": 0, "finish_reason": None}]})
            chunk = f"data: {data}\n\n".encode()
            self.wfile.write(f"{len(chunk):x}\r\n".encode() + chunk + b"\r\n")
            self.wfile.flush()
        done = b"data: [DONE]\n\n"
        self.wfile.write(f"{len(done):x}\r\n".encode() + done + b"\r\n0\r\n\r\n")
        self.wfile.flush()

    def do_GET(self):
        if self.path == "/healthz":
            self._send_json(200, {"status": "ok"})
        else:
            self._send_json(404, {"error": {"message": "not found"}})

    def do_POST(self):
        if self.path != "/v1/chat/completions":
            self._send_json(404, {"error": {"message": "not found"}})
            return
        length = int(self.headers.get("Content-Length", 0))
        try:
            body = json.loads(self.rfile.read(length) or b"{}")
        except json.JSONDecodeError:
            self._send_json(400, {"error": {"message": "invalid json"}})
            return
        request_id = self.headers.get("X-Request-Id", "")
        model = body.get("model", "mock-ok")
        mode = self.headers.get("X-Mock-Mode", "ok")
        stream = bool(body.get("stream"))
        tokens = max(1, min(int(body.get("max_tokens") or 16), 64))

        if mode in RATELIMIT_AFTER:
            self._send_429(RATELIMIT_AFTER[mode])
            record(request_id, model, 429, mode)
            return

        if mode == "q05":
            # deterministic, no RNG: crc32(request_id) % 2 picks the failing half
            if zlib.crc32(request_id.encode()) % 2 == 0:
                self._send_json(500, {"error": {"message": "deterministic q05 failure", "type": "server_error"}})
                record(request_id, model, 500, mode)
            else:
                self._respond_ok(request_id, model, mode, stream, tokens)
            return

        if mode in TTFT_MS:
            # record before streaming: a mid-stream client disconnect must not
            # lose the reconciliation line for a request we answered 200.
            record(request_id, model, 200, mode)
            self._stream(model, tokens, TTFT_MS[mode])
            return

        # default ok
        self._respond_ok(request_id, model, mode, stream, tokens)

    def _respond_ok(self, request_id: str, model: str, mode: str, stream: bool, tokens: int) -> None:
        created = int(time.time())
        if not stream:
            self._send_json(200, {"id": "mock", "object": "chat.completion", "created": created, "model": model, "choices": [{"index": 0, "message": {"role": "assistant", "content": "OK " * tokens}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 10, "completion_tokens": tokens, "total_tokens": tokens + 10}})
            record(request_id, model, 200, mode)
            return
        record(request_id, model, 200, mode)
        self._stream(model, tokens, 0.0)


if __name__ == "__main__":
    MockServer(("0.0.0.0", PORT), Handler).serve_forever()
