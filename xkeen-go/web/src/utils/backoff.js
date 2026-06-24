/**
 * Exponential-backoff reconnect delay with optional jitter.
 *
 * Used by the WebSocket services (logs, metrics) to avoid hammering a
 * struggling backend. Delay grows exponentially per failed attempt up to a
 * cap, with random jitter (50–100% of the base) to decorrelate reconnects.
 *
 * @param {number} attempt - reconnect attempt number, 0-based (0 = first retry)
 * @param {object} [opts]
 * @param {number} [opts.min=1000]  - initial delay in ms (attempt 0)
 * @param {number} [opts.max=30000] - maximum delay in ms (cap)
 * @param {number} [opts.factor=2]  - backoff multiplier per attempt
 * @param {boolean} [opts.jitter=true] - apply random jitter (50–100% of base)
 * @returns {number} delay in ms
 */
export function computeBackoffDelay(attempt, opts = {}) {
  const { min = 1000, max = 30000, factor = 2, jitter = true } = opts;
  const a = attempt < 0 ? 0 : Math.floor(attempt);
  const base = Math.min(max, min * Math.pow(factor, a));
  if (!jitter) return Math.round(base);
  // equal jitter: 50% deterministic + 50% random -> 50%–100% of base
  return Math.round(base * (0.5 + Math.random() * 0.5));
}

/** Default reconnect options used by the WS services. */
export const RECONNECT_DEFAULTS = { min: 1000, max: 30000, factor: 2, jitter: true };
