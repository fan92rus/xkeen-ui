// utils/diff.js — line-by-line text diff with HTML escaping, extracted from
// the app store for unit testing. Caps the comparison at MAX lines so a huge
// config can't blow up the diff modal.

const MAX = 500;

function escapeHtml(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

/**
 * Produce an HTML string marking per-line additions/removals between two texts.
 *
 * Lines present in `a` but not `b` are "removed"; lines in `b` but not `a`
 * are "added"; shared lines are "equal" (no marker). Comparison is set-based
 * (a line is "equal" if it appears anywhere in the other side), matching the
 * original store behaviour.
 *
 * @param {string} a - current content (left side)
 * @param {string} b - saved content (right side)
 * @returns {string} HTML with <span class="diff-removed"> / diff-added markup
 */
export function computeDiff(a, b) {
  const linesA = (a || '').split('\n').slice(0, MAX);
  const linesB = (b || '').split('\n').slice(0, MAX);
  const setB = new Set(linesB);
  const setA = new Set(linesA);
  const all = [];
  for (const l of linesA) all.push({ line: l, type: setB.has(l) ? 'equal' : 'removed' });
  for (const l of linesB) if (!setA.has(l)) all.push({ line: l, type: 'added' });
  return all.map(op => {
    const e = escapeHtml(op.line);
    return op.type === 'equal' ? '  ' + e
      : op.type === 'removed' ? `<span class="diff-removed">- ${e}</span>`
      : `<span class="diff-added">+ ${e}</span>`;
  }).join('\n');
}
