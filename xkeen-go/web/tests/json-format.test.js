import { describe, it, expect } from 'vitest';
import { formatJson } from '../src/utils/json-format.js';

describe('formatJson', () => {
  it('formats a simple object', () => {
    const out = formatJson({ key: 'value', num: 42 });
    expect(out).toContain('class="pk"');
    expect(out).toContain('class="ps"');
    expect(out).toContain('class="pn"');
  });

  it('escapes HTML in string values', () => {
    const out = formatJson({ x: '<script>alert(1)</script>' });
    expect(out).not.toContain('<script>');
    expect(out).toContain('&lt;script&gt;alert(1)&lt;/script&gt;');
  });

  it('escapes HTML in object keys', () => {
    const out = formatJson({ '<b>key</b>': 'val' });
    expect(out).not.toContain('<b>');
    expect(out).toContain('&lt;b&gt;');
  });

  it('highlights booleans', () => {
    const out = formatJson({ a: true, b: false });
    expect(out).toContain('class="pb"');
    // Booleans after : are wrapped in span, text "true"/"false" still inside
    expect(out).toMatch(/<span class="pb">true<\/span>/);
  });

  it('highlights null', () => {
    const out = formatJson({ a: null });
    expect(out).toContain('class="pu"');
  });

  it('handles nested objects', () => {
    const out = formatJson({ outer: { inner: 'deep' } });
    expect(out).toContain('class="pk"');
    expect(out).toContain('class="ps"');
  });

  it('handles arrays', () => {
    const out = formatJson([1, 'two', true, null]);
    // Array elements aren't prefixed by ':', so no syntax highlighting is applied
    // (same behavior as original _fmtJson)
    // Verify no raw HTML injection
    expect(out).not.toContain('<script>');
    expect(out).toContain('&quot;two&quot;');
  });

  it('handles empty objects and arrays', () => {
    expect(formatJson({})).toContain('{}');
    expect(formatJson([])).toContain('[]');
  });

  it('handles null/undefined values', () => {
    expect(formatJson(null)).toContain('null');
    expect(formatJson(undefined)).toBe('');
  });

  it('no raw angle brackets in escaped text portions', () => {
    const out = formatJson({ danger: '<img src=x onerror=alert(1)>' });
    expect(out).toContain('&lt;img src=x onerror=alert(1)&gt;');
    // Text outside trusted spans should have no raw angle brackets
    const textOuter = out.replace(/<span[^>]*>/g, '').replace(/<\/span>/g, '');
    expect(textOuter).not.toMatch(/[<>]/);
    expect(textOuter).toContain('{');
    expect(textOuter).toContain('}');
  });

  it('produces valid-looking JSON structure', () => {
    const out = formatJson({ a: 1 });
    expect(out).toMatch(/<span class="pk">/);
    expect(out).toContain(':');
    expect(out).toContain('\n');
  });
});
