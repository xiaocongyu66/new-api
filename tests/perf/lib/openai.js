/**
 * k6 utility helpers for OpenAI-compatible APIs.
 * Pure JS module (no k6 imports) so `node --check` passes.
 */

/**
 * Build request headers with Bearer auth.
 * @param {string} apiKey
 * @returns {Record<string, string>}
 */
export function makeHeaders(apiKey) {
  return {
    'Content-Type': 'application/json',
    'Authorization': `Bearer ${apiKey}`,
  };
}

/**
 * Build chat/completions request payload.
 * @param {string} model
 * @param {number} maxTokens
 * @param {boolean} stream
 * @returns {object}
 */
export function chatPayload(model, maxTokens, stream) {
  return {
    model,
    messages: [
      { role: 'user', content: 'Hello' }
    ],
    max_tokens: maxTokens,
    stream,
  };
}

/**
 * Count SSE "data:" lines in a response body, excluding "[DONE]".
 * @param {string} body
 * @returns {number}
 */
export function countSseChunks(body) {
  if (!body) return 0;
  const lines = body.split('\n');
  let count = 0;
  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed.startsWith('data:')) {
      const data = trimmed.slice(5).trim();
      if (data !== '[DONE]') {
        count++;
      }
    }
  }
  return count;
}