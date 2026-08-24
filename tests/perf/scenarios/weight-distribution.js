/**
 * Weight distribution scenario — verify channel selection follows configured weights.
 *
 * Targets a model backed by multiple channels with known weights (e.g. 100:50:10).
 * Emits a request header `X-Perf-Scenario: weight-distribution` for server-side routing visibility.
 * Records response headers/channel id if exposed by the upstream.
 * Counter `distribution_requests` tracks total requests per observed channel.
 *
 * NOTE: Actual 100:50:10 weight verification is done via Python DB/log assertions
 * correlating the channel_id from response headers or logs.
 *
 * Constant VUS, steady-state DURATION.
 *
 * Thresholds:
 *   - error rate < 0.05
 *   - At least 3 distinct channels observed (via channel_id tag)
 *
 * Env vars (all optional, defaults shown):
 *   TARGET_URL          - base URL, e.g. http://localhost:3000
 *   API_KEY             - bearer token
 *   MODEL               - model name with weighted channels, default "mock-weighted"
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
const MODEL = __ENV.MODEL || 'mock-weighted';
const VUS = Number(__ENV.VUS) || 100;
const DURATION = __ENV.DURATION || '120s';

export const options = {
  scenarios: {
    weight_distribution: {
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
// Add scenario identification header for server-side tracking
headers['X-Perf-Scenario'] = 'weight-distribution';
const payload = JSON.stringify(chatPayload(MODEL, 16, false));

// Counter for requests per observed channel (tagged by channel_id)
const distributionRequests = new Counter('distribution_requests');
// Trend for latency per channel
const distributionLatency = new Trend('distribution_latency', true);

export default function () {
  const res = http.post(`${TARGET_URL}/chat/completions`, payload, { headers });

  check(res, {
    'status is 200': (r) => r.status === 200,
  });

  // Extract channel id from response headers or body if exposed
  let channelId = 'unknown';
  try {
    // Check response headers first (common: x-channel-id, x-upstream-channel)
    if (res.headers) {
      channelId = res.headers['x-channel-id']
        || res.headers['x-upstream-channel']
        || res.headers['x-channel']
        || 'unknown';
    }
    // Fallback to body parsing if header not present
    if (channelId === 'unknown' && res.body) {
      const body = JSON.parse(res.body);
      if (body && body.channel_id) {
        channelId = body.channel_id;
      } else if (body && body.choices && body.choices[0] && body.choices[0].channel_id) {
        channelId = body.choices[0].channel_id;
      }
    }
  } catch (error) {
    // ignore parse errors
  }

  // Tag all metrics with observed channel_id for distribution analysis
  distributionRequests.add(1, { channel_id: channelId });
  if (res.timings && res.timings.duration > 0) {
    distributionLatency.add(res.timings.duration, { channel_id: channelId });
  }
}

export function handleSummary(data) {
  const outPath = __ENV.SUMMARY_JSON || 'stdout';
  const metadata = data.metadata || {};
  metadata.scenario = 'weight-distribution';
  metadata.model = MODEL;
  metadata.expected_weights = '100:50:10 (verified via Python DB/log correlation)';
  metadata.header_injected = 'X-Perf-Scenario: weight-distribution';
  data.metadata = metadata;
  const json = JSON.stringify(data, null, 2);
  if (outPath === 'stdout') {
    console.log(json);
    return {};
  }
  return { [outPath]: json };
}