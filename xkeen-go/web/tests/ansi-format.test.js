import { describe, it, expect } from 'vitest';
import { renderAnsi, stripAnsi } from '../src/utils/ansi-format.js';

describe('renderAnsi', () => {
  it('renders plain text unchanged (washes through escape)', () => {
    const out = renderAnsi('hello world');
    expect(out).toBe('hello world');
  });

  it('escapes HTML chars before ANSI processing', () => {
    const out = renderAnsi('<script>alert(1)</script>');
    expect(out).not.toContain('<script>');
    expect(out).toContain('&lt;script&gt;');
  });

  it('strips XSS vectors in plain text', () => {
    const vectors = [
      '<img src=x onerror=alert(1)>',
      '" onmouseover="alert(1)"',
      "'-alert(1)-'",
    ];
    for (const v of vectors) {
      const out = renderAnsi(v);
      // No raw <img or <script> tags
      expect(out).not.toContain('<img');
      expect(out).not.toContain('<script');
      // No raw unescaped angle brackets
      expect(out).not.toMatch(/(?<!&lt;)[<>](?!;)/);
    }
  });

  it('applies bold ANSI code', () => {
    const out = renderAnsi('\x1b[1mbold\x1b[0mnormal');
    expect(out).toContain('bold');
    expect(out).toContain('normal');
    expect(out).toContain('font-weight:bold');
    expect(out).toMatch(/<span[^>]*font-weight:bold[^>]*>bold<\/span>/);
  });

  it('applies foreground color ANSI code', () => {
    const out = renderAnsi('\x1b[31mred\x1b[0mnormal');
    expect(out).toMatch(/<span[^>]*color:#c00[^>]*>red<\/span>/);
  });

  it('applies bright foreground color', () => {
    const out = renderAnsi('\x1b[91mred\x1b[0m');
    expect(out).toMatch(/<span[^>]*color:#f55[^>]*>/);
  });

  it('applies background color', () => {
    const out = renderAnsi('\x1b[41mbg-red\x1b[0m');
    expect(out).toMatch(/<span[^>]*background:#c00[^>]*>bg-red<\/span>/);
  });

  it('strips non-SGR ANSI sequences (cursor movement)', () => {
    const out = renderAnsi('line1\x1b[2Kline2');
    expect(out).not.toContain('\x1b');
    expect(out).toContain('line1');
    expect(out).toContain('line2');
  });

  it('strips non-SGR CSI sequences (clear screen)', () => {
    const out = renderAnsi('before\x1b[2Jafter');
    expect(out).toContain('before');
    expect(out).toContain('after');
  });

  it('resets styles on code 0', () => {
    const out = renderAnsi('\x1b[1mbold\x1b[0mnormal\x1b[31mred\x1b[0mplain');
    expect(out).toMatch(/font-weight:bold[^>]*>bold/);
    expect(out).toContain('normal');
    expect(out).toMatch(/color:#c00[^>]*>red/);
    expect(out).toContain('plain');
  });

  it('handles multiple params in one SGR code', () => {
    const out = renderAnsi('\x1b[1;31mbold-red\x1b[0m');
    expect(out).toMatch(/font-weight:bold/);
    expect(out).toMatch(/color:#c00/);
  });

  it('handles empty input', () => {
    expect(renderAnsi('')).toBe('');
    expect(renderAnsi(null)).toBe('');
    expect(renderAnsi(undefined)).toBe('');
  });

  it('strips unrecognized ANSI sequences gracefully', () => {
    const out = renderAnsi('\x1b[38;5;82m256color\x1b[0m');
    expect(out).toContain('256color');
  });

  it('handles mixed ANSI and HTML-dangerous chars', () => {
    const out = renderAnsi('\x1b[31m<DANGER>\x1b[0m');
    expect(out).toContain('&lt;DANGER&gt;');
    expect(out).toMatch(/color:#c00/);
    const textContent = out.replace(/<[^>]+>/g, '');
    expect(textContent).not.toMatch(/[<>]/);
  });
});

describe('stripAnsi', () => {
  it('strips ANSI codes, returns plain text', () => {
    expect(stripAnsi('\x1b[31mhello\x1b[0m')).toBe('hello');
  });

  it('strips multiple ANSI codes', () => {
    expect(stripAnsi('\x1b[1mBOLD\x1b[0m and \x1b[31mRED\x1b[0m')).toBe('BOLD and RED');
  });

  it('handles non-SGR sequences', () => {
    expect(stripAnsi('a\x1b[2Jb\x1b[A\nc')).toBe('ab\nc');
  });

  it('returns empty string for empty input', () => {
    expect(stripAnsi('')).toBe('');
  });
});
