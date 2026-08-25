/**
 * #392 scenario 2 — concurrent CAS updates on the same model routing units.
 *
 * The prepared mock-flaky pool gives every 50 VUs retryable failures; Python
 * checks health versions and worker CAS exhaustion logs after the run.
 */
import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { makeHeaders, chatPayload } from '../lib/openai.js';

const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:3000';
const API_KEY = __ENV.API_KEY || '';
const CAS_MODEL = __ENV.CAS_MODEL || 'mock-flaky';
const VUS = Number(__ENV.VUS) || 50;
const DURATION = __ENV.DURATION || '120s';

export const options = {
  scenarios: {
    cas_contention: {
      executor: 'constant-vus',
      vus: VUS,
      duration: DURATION,
      gracefulStop: '5s',
    },
  },
  thresholds: {
    // Injected upstream failures dominate the error rate; CAS correctness is
    // asserted from version growth and conflict logs in DB snapshots.
    http_req_failed: ['rate<0.60'],
  },
};

const headers = makeHeaders(API_KEY);
const payload = JSON.stringify(chatPayload(CAS_MODEL, 8, false));

const casConflicts = new Counter('cas_conflicts');
const casLatency = new Trend('cas_latency', true);

export default function () {
  const start = Date.now();
  const res = http.post(`${TARGET_URL}/chat/completions`, payload, { headers });
  const latency = Date.now() - start;

  casLatency.add(latency);

  const isAccepted = [200, 409, 503].includes(res.status);
  check(res, { 'status is accepted (200/409/503)': () => isAccepted });

  // 409 Conflict or 503 with retry-after indicates CAS contention
  if (res.status === 409 || (res.status === 503 && res.headers['retry-after'])) {
    casConflicts.add(1, {
      status: String(res.status),
      retry_after: res.headers['retry-after'] || 'none',
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