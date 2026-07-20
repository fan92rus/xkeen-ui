// @vitest-environment happy-dom
// tests/rws.test.js — Tests for ReconnectingWebSocket
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ReconnectingWebSocket } from '../src/utils/rws.js';

// Mock WebSocket — synchronous open for deterministic testing
class MockWebSocket {
    static OPEN = 1;
    static CLOSED = 3;
    constructor(url) {
        this.url = url;
        this.readyState = 1; // OPEN immediately
        this.onopen = null;
        this.onmessage = null;
        this.onclose = null;
        this.onerror = null;
        this._sent = [];
    }
    send(data) { this._sent.push(data); }
    close() {
        this.readyState = 3;
    }
    // Test helpers
    _open() { this.readyState = 1; this.onopen?.(); }
    _message(data) { this.onmessage?.({ data }); }
    _close(code = 1000, reason = '') {
        this.readyState = 3;
        this.onclose?.({ code, reason });
    }
    _error() { this.onerror?.(); }
}

beforeEach(() => {
    global.WebSocket = MockWebSocket;
    vi.useFakeTimers();
});
afterEach(() => {
    vi.useRealTimers();
    delete global.WebSocket;
});

describe('ReconnectingWebSocket', () => {
    it('builds URL with correct protocol', () => {
        const url = ReconnectingWebSocket.buildURL('/ws/logs');
        expect(url).toMatch(/^wss?:\/\/[^/]+\/ws\/logs$/);
    });

    it('connects and calls onOpen/onStatus', () => {
        const onOpen = vi.fn();
        const onStatus = vi.fn();
        const rws = new ReconnectingWebSocket('ws://localhost/ws', { onOpen, onStatus });
        rws.connect();
        rws.ws._open(); // trigger open event
        expect(onOpen).toHaveBeenCalled();
        expect(onStatus).toHaveBeenCalledWith('connected');
        rws.disconnect();
    });

    it('dispatches messages to onMessage', () => {
        const onMessage = vi.fn();
        const rws = new ReconnectingWebSocket('ws://localhost/ws', { onMessage });
        rws.connect();
        rws.ws._open();
        rws.ws._message('{"type":"ping"}');
        expect(onMessage).toHaveBeenCalledWith({ data: '{"type":"ping"}' });
        rws.disconnect();
    });

    it('reconnects after unexpected close', () => {
        const onStatus = vi.fn();
        const rws = new ReconnectingWebSocket('ws://localhost/ws', { onStatus });
        rws.connect();
        rws.ws._open();
        // Simulate unexpected close
        rws.ws._close(1006, 'abnormal');
        expect(onStatus).toHaveBeenCalledWith('disconnected');
        // Advance past backoff delay
        vi.advanceTimersByTime(5000);
        expect(rws.reconnectAttempts).toBe(1);
        rws.disconnect();
    });

    it('does NOT reconnect on server rejection (code >= 4000)', () => {
        const rws = new ReconnectingWebSocket('ws://localhost/ws');
        rws.connect();
        rws.ws._open();
        // Simulate policy violation / rejection
        rws.ws._close(4001, 'unauthorized');
        // Advance past any potential backoff
        vi.advanceTimersByTime(10000);
        // reconnectAttempts stays 0 — no reconnection attempted
        expect(rws.reconnectAttempts).toBe(0);
    });

    it('does NOT reconnect after intentional disconnect', () => {
        const rws = new ReconnectingWebSocket('ws://localhost/ws');
        rws.connect();
        rws.disconnect();
        expect(rws.stopped).toBe(true);
        // Advance time — no reconnection should happen
        vi.advanceTimersByTime(10000);
        expect(rws.reconnectAttempts).toBe(0);
    });

    it('isOpen reflects connection state', () => {
        const rws = new ReconnectingWebSocket('ws://localhost/ws');
        rws.connect();
        rws.ws._open();
        expect(rws.isOpen()).toBe(true);
        rws.disconnect();
        expect(rws.isOpen()).toBe(false);
    });
});
