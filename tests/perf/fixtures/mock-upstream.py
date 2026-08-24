#!/usr/bin/env python3
"""Dependency-free OpenAI-compatible fixture for #392 scenario matrix."""
from __future__ import annotations

import json
import os
import random
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(os.getenv("PORT", "8099"))
FAILURE_RATIO = float(os.getenv("FAILURE_RATIO", "0.3"))
SLOW_DELAY = float(os.getenv("SLOW_DELAY", "5"))
MODELS = ["mock-fast", "mock-slow", "mock-flaky", "mock-bad", "mock-recover", "mock-weighted"]


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_args):
        return

    def send_json(self, status: int, value: dict) -> None:
        body = json.dumps(value).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            self.send_json(200, {"status": "ok"})
        elif self.path == "/v1/models":
            self.send_json(200, {"object": "list", "data": [{"id": name} for name in MODELS]})
        else:
            self.send_json(404, {"error": {"message": "not found"}})

    def do_POST(self):
        if self.path != "/v1/chat/completions":
            self.send_json(404, {"error": {"message": "not found"}})
            return
        length = int(self.headers.get("Content-Length", 0))
        try:
            body = json.loads(self.rfile.read(length) or b"{}")
        except json.JSONDecodeError:
            self.send_json(400, {"error": {"message": "invalid json"}})
            return
        model = body.get("model", "mock-fast")
        if model == "mock-bad" or model.endswith("-bad"):
            self.send_json(401, {"error": {"message": "invalid mock api key", "type": "authentication_error"}})
            return
        if model in ("mock-flaky", "mock-recover") and random.random() < FAILURE_RATIO:
            self.send_json(500, {"error": {"message": "mock transient failure", "type": "server_error"}})
            return
        if model == "mock-slow":
            time.sleep(SLOW_DELAY)
        tokens = max(1, min(int(body.get("max_tokens") or 16), 64))
        created = int(time.time())
        if not body.get("stream"):
            self.send_json(200, {"id": "mock", "object": "chat.completion", "created": created, "model": model, "choices": [{"index": 0, "message": {"role": "assistant", "content": "OK " * tokens}, "finish_reason": "stop"}], "usage": {"prompt_tokens": 10, "completion_tokens": tokens, "total_tokens": tokens + 10}})
            return
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()
        for _ in range(tokens):
            data = json.dumps({"choices": [{"delta": {"content": "x"}, "index": 0, "finish_reason": None}]})
            chunk = f"data: {data}\n\n".encode()
            self.wfile.write(f"{len(chunk):x}\r\n".encode() + chunk + b"\r\n")
            self.wfile.flush()
            time.sleep(0.05)
        done = b"data: [DONE]\n\n"
        self.wfile.write(f"{len(done):x}\r\n".encode() + done + b"\r\n0\r\n\r\n")
        self.wfile.flush()


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
