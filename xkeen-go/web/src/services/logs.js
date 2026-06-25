// services/logs.js - Logs fetching and WebSocket streaming

import { warn } from '../utils/logger.js';
import { get } from './api.js';
import { computeBackoffDelay } from '../utils/backoff.js';

export async function fetchLogs(path, lines = 100) {
    const data = await get(`/api/logs/xray?path=${encodeURIComponent(path)}&lines=${lines}`);
    return data.entries || [];
}

/**
 * Create an auto-reconnecting WebSocket log stream.
 *
 * @param {function} onMessage - Called with each parsed log message (non-ping).
 * @param {function} [onError] - Called on socket errors.
 * @param {function} [onStatus] - Called with 'connected'|'disconnected' on
 *   connection state changes (optional — existing callers may omit it).
 * @returns {{close: function, isOpen: function}} handle with close() and isOpen().
 */
export function createLogStream(onMessage, onError, onStatus) {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/logs`;

    let ws = null;
    let reconnectTimer = null;
    let reconnectAttempts = 0;
    let stopped = false;

    const status = (s) => { try { onStatus?.(s); } catch (_) { /* ignore */ } };

    function open() {
        ws = new WebSocket(wsUrl);

        ws.onopen = () => {
            reconnectAttempts = 0; // back on track — reset backoff
            status('connected');
        };

        ws.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                if (data.type !== 'ping') {
                    onMessage(data);
                }
            } catch (e) {
                warn('Failed to parse WebSocket message:', e);
            }
        };

        ws.onclose = () => {
            status('disconnected');
            if (!stopped) {
                const delay = computeBackoffDelay(reconnectAttempts++);
                reconnectTimer = setTimeout(open, delay);
            }
        };

        ws.onerror = () => {
            // onclose will always follow onerror; reconnect handled there.
            try { onError?.(); } catch (_) { /* ignore */ }
        };
    }

    open();

    return {
        close: () => {
            stopped = true;
            if (reconnectTimer) {
                clearTimeout(reconnectTimer);
                reconnectTimer = null;
            }
            if (ws) {
                // Remove listeners so our own close() doesn't trigger a reconnect.
                ws.onclose = null;
                try { ws.close(); } catch (_) { /* already closed */ }
                ws = null;
            }
        },
        isOpen: () => ws !== null && ws.readyState === WebSocket.OPEN
    };
}
