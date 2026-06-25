// Composable: WebSocket lifecycle for live metrics.
import { ref, shallowReactive } from 'vue';
import { error as logError } from '../utils/logger.js';
import { MetricsWS } from '../services/metrics.js';

/**
 * Manages a MetricsWS connection and exposes reactive state.
 *
 * @returns {{
 *   wsStatus: import('vue').Ref<string>,
 *   wsError:  import('vue').Ref<string>,
 *   history:  import('vue').ShallowReactive<Array>,
 *   latestSnap: import('vue').Ref<object|null>,
 *   connect:  () => void,
 *   disconnect: () => void,
 * }}
 */
export function useMetricsWS() {
	const wsStatus = ref('disconnected');
	const wsError = ref('');
	const history = shallowReactive([]);
	const latestSnap = ref(null);
	let ws = null;

	function connect() {
		if (ws) return;
		wsStatus.value = 'connecting';
		ws = new MetricsWS({
			onData: (msg) => {
				if (msg.type === 'history') {
					history.splice(0, history.length, ...msg.history);
				} else if (msg.type === 'snapshot') {
					latestSnap.value = msg.snap;
					history.push(msg.snap);
					if (history.length > 300) history.splice(0, history.length - 300);
				}
			},
			onError: (err) => { wsError.value = String(err); },
			onStatus: (status) => {
				wsStatus.value = status;
				if (status === 'connected') wsError.value = '';
			},
		});
		const cached = ws.getCachedHistory();
		if (cached.length > 0) history.splice(0, history.length, ...cached);
		ws.connect();
	}

	function disconnect() {
		if (ws) { ws.disconnect(); ws = null; }
		wsStatus.value = 'disconnected';
	}

	return { wsStatus, wsError, history, latestSnap, connect, disconnect };
}
