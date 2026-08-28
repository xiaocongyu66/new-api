#!/usr/bin/env python3
"""Self-check for mock_upstream.py: boots the server on an ephemeral port and
exercises every mode via stdlib urllib. Run: python3 mock_upstream_test.py"""
from __future__ import annotations

import json
import os
import tempfile
import threading
import urllib.request
from http.server import ThreadingHTTPServer


def _post(url: str, mode: str, rid: str, stream: bool = False):
    body = json.dumps({"model": "m", "stream": stream, "max_tokens": 3}).encode()
    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("X-Mock-Mode", mode)
    req.add_header("X-Request-Id", rid)
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=10) as r:
            return r.status, r.headers
    except urllib.error.HTTPError as e:
        return e.code, e.headers


def main() -> None:
    # Create temp dir that lives for the whole test
    td = tempfile.mkdtemp()
    try:
        os.environ["MOCK_NDJSON"] = os.path.join(td, "out.ndjson")
        import mock_upstream

        path = os.environ["MOCK_NDJSON"]
        srv = ThreadingHTTPServer(("127.0.0.1", 0), mock_upstream.Handler)
        srv.daemon_threads = True
        srv.request_queue_size = 128
        port = srv.server_address[1]
        t = threading.Thread(target=srv.serve_forever, daemon=True)
        t.start()
        base = f"http://127.0.0.1:{port}/v1/chat/completions"
        try:
            assert urllib.request.urlopen(base.replace("/v1/chat/completions", "/healthz"), timeout=5).status == 200
            # ttft_500 streaming
            st, _ = _post(base, "ttft_500", "rid-ttft", stream=True)
            assert st == 200, st
            # ratelimit_5s -> 429 + Retry-After
            st, h = _post(base, "ratelimit_5s", "rid-rl")
            assert st == 429, st
            assert h.get("Retry-After") == "5", h.get("Retry-After")
            # ratelimit_missing -> no Retry-After
            st, h = _post(base, "ratelimit_missing", "rid-rlm")
            assert st == 429 and h.get("Retry-After") is None
            # q05 deterministic: crc32 even -> 500, odd -> 200
            even, odd = None, None
            for rid in (f"rid-q05-{i}" for i in range(20)):
                st, _ = _post(base, "q05", rid)
                if mock_upstream.zlib.crc32(rid.encode()) % 2 == 0:
                    even = st
                else:
                    odd = st
                if even is not None and odd is not None:
                    break
            assert even == 500, even
            assert odd == 200, odd
            # ok non-stream
            st, _ = _post(base, "ok", "rid-ok")
            assert st == 200, st
        finally:
            srv.shutdown()
        with open(path) as f:
            lines = [json.loads(l) for l in f if l.strip()]
        modes_seen = {l["mode"] for l in lines}
        for want in ("ttft_500", "ratelimit_5s", "ratelimit_missing", "q05", "ok"):
            assert want in modes_seen, (want, modes_seen)
        assert all({"ts", "request_id", "upstream_model", "status", "mode"} <= set(l) for l in lines)
        print(f"ok: {len(lines)} ndjson lines, modes={sorted(modes_seen)}")
    finally:
        import shutil
        shutil.rmtree(td, ignore_errors=True)


if __name__ == "__main__":
    main()