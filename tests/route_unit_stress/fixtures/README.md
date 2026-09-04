# route-unit stress mock upstream

Dependency-free OpenAI-compatible fixture (`mock_upstream.py`). Behavior is
selected per request via the `X-Mock-Mode` header; default `ok`.

## Start

```sh
MOCK_PORT=8099 MOCK_NDJSON=/tmp/upstream.ndjson python3 mock_upstream.py
```

Preflight: `curl -s localhost:8099/healthz` -> `{"ok":true,"port":8099,"force_mode":null}`

## Environment variables

| variable           | description                                                                 |
|--------------------|-----------------------------------------------------------------------------|
| `MOCK_PORT`        | listen port (default `8099`)                                                |
| `MOCK_NDJSON`      | path to ndjson log file (optional; if unset, nothing is recorded)           |
| `MOCK_FORCE_MODE`  | if set, **ignore** `X-Mock-Mode` header and force this mode for **all** requests; must be one of the valid modes below; invalid value causes immediate exit at startup |

## Modes (X-Mock-Mode header / MOCK_FORCE_MODE)

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

```json
{"ts":float,"request_id":str,"upstream_model":str,"status":int,"mode":str,"port":int}
```

- `status` is the HTTP status the mock returned for that request.
- `mode` is the **effective** mode that was applied (after `MOCK_FORCE_MODE` override).
- `port` is the listen port of the mock instance that handled the request (useful when multiple instances write to the same ndjson file).

## Dual-instance deployment example (S2/S3)

Two routes (A, B) each target their own mock instance with different forced modes:

```sh
# Terminal 1: Route A — ttft_2000
MOCK_PORT=18200 MOCK_NDJSON=/tmp/upstream.ndjson MOCK_FORCE_MODE=ttft_2000 python3 mock_upstream.py

# Terminal 2: Route B — ttft_4000
MOCK_PORT=18201 MOCK_NDJSON=/tmp/upstream.ndjson MOCK_FORCE_MODE=ttft_4000 python3 mock_upstream.py
```

Gateway routes:
- Route A -> `http://localhost:18200/v1/chat/completions`
- Route B -> `http://localhost:18201/v1/chat/completions`

Both instances append to the same `/tmp/upstream.ndjson`; the `port` field tells you which upstream handled each request.

## healthz response

```json
{"ok": true, "port": 18200, "force_mode": "ttft_2000"}
```

- `ok`: always `true` when the server is alive
- `port`: the listen port of this instance
- `force_mode`: the value of `MOCK_FORCE_MODE` (or `null` if unset)

## Self-check

```sh
python3 mock_upstream_test.py
```