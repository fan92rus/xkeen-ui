// services/logs.js - Logs fetching and WebSocket streaming

import { warn } from '../utils/logger.js';
import { get } from './api.js';
import { ReconnectingWebSocket } from '../utils/rws.js';

export async function fetchLogs(path, lines = 100) {
    const data = await get(`/api/logs/xray?path=${encodeURIComponent(path)}&lines=${lines}`);
    return data.entries || [];
}

/**
 * Create an auto-reconnecting WebSocket log stream.
 *
 * @param {function} onMessage - Called with each parsed log message (non-ping).
 * @param {function} [onError] - Called on socket errors.
 * @param {function} [onStatus] - Called with 'connected'|'disconnected'.
 * @returns {{close: function, isOpen: function}} handle with close() and isOpen().
 */
export function createLogStream(onMessage, onError, onStatus) {
    const rws = new ReconnectingWebSocket(ReconnectingWebSocket.buildURL('/ws/logs'), {
        onMessage: (event) => {
            try {
                const data = JSON.parse(event.data);
                if (data.type !== 'ping') {
                    onMessage(data);
                }
            } catch (e) {
                warn('Failed to parse WebSocket message:', e);
            }
        },
        onError: () => {
            try { onError?.(); } catch (_) { /* ignore */ }
        },
        onStatus: (s) => {
            try { onStatus?.(s); } catch (_) { /* ignore */ }
        },
    });

    rws.connect();

    return {
        close: () => rws.disconnect(),
        isOpen: () => rws.isOpen(),
    };
}
