/**
 * Complete HTML escaping — safe for text content and attribute contexts.
 * Covers: & < > " ' U+2028 U+2029
 * @param {string} s raw string to escape
 * @returns {string} HTML-safe string
 */
export function escapeHtml(s) {
  if (s == null) return '';
  return String(s).replace(/[&<>"'\u2028\u2029]/g, ch => {
    switch (ch) {
      case '&':  return '&amp;';
      case '<':  return '&lt;';
      case '>':  return '&gt;';
      case '"':  return '&quot;';
      case "'":  return '&#39;';
      case '\u2028': return '\\u2028';
      case '\u2029': return '\\u2029';
      default:   return ch;
    }
  });
}
