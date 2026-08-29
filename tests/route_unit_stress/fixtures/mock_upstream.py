#!/usr/bin/env python3
"""Dependency-free OpenAI-compatible upstream mock for route-unit EWMA stress (#418 T1/T2).

Behavior is selected per request via the X-Mock-Mode header. Every received
request (including failures) is appended as one ndjson line to the file named
by MOCK_NDJSON so the stress client can reconcile against the gateway audit
endpoint. Intentionally independent of tests/perf/fixtures/mock-upstream.py.

Environment variables:
  MOCK_PORT         - listen port (default 8099)
  MOCK_NDJSON       - path to ndjson log file (optional)
  MOCK_FORCE_MODE   - if set, ignore X-Mock-Mode header and force this mode for all requests
"""
from __future__ import annotations

import json
import os
import sys
import threading
import time
import zlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(os.getenv("MOCK_PORT", "8099"))
NDJSON_PATH = os.getenv("MOCK_NDJSON", "")
FORCE_MODE = os.getenv("MOCK_FORCE_MODE", "") or None

VALID_MODES = {"ok", "ttft_500", "ttft_2000", "ttft_4000", "ratelimit_missing", "ratelimit_5s", "ratelimit_10s", "q05", "first_fail_then_ok"}

if FORCE_MODE is not None and FORCE_MODE not in VALID_MODES:
    sys.exit(f"MOCK_FORCE_MODE={FORCE_MODE!r} is not a valid mode; valid: {sorted(VALID_MODES)}")

TTFT_MS = {"ttft_500": 0.5, "ttft_2000": 2.0, "ttft_4000": 4.0}
RATELIMIT_AFTER = {"ratelimit_missing": None, "ratelimit_5s": "5", "ratelimit_10s": "10"}

# S12 first_fail_then_ok state.
#
# Correlated mode: when the channel forwards the client X-Request-Id (channel
# header_override {"X-Request-Id": "{client_header:X-Request-Id}"}), the first
# call for a given id fails and every later call for that same id succeeds. That
# is true per-chain state: attempt 0 of request R gets the 503 and R's retry gets
# the 200, regardless of how other chains interleave.
#
# Uncorrelated fallback: without the override every call arrives with an empty
# id, so per-chain state is impossible and the mode degrades to alternating on a
# call counter (odd -> 503, even -> 200). That still produces fail-then-succeed
# chains in aggregate but cannot prove which retry rescued which request; the
# runner records it as a limitation.
_seen_request_ids: set[str] = set()
_call_counter = 0
_seen_lock = threading.Lock()


def first_attempt_for(request_id: str) -> bool:
    """True when this call must fail so the gateway's retry can succeed.

    With a real request_id this is exact per-chain state. With an empty one it
    falls back to alternating calls, which is all an uncorrelated transport
    permits.
    """
    global _call_counter
    with _seen_lock:
        if not request_id:
            _call_counter += 1
            return _call_counter % 2 == 1
        if request_id in _seen_request_ids:
            return False
        _seen_request_ids.add(request_id)
        return True


class MockServer(ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 128  # #437: socketserver default backlog is too small


def record(request_id: str, model: str, status: int, mode: str) -> None:
    if not NDJSON_PATH:
        return
    line = json.dumps(
        {"ts": time.time(), "request_id": request_id, "upstream_model": model, "status": status, "mode": mode, "port": PORT}
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
            self._send_json(200, {"ok": True, "port": PORT, "force_mode": FORCE_MODE})
        elif self.path == "/_ndjson":
            # Reconciliation needs these records, and when the mock runs inside the
            # cluster (so the gateway can reach it) the stress client has no access
            # to the container filesystem. Serve the log over HTTP instead.
            if not NDJSON_PATH:
                self._send_json(409, {"error": {"message": "MOCK_NDJSON is not configured"}})
                return
            try:
                with open(NDJSON_PATH, "rb") as f:
                    payload = f.read()
            except FileNotFoundError:
                payload = b""
            self.send_response(200)
            self.send_header("Content-Type", "application/x-ndjson")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
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
        # Use FORCE_MODE if set, otherwise fall back to X-Mock-Mode header
        mode = FORCE_MODE if FORCE_MODE is not None else self.headers.get("X-Mock-Mode", "ok")
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

        if mode == "first_fail_then_ok":
            # S12: fail the first attempt of a chain, serve the retry. Both
            # attempts are recorded with their true status so reconciliation
            # sees the whole chain, not just the rescued success.
            if first_attempt_for(request_id):
                self._send_json(503, {"error": {"message": "first attempt fails, retry succeeds", "type": "server_error"}})
                record(request_id, model, 503, mode)
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