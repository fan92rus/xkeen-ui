// utils/log-filter.js — pure logic extracted from the app store so it can be
// unit-tested in isolation. The store keeps its reactive computed that
// delegates here.

/**
 * Filter a list of log entries by level and a case-insensitive search term.
 *
 * @param {Array<{level:string,message:string}>} logs
 * @param {string} levelFilter - a log level ('debug'|'info'|...) or 'all'
 * @param {string} search      - free-text substring; empty = no text filter
 * @returns {Array} the filtered entries (same order)
 */
export function filterLogs(logs, levelFilter, search) {
  let result = logs;
  if (levelFilter && levelFilter !== 'all') {
    result = result.filter(l => l.level === levelFilter);
  }
  if (search) {
    const t = search.toLowerCase();
    result = result.filter(l => l.message.toLowerCase().includes(t));
  }
  return result;
}
