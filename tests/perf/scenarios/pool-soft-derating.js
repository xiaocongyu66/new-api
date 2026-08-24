/**
 * Pool soft derating scenario — sustained load causes gradual pool derating.
 *
 * Targets a model (DERATING_MODEL, default "mock-derating") that simulates
 * connection pool soft limits being approached. The upstream gradually increases
 * latency and error rate as pool utilization rises.
 * Emits Counter `derating_events` for observed derating indicators.
 *
 * Ramp: RAMP_DURATION to VUS, hold DURATION, ramp down.
 *
 * Thresholds:
 *   - error rate < 0.15 (allow elevated errors during derating)
 *   - p99 latency < 10000ms
 *
 * Env vars (all optional, defaults shown):
 *   TARGET_URL          - base URL, e.g. http://localhost:3000
 *   API_KEY             - bearer token
 *   DERATING_MODEL      - model name with derating behavior, default "mock-derating"
 *   VUS                 - target VUs, default 150
 *   DURATION            - steady-state duration, default "180s"
 *   RAMP_DURATION       - ramp-up duration, default "60s"
 *   SUMMARY_JSON        - output path for handleSummary, default "stdout"
 */
import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { makeHeaders, chatPayload } from '../lib/openai.js';

const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:3000';
const API_KEY = __ENV.API_KEY || '';
const DERATING_MODEL = __ENV.DERATING_MODEL || 'mock-derating';
const VUS = Number(__ENV.VUS) || 150;
const DURATION = __ENV.DURATION || '180s';
const RAMP_DURATION = __ENV.RAMP_DURATION || '60s';

export const options = {
  scenarios: {
    pool_derating: {
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
    http_req_failed: ['rate<0.15'],
    http_req_duration: ['p(99)<10000'],
  },
};

const headers = makeHeaders(API_KEY);
const payload = JSON.stringify(chatPayload(DERATING_MODEL, 16, false));

const deratingEvents = new Counter('derating_events');
const deratingLatency = new Trend('derating_latency', true);

export default function () {
  const start = Date.now();
  const res = http.post(`${TARGET_URL}/chat/completions`, payload, { headers });
  const latency = Date.now() - start;

  deratingLatency.add(latency);

  const isSuccess = res.status === 200;
  check(res, { 'status is 200': () => isSuccess });

  // Track derating indicators: increasing latency, 503, 429
  if (res.status === 503 || res.status === 429 || latency > 5000) {
    deratingEvents.add(1, {
      status: String(res.status),
      latency_bucket: String(Math.floor(latency / 1000) * 1000),
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