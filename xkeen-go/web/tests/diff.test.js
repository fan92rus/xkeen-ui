import { describe, it, expect } from 'vitest';
import { computeDiff } from '../src/utils/diff.js';

describe('computeDiff', () => {
  it('marks identical text as all-equal (no spans)', () => {
    const out = computeDiff('a\nb\nc', 'a\nb\nc');
    expect(out).not.toContain('diff-removed');
    expect(out).not.toContain('diff-added');
    expect(out.split('\n')).toHaveLength(3);
  });

  it('marks removed lines (in a, not in b)', () => {
    const out = computeDiff('x\ny\nz', 'x\nz');
    expect(out).toContain('diff-removed');
    expect(out).toContain('y');
    expect(out).not.toContain('diff-added');
  });

  it('marks added lines (in b, not in a)', () => {
    const out = computeDiff('x\nz', 'x\ny\nz');
    expect(out).toContain('diff-added');
    expect(out).toContain('y');
    expect(out).not.toContain('diff-removed');
  });

  it('escapes HTML to prevent injection in the diff modal', () => {
    const out = computeDiff('<script>alert(1)</script>', 'safe');
    expect(out).toContain('&lt;script&gt;');
    expect(out).not.toContain('<script>');
  });

  it('caps comparison at 500 lines per side', () => {
    const big = Array.from({ length: 1000 }, (_, i) => `line${i}`).join('\n');
    const out = computeDiff(big, '');
    // 500 removed lines from `a`; `b` contributes nothing.
    const removed = out.split('\n').filter(l => l.includes('diff-removed'));
    expect(removed).toHaveLength(500);
  });

  it('handles empty inputs without throwing', () => {
    expect(() => computeDiff('', '')).not.toThrow();
    expect(computeDiff('', '')).toBe('  ');
    expect(computeDiff('a', '')).toContain('diff-removed');
    expect(computeDiff('', 'a')).toContain('diff-added');
  });

  it('handles null/undefined inputs defensively', () => {
    expect(() => computeDiff(null, undefined)).not.toThrow();
  });

  it('produces joined newline-separated output', () => {
    const out = computeDiff('a\nb', 'a\nb');
    expect(out).toBe('  a\n  b');
  });
});
