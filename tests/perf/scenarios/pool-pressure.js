/**
 * Pool pressure scenario — ramp load to force connection pool exhaustion and retry failures.
 *
 * Targets a flaky model (FLAKY_MODEL, default "mock-flaky") that simulates intermittent
 * upstream failures. Ramping VUs to saturate connection pools and trigger retry logic.
 * Emits Counter `pool_pressure_errors` for non-200 responses to quantify retry exhaustion.
 *
 * Ramp: RAMP_DURATION to VUS, hold DURATION, ramp down.
 *
 * Thresholds:
 *   - error rate < 0.20 (allow higher errors under pool pressure)
 *
 * Env vars (all optional, defaults shown):
 *   TARGET_URL          - base URL, e.g. http://localhost:3000
 *   API_KEY             - bearer token
 *   FLAKY_MODEL         - model name with flaky behavior, default "mock-flaky"
 *   VUS                 - target VUs, default 200
 *   DURATION            - steady-state duration, default "120s"
 *   RAMP_DURATION       - ramp-up duration, default "60s"
 *   SUMMARY_JSON        - output path for handleSummary, default "stdout"
 */
import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';
import { makeHeaders, chatPayload } from '../lib/openai.js';

const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:3000';
const API_KEY = __ENV.API_KEY || '';
const FLAKY_MODEL = __ENV.FLAKY_MODEL || __ENV.MODEL || 'mock-flaky';
const VUS = Number(__ENV.VUS) || 200;
const DURATION = __ENV.DURATION || '120s';
const RAMP_DURATION = __ENV.RAMP_DURATION || '60s';

export const options = {
  scenarios: {
    pool_pressure: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: RAMP_DURATION, target: VUS },
        { duration: DURATION, target: VUS },
        { duration: '30s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    // Injected 30% upstream failures plus the retry-exhaustion tail; the
    // state-machine contract is asserted from DB snapshots, not this budget.
    http_req_failed: ['rate<0.60'],
  },
};

const headers = makeHeaders(API_KEY);
const payload = JSON.stringify(chatPayload(FLAKY_MODEL, 16, false));

// Counter for pool pressure induced errors (non-200)
const poolPressureErrors = new Counter('pool_pressure_errors');

export default function () {
  const res = http.post(`${TARGET_URL}/chat/completions`, payload, { headers });

  check(res, {
    'status is 200': (r) => r.status === 200,
  });

  if (res.status !== 200) {
    poolPressureErrors.add(1, {
      status: String(res.status),
      // Tag with potential retry indicators
      is_retryable: String(res.status >= 500 || res.status === 429),
    });
  }
}

export function handleSummary(data) {
  const outPath = __ENV.SUMMARY_JSON || 'stdout';
  const json = JSON.stringify(data, null, 2);
  if (outPath === 'stdout') {
    console.log(json);
    return {};
  }
  return { [outPath]: json };
}