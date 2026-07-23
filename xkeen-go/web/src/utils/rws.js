// utils/rws.js — ReconnectingWebSocket base class.
//
// Provides auto-reconnect with exponential backoff, shared by logs.js
// and metrics.js.  Subclasses or callers define onMessage handling.

import { computeBackoffDelay } from './backoff.js';

/**
 * Auto-reconnecting WebSocket.
 *
 * @param {string} url - WebSocket URL (ws:// or wss://).
 * @param {object} handlers
 * @param {function(MessageEvent)} handlers.onMessage - Called for each raw message.
 * @param {function()} [handlers.onOpen] - Called on successful connection.
 * @param {function(CloseEvent)} [handlers.onClose] - Called on socket close (before reconnect logic).
 * @param {function()} [handlers.onError] - Called on socket error.
 * @param {function(string)} [handlers.onStatus] - Called with 'connected'|'disconnected'.
 */
export class ReconnectingWebSocket {
    constructor(url, { onMessage, onOpen, onClose, onError, onStatus } = {}) {
        this.url = url;
        this.onMessage = onMessage || (() => {});
        this.onOpen = onOpen || (() => {});
        this.onClose = onClose || (() => {});
        this.onError = onError || (() => {});
        this.onStatus = onStatus || (() => {});

        this.ws = null;
        this.reconnectTimer = null;
        this.reconnectAttempts = 0;
        this.stopped = false;
    }

    /** Build the WebSocket URL from a path like '/ws/logs'. */
    static buildURL(path) {
        const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        return `${proto}//${window.location.host}${path}`;
    }

    /** Open the connection (public entry point). */
    connect() {
        this.stopped = false;
        this._doConnect();
    }

    _doConnect() {
        if (this.stopped) return;

        this.ws = new WebSocket(this.url);

        this.ws.onopen = () => {
            this.reconnectAttempts = 0;
            this.onStatus('connected');
            this.onOpen();
        };

        this.ws.onmessage = (event) => {
            this.onMessage(event);
        };

        this.ws.onclose = (event) => {
            this.onStatus('disconnected');
            this.onClose(event);
            // Stop reconnecting on intentional close or server rejection (code >= 4000).
            if (this.stopped || (event && event.code >= 4000)) {
                return;
            }
            const delay = computeBackoffDelay(this.reconnectAttempts++);
            this.reconnectTimer = setTimeout(() => this._doConnect(), delay);
        };

        this.ws.onerror = () => {
            this.onError();
        };
    }

    /** Close the connection and stop reconnecting. */
    disconnect() {
        this.stopped = true;
        if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer);
            this.reconnectTimer = null;
        }
        if (this.ws) {
            // Remove listeners so our own close() doesn't trigger a reconnect.
            this.ws.onclose = null;
            try { this.ws.close(); } catch { /* already closed */ }
            this.ws = null;
        }
    }

    /** Returns true if the socket is open. */
    isOpen() {
        return this.ws !== null && this.ws.readyState === WebSocket.OPEN;
    }
}
