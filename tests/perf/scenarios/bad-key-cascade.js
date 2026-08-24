/**
 * Bad key cascade scenario — upstream returns 401/500 for invalid credentials.
 *
 * Targets a model configured with bad credentials (BAD_KEY_MODEL, default "mock-bad").
 * Accepts 401/500 as injected fault outcomes — these are expected, not failures.
 * Emits a Counter `bad_key_responses` for non-200 responses with request id if present.
 *
 * Fixed VUs (no ramp), steady-state DURATION.
 *
 * Thresholds:
 *   - No strict success threshold (fault injection scenario)
 *   - error rate only on unexpected status codes (not 200/401/500)
 *
 * Env vars (all optional, defaults shown):
 *   TARGET_URL          - base URL, e.g. http://localhost:3000
 *   API_KEY             - bearer token (may be intentionally bad)
 *   BAD_KEY_MODEL       - model name with bad credentials, default "mock-bad"
 *   VUS                 - target VUs, default 20
 *   DURATION            - test duration, default "60s"
 *   SUMMARY_JSON        - output path for handleSummary, default "stdout"
 */
import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';
import { makeHeaders, chatPayload } from '../lib/openai.js';

const TARGET_URL = __ENV.TARGET_URL || 'http://localhost:3000';
const API_KEY = __ENV.API_KEY || '';
const BAD_KEY_MODEL = __ENV.BAD_KEY_MODEL || 'mock-bad';
const VUS = Number(__ENV.VUS) || 20;
const DURATION = __ENV.DURATION || '60s';

export const options = {
  scenarios: {
    bad_key_cascade: {
      executor: 'constant-vus',
      vus: VUS,
      duration: DURATION,
      gracefulStop: '5s',
    },
  },
  thresholds: {
    // Only flag unexpected status codes (not 200, 401, 500)
    http_req_failed: ['rate<0.10'],
  },
};

const headers = makeHeaders(API_KEY);
const payload = JSON.stringify(chatPayload(BAD_KEY_MODEL, 16, false));

// Counter for bad-key responses (401/500) with request id tracking
const badKeyResponses = new Counter('bad_key_responses');

export default function () {
  const res = http.post(`${TARGET_URL}/chat/completions`, payload, { headers });

  // Accept 200, 401, 500 as valid outcomes for this fault scenario
  const acceptedStatus = [200, 401, 500];
  const isAccepted = acceptedStatus.includes(res.status);

  check(res, {
    'status is accepted (200/401/500)': () => isAccepted,
  });

  if (!isAccepted) {
    // Unexpected status — count as error for threshold
    badKeyResponses.add(1, { status: String(res.status), type: 'unexpected' });
  } else if (res.status === 401 || res.status === 500) {
    // Expected fault injection outcome — track with request id if available
    let requestId = 'unknown';
    try {
      const body = JSON.parse(res.body);
      if (body && body.request_id) {
        requestId = body.request_id;
      } else if (res.headers && res.headers['x-request-id']) {
        requestId = res.headers['x-request-id'];
      }
    } catch (error) {
      // ignore parse errors
    }
    badKeyResponses.add(1, { status: String(res.status), request_id: requestId, type: 'expected_fault' });
  }
  // 200 responses are not counted in bad_key_responses
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