import { describe, it, expect } from 'vitest';
import { formatBackupTime } from '../src/utils/format.js';

describe('formatBackupTime', () => {
  it('returns em-dash for null', () => {
    expect(formatBackupTime(null)).toBe('—');
  });

  it('returns em-dash for undefined', () => {
    expect(formatBackupTime(undefined)).toBe('—');
  });

  it('returns em-dash for NaN', () => {
    expect(formatBackupTime(NaN)).toBe('—');
  });

  it('returns a string for a valid timestamp', () => {
    const result = formatBackupTime(1719000000);
    expect(result).toBeTypeOf('string');
    expect(result.length).toBeGreaterThan(0);
    expect(result).not.toBe('—');
  });

  it('converts unix seconds correctly', () => {
    // 2024-06-21T12:00:00Z
    const ts = 1718971200;
    const result = formatBackupTime(ts);
    // Should contain date parts in the locale format
    expect(result).toContain('2024');
  });

  it('handles zero timestamp as a valid Unix epoch', () => {
    const result = formatBackupTime(0);
    expect(result).toBeTypeOf('string');
    expect(result).not.toBe('—');
  });

  it('falsy values other than 0 return em-dash', () => {
    expect(formatBackupTime('')).toBe('—');
  });
});
