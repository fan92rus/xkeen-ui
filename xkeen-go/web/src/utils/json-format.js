/**
 * Safe JSON formatting — converts a JavaScript value to a syntax-highlighted HTML string.
 *
 * SAFETY GUARANTEE: The JSON string is fully HTML-escaped before any <span> tags
 * are applied. The regex highlighting operates only on &quot;-encoded JSON strings,
 * making it provably safe for use with v-html.
 */

import { escapeHtml } from './escape.js';

/**
 * @param {any} obj any JSON-serializable value
 * @returns {string} safe HTML string (can be used with v-html)
 */
export function formatJson(obj) {
  const raw = JSON.stringify(obj, null, 2);
  const safe = escapeHtml(raw);

  // Highlight JSON keys: "key":
  const withKeys = safe.replace(
    /(&quot;(?:[^&]|&(?!quot;))*?&quot;)\s*:/g,
    '<span class="pk">$1</span>:'
  );

  // Highlight JSON string values: "value"
  const withStrings = withKeys.replace(
    /:\s*(&quot;(?:[^&]|&(?!quot;))*?&quot;)/g,
    ': <span class="ps">$1</span>'
  );

  // Highlight numbers
  const withNumbers = withStrings.replace(
    /:\s*(\d+\.?\d*)/g,
    ': <span class="pn">$1</span>'
  );

  // Highlight booleans
  const withBooleans = withNumbers.replace(
    /:\s*(true|false)/g,
    ': <span class="pb">$1</span>'
  );

  // Highlight null
  return withBooleans.replace(
    /:\s*(null)/g,
    ': <span class="pu">$1</span>'
  );
}
