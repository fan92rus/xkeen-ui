// Formatters for metrics display (speed, volume, time, percentiles).
// All functions are pure — no reactive dependencies.

/**
 * Format bytes into human-readable form.
 * @param {number} v — value in bytes
 * @returns {string} e.g. "1.5 MB"
 */
export function fmtBytes(v) {
	if (!v || v <= 0) return '0 B';
	const units = ['B', 'KB', 'MB', 'GB', 'TB'];
	let i = 0;
	while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
	return v.toFixed(i === 0 ? 0 : 1) + ' ' + units[i];
}

/**
 * Format transfer rate (bytes/second) into human-readable form.
 * @param {number} v — value in B/s
 * @returns {string} e.g. "2.3 MB/s"
 */
export function fmtRate(v) {
	if (!v || v <= 0) return '0 B/s';
	const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
	let i = 0;
	while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
	return v.toFixed(i === 0 ? 0 : 1) + ' ' + units[i];
}

/**
 * Short rate label for chart axis ticks.
 * @param {number} v
 * @returns {string} e.g. "2.3M"
 */
export function fmtRateShort(v) {
	if (!v || v <= 0) return '0';
	const units = ['B', 'K', 'M', 'G'];
	let i = 0;
	while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
	return v.toFixed(i === 0 ? 0 : 1) + units[i];
}

/**
 * Format delay in ms to human-readable.
 * @param {number} ms
 * @returns {string}
 */
export function fmtDelay(ms) {
	if (!ms || ms <= 0) return '—';
	return ms < 1000 ? Math.round(ms) + ' ms' : (ms / 1000).toFixed(1) + ' s';
}

/**
 * Format a Unix timestamp to HH:MM:SS.
 * @param {number} ts — Unix seconds
 * @returns {string}
 */
export function fmtTime(ts) {
	return new Date(ts * 1000).toLocaleTimeString('ru-RU', {
		hour: '2-digit', minute: '2-digit', second: '2-digit',
	});
}

/**
 * Format a Unix timestamp to MM:SS.
 * @param {number} ts — Unix seconds
 * @returns {string}
 */
export function fmtTimeShort(ts) {
	return new Date(ts * 1000).toLocaleTimeString('ru-RU', {
		minute: '2-digit', second: '2-digit',
	});
}

/**
 * Format a duration in seconds to compact Russian form.
 * @param {number} seconds
 * @returns {string} e.g. "3ч 15м" or "45с"
 */
export function fmtDuration(seconds) {
	if (seconds < 60) return Math.round(seconds) + 'с';
	if (seconds < 3600) return Math.floor(seconds / 60) + 'м ' + Math.round(seconds % 60) + 'с';
	const h = Math.floor(seconds / 3600);
	const m = Math.floor((seconds % 3600) / 60);
	return h + 'ч ' + m + 'м';
}

/**
 * Compute the p-th percentile from an array of numbers.
 * @param {number[]} arr
 * @param {number} p — percentile (0–100)
 * @returns {number}
 */
export function percentile(arr, p) {
	if (!arr.length) return 0;
	const sorted = [...arr].sort((a, b) => a - b);
	const idx = Math.ceil((p / 100) * sorted.length) - 1;
	return sorted[Math.max(0, idx)];
}
