# State Machine Stress Test Scenarios (Issue #392)

This document describes the 8 state-machine scenarios for validating the channel health
state machine under various stress conditions. Each scenario maps to a k6 script and
a corresponding assertion alias used by `tests/perf/assertions/state_machine.py`.

## Scenario Matrix

| # | Scenario Name | k6 Script | Assertion Alias | Description |
|---|---------------|-----------|-----------------|-------------|
| 1 | Pool Soft Derating | `pool-soft-derating.js` | `pool-pressure` | Sustained load causes gradual pool derating |
| 2 | CAS Contention | `cas-contention.js` | `pool-pressure` | Concurrent health updates trigger CAS retries |
| 3 | Bad Key Cascade | `bad-key-cascade.js` | `bad-key-cascade` | Invalid credentials cascade through channels |
| 4 | Recovery Decay | `recovery-decay.js` | `gray-failure` | Recovery after degradation is slower than expected |
| 5 | Pool Pressure | `pool-pressure.js` | `pool-pressure` | Connection pool exhaustion and retry failures |
| 6 | Gray Failure | `gray-failure.js` | `gray-failure` | Partial degradation with steady-state failure ratio |
| 7 | Weight Distribution | `weight-distribution.js` | `weight-distribution` | Channel selection follows configured weights |
| 8 | Timeout Classification | `timeout-classification.js` | `timeout-classification` | Distinguish transport timeouts from upstream errors |

## Running the Matrix

### Via GitHub Actions (workflow_dispatch)

```bash
gh workflow run state-machine-test.yml \
  -f scenario=all \
  -f target_url=http://gateway:3000 \
  -f api_key=$API_KEY \
  -f model=gpt-4 \
  -f vus=100 \
  -f duration=120s
```

### Local Orchestrator

```bash
cd tests/perf
python run_state_matrix.py \
  --scenario all \
  --target-url http://localhost:3000 \
  --api-key $API_KEY \
  --report-dir /tmp/state-reports \
  --worker-log-source "worker*.log" \
  --health-csv-source "*channel_model_health*.csv"
```

### Single Scenario

```bash
python run_state_matrix.py \
  --scenario 3 \
  --target-url http://localhost:3000 \
  --api-key $API_KEY \
  --report-dir /tmp/state-reports
```

## Scenario Details

### 1. Pool Soft Derating (`pool-soft-derating.js` → `pool-pressure`)

**Purpose**: Validate that sustained load causes graceful pool derating rather than
catastrophic failure. The upstream mock gradually increases latency and returns
503/429 as pool utilization approaches soft limits.

**Key Metrics**:
- `derating_events` counter (tags: status, latency_bucket)
- `derating_latency` trend
- Error rate threshold: < 15%
- p99 latency threshold: < 10s

**Env Vars**: `DERATING_MODEL`, `VUS` (default 150), `DURATION` (default 180s), `RAMP_DURATION` (default 60s)

---

### 2. CAS Contention (`cas-contention.js` → `pool-pressure`)

**Purpose**: Verify that rapid concurrent health updates on `channel_model_health`
trigger compare-and-swap (CAS) contention, and the system handles retries correctly.
409 Conflict or 503 with `Retry-After` indicates CAS contention.

**Key Metrics**:
- `cas_conflicts` counter (tags: status, retry_after)
- `cas_latency` trend
- Error rate threshold: < 10%

**Env Vars**: `CAS_MODEL`, `VUS` (default 100), `DURATION` (default 120s)

---

### 3. Bad Key Cascade (`bad-key-cascade.js` → `bad-key-cascade`)

**Purpose**: Test cascade behavior when a channel is configured with invalid
credentials. Upstream returns 401/500; these are **expected** fault outcomes,
not test failures. The state machine should mark the key/channel disabled and
route around it.

**Key Metrics**:
- `bad_key_responses` counter (tags: status, request_id, type)
- Accepted status codes: 200, 401, 500
- Unexpected status threshold: < 10%

**Env Vars**: `BAD_KEY_MODEL`, `VUS` (default 20), `DURATION` (default 60s)

**Assertion Checks**:
- `bad-key-cascade-evidence`: channel marked disabled in logs
- `health-rows`: health CSV contains rows

---

### 4. Recovery Decay (`recovery-decay.js` → `gray-failure`)

**Purpose**: After a period of high failure ratio (50%), the system should recover
to a low failure ratio (5%). The "decay" is the gap between expected exponential
recovery and actual observed recovery curve.

**Key Metrics**:
- `recovery_success` / `recovery_failure` counters
- `recovery_latency` trend
- Final error rate threshold: < 15%

**Env Vars**: `RECOVERY_MODEL`, `VUS` (default 50), `DURATION` (default 300s), `FAILURE_RATIO_START` (0.50), `FAILURE_RATIO_END` (0.05)

---

### 5. Pool Pressure (`pool-pressure.js` → `pool-pressure`)

**Purpose**: Ramp load to saturate connection pools and trigger retry exhaustion.
Uses a flaky model that simulates intermittent upstream failures.

**Key Metrics**:
- `pool_pressure_errors` counter (tags: status, is_retryable)
- Error rate threshold: < 20% (higher budget under pressure)

**Env Vars**: `FLAKY_MODEL`, `VUS` (default 200), `DURATION` (default 120s), `RAMP_DURATION` (default 60s)

**Assertion Checks**:
- `pool-pressure-evidence`: "emergency recover" or "pool pressure" in worker logs
- `health-rows`: health CSV contains rows

---

### 6. Gray Failure (`gray-failure.js` → `gray-failure`)

**Purpose**: Constant load with a configured partial failure ratio (default 30%).
Observes steady-state behavior under sustained partial degradation. Validates
that the state machine correctly tracks channel health without flapping.

**Key Metrics**:
- `gray_failure_success` / `gray_failure_failure` counters
- `gray_failure_latency` trend
- Availability threshold: success rate ≥ 1 - FAILURE_RATIO - 0.05

**Env Vars**: `FLAKY_MODEL`, `VUS` (default 50), `DURATION` (default 120s), `FAILURE_RATIO` (default 0.30)

**Assertion Checks**:
- `gray-failure-states`: observed states include expected degraded/healthy
- `health-rows`: health CSV contains rows

---

### 7. Weight Distribution (`weight-distribution.js` → `weight-distribution`)

**Purpose**: Verify that channel selection follows configured weights (e.g., 100:50:10).
The k6 script emits `X-Perf-Scenario: weight-distribution` header for server-side
tracking. Actual weight verification is done via Python DB/log assertions
correlating `channel_id` from response headers or logs.

**Key Metrics**:
- `distribution_requests` counter (tagged by channel_id)
- `distribution_latency` trend per channel
- Error rate threshold: < 5%
- At least 3 distinct channels observed

**Env Vars**: `MODEL`, `VUS` (default 100), `DURATION` (default 120s)

**Assertion Checks**:
- `weight-distribution-ratio`: observed ratios within 15% of configured
- `weight-distribution-channels`: ≥ 3 distinct channels

---

### 8. Timeout Classification (`timeout-classification.js` → `timeout-classification`)

**Purpose**: Distinguish k6 transport timeouts (request timeout exceeded) from
upstream HTTP error responses (4xx/5xx received before timeout). Critical for
correct state machine transitions: transport timeout → `degraded`, upstream
error → `unhealthy`/`disabled`.

**Key Metrics**:
- `timed_out_requests` counter (k6 transport timeouts)
- `upstream_http_errors` counter (4xx/5xx before timeout)
- `completed_latency` trend (excludes timeouts)
- Upstream error rate threshold: < 10%

**Env Vars**: `SLOW_MODEL`, `VUS` (default 30), `DURATION` (default 60s), `TIMEOUT` (default 2s)

**Assertion Checks**:
- `timeout-classification-local`: local_failure_count > 0 (transport timeouts recorded)
- `timeout-classification-upstream`: upstream HTTP errors tracked separately
- `health-rows`: health CSV contains rows

## State Reset & Topology Preconditions

**IMPORTANT**: These scenarios assume specific initial state and topology.
The orchestrator **does not** automatically reset state or deploy topology.
Before running the matrix, you MUST prepare:

### Required Preconditions

1. **Channel Model Health Table Reset**
   ```sql
   TRUNCATE channel_model_health RESTART IDENTITY;
   -- Re-insert baseline healthy channels for each model under test
   ```

2. **Gateway Configuration**
   - Models configured with correct channel weights (for scenario 7)
   - Flaky/bad/slow/derating/CAS/recovery models deployed to mock upstream
   - API keys configured (valid for most, invalid for scenario 3)

3. **Mock Upstream Deployment**
   - `tests/perf/fixtures/mock-upstream.py` running with appropriate `FAILURE_MODE`
   - Or k8s deployment from `tests/perf/fixtures/stress-mock.yaml`

4. **Worker Logs & Health CSV Access**
   - Worker process writing structured logs (for `derive_distribution`)
   - `channel_model_health` CSV export available (or Postgres access via `POSTGRES_*` env)

### Database Reset (Optional Explicit Command)

```bash
# Only run when explicitly requested — NOT automatic
python -c "
import psycopg2
conn = psycopg2.connect(host='...', dbname='...', user='...', password='...')
with conn.cursor() as cur:
    cur.execute('TRUNCATE channel_model_health RESTART IDENTITY;')
    # Insert baseline channels per model
conn.commit()
"
```

**Do not** destroy test data automatically. The orchestrator treats DB reset as
an explicit opt-in operation to preserve forensic state after failures.

## Artifacts Produced

Per scenario (in `--report-dir/scenario_N/`):
- `summary.json` — k6 handleSummary output
- `assertion-report.md` — human-readable assertion results
- Worker log snapshot (if `--worker-log-source` provided)
- Health CSV snapshot (if `--health-csv-source` provided)

Aggregate:
- `matrix-summary.json` — machine-readable summary with all 8 result statuses
  and the scenario→assertion mapping table

## Exit Codes

- `0`: All selected scenarios passed (k6 exit 0 + assertions exit 0)
- `1`: One or more scenarios failed (check `matrix-summary.json` for details)

The orchestrator runs all selected scenarios **sequentially** and collects
results before returning. It does **not** silently convert assertion failures
to success.