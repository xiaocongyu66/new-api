/**
 * #392 scenario 4 — recovery and decay after partial degradation.
 *
 * mock-flaky failures create health rows; subsequent successful requests
 * exercise RecordSuccess/expiry decay. Python checks calm/dormant -> healthy.
 */
import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { makeHeaders, chatPayload } from '../lib/openai.js';

const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:3000';
const API_KEY = __ENV.API_KEY || '';
const RECOVERY_MODEL = __ENV.RECOVERY_MODEL || __ENV.MODEL || 'mock-recover';
const VUS = Number(__ENV.VUS) || 30;
const DURATION = __ENV.DURATION || '120s';
const RAMP_DURATION = __ENV.RAMP_DURATION || '30s';

export const options = {
  scenarios: {
    recovery_decay: {
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
  },
};

const headers = makeHeaders(API_KEY);
const payload = JSON.stringify(chatPayload(RECOVERY_MODEL, 16, false));

const recoverySuccess = new Counter('recovery_success');
const recoveryFailure = new Counter('recovery_failure');
const recoveryLatency = new Trend('recovery_latency', true);

export default function () {
  const start = Date.now();
  const res = http.post(`${TARGET_URL}/chat/completions`, payload, { headers });
  const latency = Date.now() - start;

  recoveryLatency.add(latency);

  const isSuccess = res.status === 200;
  check(res, { 'status is 200': () => isSuccess });

  if (isSuccess) {
    recoverySuccess.add(1);
  } else {
    recoveryFailure.add(1, { status: String(res.status) });
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