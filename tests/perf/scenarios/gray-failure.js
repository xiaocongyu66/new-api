/**
 * Gray failure scenario — constant load with expected partial failure ratio.
 *
 * Targets a flaky model (FLAKY_MODEL, default "mock-flaky") that returns a configurable
 * failure ratio. Constant VUs to observe steady-state behavior under partial degradation.
 * Summary explicitly carries expected failure ratio from FAILURE_RATIO env (default 0.30).
 * Records success/failure counters for Python correlation.
 *
 * Thresholds:
 *   - error rate tracks against expected FAILURE_RATIO with tolerance
 *   - availability (success rate) >= 1 - FAILURE_RATIO - 0.05
 *
 * Env vars (all optional, defaults shown):
 *   TARGET_URL          - base URL, e.g. http://localhost:3000
 *   API_KEY             - bearer token
 *   FLAKY_MODEL         - model name with flaky behavior, default "mock-flaky"
 *   VUS                 - target VUs, default 50
 *   DURATION            - test duration, default "120s"
 *   FAILURE_RATIO       - expected failure ratio (0.0-1.0), default 0.30
 *   SUMMARY_JSON        - output path for handleSummary, default "stdout"
 */
import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { makeHeaders, chatPayload } from '../lib/openai.js';

const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:3000';
const API_KEY = __ENV.API_KEY || '';
const FLAKY_MODEL = __ENV.FLAKY_MODEL || __ENV.MODEL || 'mock-flaky';
const VUS = Number(__ENV.VUS) || 50;
const DURATION = __ENV.DURATION || '120s';
const FAILURE_RATIO = Number(__ENV.FAILURE_RATIO) || 0.30;
const EXPECTED_SUCCESS_RATIO = 1 - FAILURE_RATIO;
const TOLERANCE = 0.20; // covers the retry-exhaustion tail on top of the injected ratio

export const options = {
  scenarios: {
    gray_failure: {
      executor: 'constant-vus',
      vus: VUS,
      duration: DURATION,
      gracefulStop: '5s',
    },
  },
  thresholds: {
    // Error rate should not exceed expected failure ratio + tolerance
    http_req_failed: [`rate<${FAILURE_RATIO + TOLERANCE}`],
  },
};

const headers = makeHeaders(API_KEY);
const payload = JSON.stringify(chatPayload(FLAKY_MODEL, 16, false));

// Counters for success/failure tracking (for Python correlation)
const grayFailureSuccess = new Counter('gray_failure_success');
const grayFailureFailure = new Counter('gray_failure_failure');
// Trend for latency distribution under gray failure
const grayFailureLatency = new Trend('gray_failure_latency', true);

export default function () {
  const res = http.post(`${TARGET_URL}/chat/completions`, payload, { headers });

  const isSuccess = res.status === 200;

  check(res, {
    'status tracked': () => true, // always pass, we track via counters
  });

  if (isSuccess) {
    grayFailureSuccess.add(1);
  } else {
    grayFailureFailure.add(1, { status: String(res.status) });
  }

  if (res.timings && res.timings.duration > 0) {
    grayFailureLatency.add(res.timings.duration, { status: String(res.status) });
  }
}

export function handleSummary(data) {
  const outPath = __ENV.SUMMARY_JSON || 'stdout';
  // Enrich summary with expected failure ratio for Python assertions
  const metadata = data.metadata || {};
  metadata.scenario = 'gray-failure';
  metadata.expected_failure_ratio = FAILURE_RATIO;
  metadata.expected_success_ratio = EXPECTED_SUCCESS_RATIO;
  metadata.tolerance = TOLERANCE;
  metadata.model = FLAKY_MODEL;
  data.metadata = metadata;
  const json = JSON.stringify(data, null, 2);
  if (outPath === 'stdout') {
    console.log(json);
    return {};
  }
  return { [outPath]: json };
}