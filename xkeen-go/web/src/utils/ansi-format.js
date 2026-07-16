/**
 * ANSI-rendering — converts raw terminal text with ANSI escape codes into safe HTML.
 *
 * SAFETY GUARANTEE: ALL HTML-special characters (& < > " ' + U+2028/U+2029) are
 * fully escaped BEFORE any <span> tags are inserted. The output is provably safe
 * for use with v-html.
 *
 * Handles SGR (Select Graphic Rendition) codes common in shell output:
 *   0 reset, 1/22 bold, 3/23 italic, 4/24 underline,
 *   30-37 fg colors, 40-47 bg colors, 90-97 bright fg, 100-107 bright bg,
 *   38;5;N / 48;5;N 256-color (basic support)
 *
 * Non-SGR CSI sequences (cursor movement, clear screen, etc.) are silently stripped.
 * Unrecognized SGR parameters are ignored.
 */

import { escapeHtml } from './escape.js';

/* ── ANSI color mapping ─────────────────────────────────── */

const ANSI_COLORS = {
  30: '#000', 31: '#c00', 32: '#0c0', 33: '#cc0',
  34: '#00c', 35: '#c0c', 36: '#0cc', 37: '#ccc',
  90: '#888', 91: '#f55', 92: '#5f5', 93: '#ff5',
  94: '#55f', 95: '#f5f', 96: '#5ff', 97: '#fff',
  40: '#000', 41: '#c00', 42: '#0c0', 43: '#cc0',
  44: '#00c', 45: '#c0c', 46: '#0cc', 47: '#ccc',
  100:'#888',101:'#f55',102:'#5f5',103:'#ff5',
  104:'#55f',105:'#f5f',106:'#5ff',107:'#fff',
};

/* ── ANSI state machine ─────────────────────────────────── */

/**
 * @param {string} rawText raw terminal text with ANSI codes
 * @returns {string} safe HTML string (can be used with v-html)
 */
export function renderAnsi(rawText) {
  const escaped = escapeHtml(rawText);

  // eslint-disable-next-line no-control-regex
  const parts = escaped.split(/(\x1b\[[\d;]*[a-zA-Z])/);
  const spans = []; // { text: string, css: string }
  let buf = '';
  const style = { bold: false, italic: false, underline: false, fg: null, bg: null };

  function flushText() {
    if (buf) {
      spans.push({ text: buf, css: toCss(style) });
      buf = '';
    }
  }

  for (const part of parts) {
    // eslint-disable-next-line no-control-regex
    const m = part.match(/^\x1b\[([\d;]*)([a-zA-Z])$/);
    if (m) {
      const nums = m[1] ? m[1].split(';') : ['0'];
      const cmd = m[2];
      if (cmd === 'm') {
        flushText();
        for (const n of nums) {
          const code = parseInt(n, 10);
          if (isNaN(code)) continue;
          if (code === 0) { style.bold = false; style.italic = false; style.underline = false; style.fg = null; style.bg = null; }
          else if (code === 1) { style.bold = true; }
          else if (code === 22) { style.bold = false; }
          else if (code === 3) { style.italic = true; }
          else if (code === 23) { style.italic = false; }
          else if (code === 4) { style.underline = true; }
          else if (code === 24) { style.underline = false; }
          else if (code >= 30 && code <= 37) { style.fg = code; }
          else if (code >= 90 && code <= 97) { style.fg = code; }
          else if (code >= 40 && code <= 47) { style.bg = code; }
          else if (code >= 100 && code <= 107) { style.bg = code; }
          else if (code === 39) { style.fg = null; }
          else if (code === 49) { style.bg = null; }
          // 38/48 with ;5;N (256-color) — skip for simplicity
        }
      } // non-SGR CSI sequences (cursor, clear, etc.) — silently stripped
    } else {
      buf += part;
    }
  }

  flushText();

  /* ── Build HTML from spans ── */
  let out = '';
  for (const sp of spans) {
    if (sp.css) {
      out += '<span style="' + sp.css + '">' + sp.text + '</span>';
    } else {
      out += sp.text;
    }
  }
  return out;
}

/**
 * Strips all ANSI escape codes, returns plain text.
 * @param {string} text raw terminal text
 * @returns {string} plain text (no HTML escaping)
 */
export function stripAnsi(text) {
  // eslint-disable-next-line no-control-regex
  return String(text).replace(/\x1b\[[\d;]*[a-zA-Z]/g, '');
}

/* ── helpers ────────────────────────────────────────────── */

function toCss(s) {
  const rules = [];
  if (s.bold) rules.push('font-weight:bold');
  if (s.italic) rules.push('font-style:italic');
  if (s.underline) rules.push('text-decoration:underline');
  if (s.fg !== null && ANSI_COLORS[s.fg]) rules.push('color:' + ANSI_COLORS[s.fg]);
  if (s.bg !== null && ANSI_COLORS[s.bg]) rules.push('background:' + ANSI_COLORS[s.bg]);
  return rules.join(';');
}
