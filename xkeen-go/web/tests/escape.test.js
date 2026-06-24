import { describe, it, expect } from 'vitest';
import { escapeHtml } from '../src/utils/escape.js';

describe('escapeHtml', () => {
  it('escapes &', () => {
    expect(escapeHtml('a&b')).toBe('a&amp;b');
  });

  it('escapes <', () => {
    expect(escapeHtml('<script>')).toBe('&lt;script&gt;');
  });

  it('escapes >', () => {
    expect(escapeHtml('3 > 2')).toBe('3 &gt; 2');
  });

  it('escapes double quotes', () => {
    expect(escapeHtml('say "hello"')).toBe('say &quot;hello&quot;');
  });

  it('escapes single quotes', () => {
    expect(escapeHtml("it's")).toBe('it&#39;s');
  });

  it('escapes U+2028 (line separator)', () => {
    const input = 'line\u2028break';
    expect(escapeHtml(input)).toBe('line\\u2028break');
  });

  it('escapes U+2029 (paragraph separator)', () => {
    const input = 'para\u2029end';
    expect(escapeHtml(input)).toBe('para\\u2029end');
  });

  it('neutralizes standard XSS vectors', () => {
    const tests = [
      '<script>alert(1)</script>',
      '<img src=x onerror=alert(1)>',
      '"><script>alert(1)</script>',
      '<svg/onload=alert(1)>',
    ];
    for (const t of tests) {
      const out = escapeHtml(t);
      expect(out).not.toContain('<script>');
      expect(out).not.toContain('<img');
      expect(out).not.toContain('<svg');
      // onerror/onload still appear as text tokens (escaped, not dangerous)
      // Verify no raw angle brackets remain
      expect(out).not.toMatch(/[<>]/);
    }
  });

  it('neutralizes half-escaped injection', () => {
    // Attempt to break out of an attribute context via single-quote
    const input = "' onfocus='alert(1)'";
    const out = escapeHtml(input);
    expect(out).toBe('&#39; onfocus=&#39;alert(1)&#39;');
    expect(out).not.toContain("'onfocus");
  });

  it('handles empty string', () => {
    expect(escapeHtml('')).toBe('');
  });

  it('handles null/undefined', () => {
    expect(escapeHtml(null)).toBe('');
    expect(escapeHtml(undefined)).toBe('');
  });

  it('handles strings with no special chars', () => {
    expect(escapeHtml('hello world 123')).toBe('hello world 123');
  });

  it('handles mixed content', () => {
    const input = '<b>"He said & then left"</b>';
    const out = escapeHtml(input);
    expect(out).not.toMatch(/[<>"']/);
    expect(out).toContain('&lt;b&gt;');
    expect(out).toContain('&quot;');
    expect(out).toContain('&amp;');
  });
});
