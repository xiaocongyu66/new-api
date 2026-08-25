# route-unit stress mock upstream

Dependency-free OpenAI-compatible fixture (`mock_upstream.py`). Behavior is
selected per request via the `X-Mock-Mode` header; default `ok`.

## Start

```sh
MOCK_PORT=8099 MOCK_NDJSON=/tmp/upstream.ndjson python3 mock_upstream.py
```

Preflight: `curl -s localhost:8099/healthz` -> `{"status":"ok"}`

## Modes (X-Mock-Mode header)

| mode               | behavior                                                     |
|--------------------|--------------------------------------------------------------|
| ok                 | completion; stream if request body `stream:true`            |
| ttft_500           | streaming SSE, 500ms before first token                     |
| ttft_2000          | streaming SSE, 2000ms before first token                     |
| ttft_4000          | streaming SSE, 4000ms before first token                     |
| ratelimit_missing  | 429, no Retry-After                                          |
| ratelimit_5s       | 429, Retry-After: 5                                          |
| ratelimit_10s      | 429, Retry-After: 10                                         |
| q05                | deterministic 50% fault: crc32(X-Request-Id) even -> 500    |

POST endpoint: `/v1/chat/completions`. Set `X-Request-Id` to tag the user
request (attempt from 0).

## ndjson schema (one line per received request)

`{"ts":float,"request_id":str,"upstream_model":str,"status":int,"mode":str}`

`status` is the HTTP status the mock returned for that request.

## Self-check

```sh
python3 mock_upstream_test.py
```
