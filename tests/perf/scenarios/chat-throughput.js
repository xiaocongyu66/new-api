/**
 * Chat throughput scenario — non-streaming.
 *
 * Ramp: RAMP_DURATION (default 30s) to VUS (default 100), hold DURATION (default 120s), then ramp down.
 * Each VU runs a non-streaming POST /chat/completions with max_tokens=16.
 *
 * Thresholds:
 *   - http_req_failed rate < 0.05 (5% error budget)
 *   - http_req_duration p(95) < P95_MS (default 30000ms, mock upstream is slow)
 *
 * Env vars (all optional, defaults shown):
 *   TARGET_URL      - base URL, e.g. http://localhost:3000
 *   API_KEY         - bearer token
 *   MODEL           - model name, default "mock-fast"
 *   VUS             - target VUs, default 100
 *   DURATION        - steady-state duration, default "120s"
 *   RAMP_DURATION   - ramp-up duration, default "30s"
 *   P95_MS          - p95 latency budget in ms, default 30000
 *   SUMMARY_JSON    - output path for handleSummary, default "stdout"
 */
import http from 'k6/http';
import { check, sleep } from 'k6';
import { makeHeaders, chatPayload } from '../lib/openai.js';

const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:3000';
const API_KEY = __ENV.API_KEY || '';
const MODEL = __ENV.MODEL || 'mock-fast';
const VUS = Number(__ENV.VUS) || 100;
const DURATION = __ENV.DURATION || '120s';
const RAMP_DURATION = __ENV.RAMP_DURATION || '30s';
const P95_MS = Number(__ENV.P95_MS) || 30000;

export const options = {
  scenarios: {
    chat_throughput: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: RAMP_DURATION, target: VUS },
        { duration: DURATION, target: VUS },
        { duration: '10s', target: 0 },
      ],
      gracefulRampDown: '5s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.05'],
    http_req_duration: [`p(95)<${P95_MS}`],
  },
};

const headers = makeHeaders(API_KEY);
const payload = JSON.stringify(chatPayload(MODEL, 16, false));

export default function () {
  const res = http.post(`${TARGET_URL}/chat/completions`, payload, { headers });
  check(res, {
    'status is 200': (r) => r.status === 200,
    'has choices': (r) => {
      try {
        const body = JSON.parse(r.body);
        return Array.isArray(body.choices) && body.choices.length > 0;
      } catch {
        return false;
      }
    },
  });
  sleep(0.1);
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