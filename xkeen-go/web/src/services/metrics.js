// services/metrics.js - WebSocket client for Xray metrics with localStorage caching

const STORAGE_KEY = 'xkeen_metrics_history';
const STORAGE_TTL = 10 * 60 * 1000; // 10 minutes in ms

/**
 * MetricsWS manages a WebSocket connection to /ws/metrics.
 *
 * Features:
 * - Connects to backend WS and receives live metrics every ~2s
 * - Stores received snapshots in localStorage for up to 10 minutes
 * - On connect, requests backend history to fill gaps if localStorage is stale
 * - Auto-reconnects on disconnect
 */
export class MetricsWS {
	/**
	 * @param {object} opts
	 * @param {function} opts.onData - Called with { type, history?, snap? } on each message
	 * @param {function} [opts.onError] - Called on errors
	 * @param {function} [opts.onStatus] - Called with 'connected'|'disconnected'
	 */
	constructor({ onData, onError, onStatus }) {
		this.onData = onData;
		this.onError = onError || (() => {});
		this.onStatus = onStatus || (() => {});
		this.ws = null;
		this.reconnectTimer = null;
		this.stopped = false;
		this._mergeBuffer = []; // buffer for merging incoming snapshots
	}

	connect() {
		this.stopped = false;
		this._doConnect();
	}

	_doConnect() {
		if (this.stopped) return;

		const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const url = `${proto}//${window.location.host}/ws/metrics`;
		this.ws = new WebSocket(url);

		this.ws.onopen = () => {
			this.onStatus('connected');
		};

		this.ws.onmessage = (event) => {
			try {
				const msg = JSON.parse(event.data);
				if (msg.type === 'ping') return;

				if (msg.type === 'history') {
					// Backend sent historical data — merge with localStorage
					const localData = this._loadFromStorage();
					const merged = this._mergeHistory(localData, msg.history || []);
					this._saveToStorage(merged);
					this.onData({ type: 'history', history: merged });
				} else if (msg.type === 'snapshot' && msg.snap) {
					// Live snapshot — append to storage
					this._appendSnapshot(msg.snap);
					this.onData({ type: 'snapshot', snap: msg.snap });
				} else if (msg.type === 'error') {
					this.onError(msg.error);
				}
			} catch (e) {
				console.error('MetricsWS: failed to parse message', e);
			}
		};

		this.ws.onclose = () => {
			this.onStatus('disconnected');
			if (!this.stopped) {
				this.reconnectTimer = setTimeout(() => this._doConnect(), 3000);
			}
		};

		this.ws.onerror = (err) => {
			this.onError(err);
		};
	}

	disconnect() {
		this.stopped = true;
		if (this.reconnectTimer) {
			clearTimeout(this.reconnectTimer);
			this.reconnectTimer = null;
		}
		if (this.ws) {
			this.ws.close();
			this.ws = null;
		}
	}

	// ── localStorage helpers ──

	_loadFromStorage() {
		try {
			const raw = localStorage.getItem(STORAGE_KEY);
			if (!raw) return [];
			const parsed = JSON.parse(raw);
			if (!parsed.ts || !Array.isArray(parsed.data)) return [];
			// Check TTL
			if (Date.now() - parsed.ts > STORAGE_TTL) {
				localStorage.removeItem(STORAGE_KEY);
				return [];
			}
			return parsed.data;
		} catch {
			return [];
		}
	}

	_saveToStorage(data) {
		try {
			localStorage.setItem(STORAGE_KEY, JSON.stringify({
				ts: Date.now(),
				data: data.slice(-300), // Keep max 300 entries (~10 min at 2s)
			}));
		} catch {
			// localStorage full or unavailable — ignore
		}
	}

	_appendSnapshot(snap) {
		const data = this._loadFromStorage();
		data.push(snap);
		this._saveToStorage(data);
	}

	/**
	 * Merge localStorage data with backend history.
	 * Backend history is sparse (every 20s), local data is dense (every 2s).
	 * We keep local data where available, fill gaps with backend data.
	 */
	_mergeHistory(local, backend) {
		if (!backend.length) return local;
		if (!local.length) return backend;

		// Build a map by timestamp for dedup
		const byTs = new Map();
		for (const s of backend) {
			byTs.set(s.ts, s);
		}
		for (const s of local) {
			byTs.set(s.ts, s); // local data wins on conflict (more recent/detailed)
		}

		// Sort by timestamp
		const merged = Array.from(byTs.values());
		merged.sort((a, b) => a.ts - b.ts);
		return merged;
	}

	/**
	 * Get the current cached history from localStorage.
	 * Useful for initial render without waiting for WS.
	 */
	getCachedHistory() {
		return this._loadFromStorage();
	}
}

// Legacy API helpers (kept for backward compat — e.g. settings)
import * as api from './api.js';

export const getMetricsStats = () => api.get('/api/metrics/stats');
export const getMetricsObservatory = () => api.get('/api/metrics/observatory');
export const getMetricsPort = () => api.get('/api/settings/metrics');
export const updateMetricsPort = (port) => api.put('/api/settings/metrics', { metrics_port: port });
export const getProxyNames = () => api.get('/api/metrics/proxy-names');
