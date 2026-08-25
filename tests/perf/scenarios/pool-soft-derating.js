/**
 * #392 scenario 1 — pool pressure, soft derating, and emergency recovery.
 *
 * The prepared topology supplies several mock-flaky routing units. The Python
 * assertion reads route isolation/emergency recovery events and health rows.
 */
import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { makeHeaders, chatPayload } from '../lib/openai.js';

const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:3000';
const API_KEY = __ENV.API_KEY || '';
const DERATING_MODEL = __ENV.DERATING_MODEL || __ENV.MODEL || 'mock-flaky';
const VUS = Number(__ENV.VUS) || 50;
const DURATION = __ENV.DURATION || '120s';
const RAMP_DURATION = __ENV.RAMP_DURATION || '30s';

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