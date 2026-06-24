import { describe, it, expect } from 'vitest';
import { computeBackoffDelay, RECONNECT_DEFAULTS } from '../src/utils/backoff.js';

describe('computeBackoffDelay', () => {
  // ── pure (no jitter) ──

  it('computes exponential growth with jitter disabled', () => {
    // min=1000, factor=2: 1000, 2000, 4000, 8000, 16000, 30000(cap)
    expect(computeBackoffDelay(0, { min: 1000, max: 30000, factor: 2, jitter: false })).toBe(1000);
    expect(computeBackoffDelay(1, { min: 1000, max: 30000, factor: 2, jitter: false })).toBe(2000);
    expect(computeBackoffDelay(2, { min: 1000, max: 30000, factor: 2, jitter: false })).toBe(4000);
    expect(computeBackoffDelay(3, { min: 1000, max: 30000, factor: 2, jitter: false })).toBe(8000);
    expect(computeBackoffDelay(4, { min: 1000, max: 30000, factor: 2, jitter: false })).toBe(16000);
  });

  it('caps the delay at max', () => {
    // 1000 * 2^5 = 32000 > 30000 cap
    expect(computeBackoffDelay(5, { min: 1000, max: 30000, factor: 2, jitter: false })).toBe(30000);
    // stays at cap for larger attempts
    expect(computeBackoffDelay(10, { min: 1000, max: 30000, factor: 2, jitter: false })).toBe(30000);
    expect(computeBackoffDelay(100, { min: 1000, max: 30000, factor: 2, jitter: false })).toBe(30000);
  });

  it('respects a custom factor', () => {
    // factor=1.5: 1000, 1500, 2250, 3375, 5062.5 -> 5063
    expect(computeBackoffDelay(0, { min: 1000, max: 30000, factor: 1.5, jitter: false })).toBe(1000);
    expect(computeBackoffDelay(1, { min: 1000, max: 30000, factor: 1.5, jitter: false })).toBe(1500);
    expect(computeBackoffDelay(2, { min: 1000, max: 30000, factor: 1.5, jitter: false })).toBe(2250);
    expect(computeBackoffDelay(4, { min: 1000, max: 30000, factor: 1.5, jitter: false })).toBe(5063);
  });

  it('respects custom min and max', () => {
    expect(computeBackoffDelay(0, { min: 500, max: 5000, factor: 2, jitter: false })).toBe(500);
    expect(computeBackoffDelay(3, { min: 500, max: 5000, factor: 2, jitter: false })).toBe(4000);
    expect(computeBackoffDelay(4, { min: 500, max: 5000, factor: 2, jitter: false })).toBe(5000);
  });

  it('treats negative attempt as 0', () => {
    expect(computeBackoffDelay(-1, { jitter: false })).toBe(1000);
    expect(computeBackoffDelay(-100, { jitter: false })).toBe(1000);
  });

  it('floors non-integer attempts', () => {
    // attempt 2.9 -> floor to 2 -> 4000
    expect(computeBackoffDelay(2.9, { min: 1000, max: 30000, factor: 2, jitter: false })).toBe(4000);
  });

  it('uses defaults when opts omitted', () => {
    // defaults: min=1000, max=30000, factor=2
    expect(computeBackoffDelay(2, { jitter: false })).toBe(4000);
  });

  it('works with empty opts (defaults + jitter)', () => {
    // with jitter, base for attempt 2 is 4000; result must be in [2000, 4000]
    for (let i = 0; i < 50; i++) {
      const d = computeBackoffDelay(2);
      expect(d).toBeGreaterThanOrEqual(2000);
      expect(d).toBeLessThanOrEqual(4000);
    }
  });

  // ── jitter ──

  it('with jitter stays within 50%–100% of the base delay', () => {
    // base for attempt 3 (min=1000, factor=2) = 8000; jitter range [4000, 8000]
    for (let i = 0; i < 100; i++) {
      const d = computeBackoffDelay(3, { min: 1000, max: 30000, factor: 2, jitter: true });
      expect(d).toBeGreaterThanOrEqual(4000);
      expect(d).toBeLessThanOrEqual(8000);
    }
  });

  it('with jitter never exceeds the cap', () => {
    // attempt 10 -> base capped at 30000; jitter range [15000, 30000]
    for (let i = 0; i < 100; i++) {
      const d = computeBackoffDelay(10);
      expect(d).toBeGreaterThanOrEqual(15000);
      expect(d).toBeLessThanOrEqual(30000);
    }
  });

  it('returns an integer', () => {
    expect(Number.isInteger(computeBackoffDelay(3, { jitter: false }))).toBe(true);
    expect(Number.isInteger(computeBackoffDelay(3, { jitter: true }))).toBe(true);
  });
});

describe('RECONNECT_DEFAULTS', () => {
  it('has the expected defaults', () => {
    expect(RECONNECT_DEFAULTS).toEqual({ min: 1000, max: 30000, factor: 2, jitter: true });
  });
});
