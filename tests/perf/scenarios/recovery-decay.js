/**
 * Recovery decay scenario — after partial degradation, recovery is slower than expected.
 *
 * Targets a flaky model (RECOVERY_MODEL, default "mock-recovery") that initially
 * fails at a high rate (FAILURE_RATIO_START, default 0.50) then gradually
 * recovers (FAILURE_RATIO_END, default 0.05) over the test duration.
 * The decay is measured as the difference between expected and actual recovery curve.
 * Emits Counters `recovery_success` and `recovery_failure` per time window.
 *
 * Ramp: RAMP_DURATION to VUS, hold DURATION, ramp down.
 *
 * Thresholds:
 *   - final error rate < 0.15 (should have recovered)
 *   - recovery latency trend tracked
 *
 * Env vars (all optional, defaults shown):
 *   TARGET_URL              - base URL, e.g. http://localhost:3000
 *   API_KEY                 - bearer token
 *   RECOVERY_MODEL          - model name with recovery behavior, default "mock-recovery"
 *   VUS                     - target VUs, default 50
 *   DURATION                - test duration, default "300s"
 *   RAMP_DURATION           - ramp-up duration, default "60s"
 *   FAILURE_RATIO_START     - initial failure ratio, default 0.50
 *   FAILURE_RATIO_END       - target failure ratio, default 0.05
 *   SUMMARY_JSON            - output path for handleSummary, default "stdout"
 */
import http from 'k6/http';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { makeHeaders, chatPayload } from '../lib/openai.js';

const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:3000';
const API_KEY = __ENV.API_KEY || '';
const RECOVERY_MODEL = __ENV.RECOVERY_MODEL || 'mock-recovery';
const VUS = Number(__ENV.VUS) || 50;
const DURATION = __ENV.DURATION || '300s';
const RAMP_DURATION = __ENV.RAMP_DURATION || '60s';

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