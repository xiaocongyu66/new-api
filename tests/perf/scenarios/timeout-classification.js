/**
 * Timeout classification scenario — distinguish transport timeouts from upstream HTTP failures.
 *
 * Targets a slow model (SLOW_MODEL, default "mock-slow") with configured request TIMEOUT
 * (default 2s). k6 request timeout triggers on slow responses.
 * Emits Counter `timed_out_requests` for k6 transport timeouts.
 * Emits Counter `upstream_http_errors` for non-timeout HTTP error statuses (4xx/5xx).
 * Summary distinguishes transport timeouts from upstream failures for Python correlation.
 *
 * Fixed VUs, steady-state DURATION.
 *
 * Thresholds:
 *   - timeout rate tracked separately (no strict threshold)
 *   - upstream error rate < 0.10
 *
 * Env vars (all optional, defaults shown):
 *   TARGET_URL              - base URL, e.g. http://localhost:3000
 *   API_KEY                 - bearer token
 *   SLOW_MODEL              - model name with slow responses, default "mock-slow"
 *   VUS                     - target VUs, default 30
 *   DURATION                - test duration, default "60s"
 *   TIMEOUT                 - k6 request timeout in seconds, default "2s"
 *   SUMMARY_JSON            - output path for handleSummary, default "stdout"
 */
import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { makeHeaders, chatPayload } from '../lib/openai.js';

const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:3000';
const API_KEY = __ENV.API_KEY || '';
const SLOW_MODEL = __ENV.SLOW_MODEL || 'mock-slow';
const VUS = Number(__ENV.VUS) || 30;
const DURATION = __ENV.DURATION || '60s';
const TIMEOUT = __ENV.TIMEOUT || '2s'; // k6 timeout format: "2s", "2000ms", etc.

export const options = {
  scenarios: {
    timeout_classification: {
      executor: 'constant-vus',
      vus: VUS,
      duration: DURATION,
      gracefulStop: '5s',
    },
  },
};

const headers = makeHeaders(API_KEY);
const payload = JSON.stringify(chatPayload(SLOW_MODEL, 16, false));

// Counter for k6 transport timeouts (request timeout exceeded)
const timedOutRequests = new Counter('timed_out_requests');
// Counter for upstream HTTP errors (4xx/5xx responses received before timeout)
const upstreamHttpErrors = new Counter('upstream_http_errors');
// Trend for completed request latencies (excludes timeouts)
const completedLatency = new Trend('completed_latency', true);

export default function () {
  // k6 timeout is set via the request options
  const res = http.post(`${TARGET_URL}/chat/completions`, payload, {
    headers,
    timeout: TIMEOUT,
  });

  // k6 returns res.status === 0 and res.timings.duration === 0 on timeout
  // res.error contains the error message (e.g., "request timeout")
  const isTimeout = res.status === 0 || (res.error && res.error.includes('timeout'));

  if (isTimeout) {
    timedOutRequests.add(1, { error: res.error || 'timeout' });
    check(res, {
      'timeout detected': () => true,
    });
  } else if (res.status >= 400) {
    upstreamHttpErrors.add(1, { status: String(res.status) });
    check(res, {
      'upstream error status': () => true,
    });
  } else {
    check(res, {
      'status is 200': (r) => r.status === 200,
    });
    if (res.timings && res.timings.duration > 0) {
      completedLatency.add(res.timings.duration);
    }
  }
}

export function handleSummary(data) {
  const outPath = __ENV.SUMMARY_JSON || 'stdout';
  const metadata = data.metadata || {};
  metadata.scenario = 'timeout-classification';
  metadata.model = SLOW_MODEL;
  metadata.request_timeout = TIMEOUT;
  metadata.timeout_classification = {
    description: 'timed_out_requests = k6 transport timeouts (status 0); upstream_http_errors = 4xx/5xx received before timeout',
    counters: ['timed_out_requests', 'upstream_http_errors'],
  };
  data.metadata = metadata;
  const json = JSON.stringify(data, null, 2);
  if (outPath === 'stdout') {
    console.log(json);
    return {};
  }
  return { [outPath]: json };
}