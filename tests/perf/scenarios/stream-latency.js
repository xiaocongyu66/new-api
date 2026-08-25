/**
 * Stream latency scenario — streaming response.
 *
 * Uses k6's http_req_waiting as TTFT approximation (time to first byte).
 * ITL (inter-token latency) not measured — k6 buffers body, no chunk timestamps.
 * For precise TTFT/ITL, use scripts/stress_test.py.
 *
 * Constant VUS load (no ramp stages needed; steady stream).
 * Each VU runs a streaming POST /chat/completions with max_tokens=64.
 *
 * Thresholds:
 *   - http_req_failed rate < 0.05
 *
 * Env vars (all optional, defaults shown):
 *   TARGET_URL      - base URL, e.g. http://localhost:3000
 *   API_KEY         - bearer token
 *   MODEL           - model name, default "mock-fast"
 *   VUS             - target VUs, default 50
 *   DURATION        - test duration, default "60s"
 *   SUMMARY_JSON    - output path for handleSummary, default "stdout"
 */
import http from 'k6/http';
import { check } from 'k6';
import { Trend } from 'k6/metrics';
import { makeHeaders, chatPayload, countSseChunks } from '../lib/openai.js';

const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:3000';
const API_KEY = __ENV.API_KEY || '';
const MODEL = __ENV.MODEL || 'mock-fast';
const VUS = Number(__ENV.VUS) || 50;
const DURATION = __ENV.DURATION || '60s';

// Custom trend for TTFT (approximated by http_req_waiting)
const ttft = new Trend('ttft', true);

export const options = {
  scenarios: {
    stream_latency: {
      executor: 'constant-vus',
      vus: VUS,
      duration: DURATION,
      gracefulStop: '5s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.05'],
  },
};

const headers = makeHeaders(API_KEY);
const payload = JSON.stringify(chatPayload(MODEL, 64, true));

// Chunks per response, for streaming throughput characterization.
const chunks = new Trend('sse_chunks', true);

export default function () {
  const res = http.post(`${TARGET_URL}/chat/completions`, payload, { headers });
  check(res, {
    'status is 200': (r) => r.status === 200,
  });

  // Record TTFT approximation from http_req_waiting (tagged by k6)
  // k6 automatically adds http_req_waiting per request; we mirror it into a named trend for summary clarity.
  if (res.timings && res.timings.waiting > 0) {
    ttft.add(res.timings.waiting, { scenario: 'stream' });
  }
  chunks.add(countSseChunks(res.body));
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