/**
 * CAS contention scenario — concurrent updates to channel health cause CAS retries.
 *
 * Targets a model backed by multiple channels (CAS_MODEL, default "mock-cas")
 * where rapid health updates trigger compare-and-swap contention on the
 * channel_model_health table. High VU count with short requests maximizes
 * contention on the health update path.
 * Emits Counter `cas_conflicts` for observed retryable conflict responses.
 *
 * Fixed VUS (no ramp), steady-state DURATION.
 *
 * Thresholds:
 *   - error rate < 0.10
 *   - CAS conflict rate tracked separately
 *
 * Env vars (all optional, defaults shown):
 *   TARGET_URL          - base URL, e.g. http://localhost:3000
 *   API_KEY             - bearer token
 *   CAS_MODEL           - model name with CAS contention, default "mock-cas"
 *   VUS                 - target VUs, default 100
 *   DURATION            - test duration, default "120s"
 *   SUMMARY_JSON        - output path for handleSummary, default "stdout"
 */
import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { makeHeaders, chatPayload } from '../lib/openai.js';

const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:3000';
const API_KEY = __ENV.API_KEY || '';
const CAS_MODEL = __ENV.CAS_MODEL || 'mock-cas';
const VUS = Number(__ENV.VUS) || 100;
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
    http_req_failed: ['rate<0.10'],
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