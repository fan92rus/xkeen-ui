// @vitest-environment happy-dom
// Tests for InteractiveSession — standalone class, no Vue/app dependency.
// happy-dom provides window.location, WebSocket is mocked per test.

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { InteractiveSession } from '../src/services/interactive.js';

describe('InteractiveSession', () => {
    let mockWS;

    beforeEach(() => {
        mockWS = null;
        global.WebSocket = vi.fn(function WS() {
            const ws = {
                readyState: 1,
                _onopen: null,
                _onmessage: null,
                _onclose: null,
                _onerror: null,
                close: vi.fn(),
                send: vi.fn(),
                set onopen(fn) { this._onopen = fn; },
                get onopen() { return this._onopen; },
                set onmessage(fn) { this._onmessage = fn; },
                get onmessage() { return this._onmessage; },
                set onclose(fn) { this._onclose = fn; },
                get onclose() { return this._onclose; },
                set onerror(fn) { this._onerror = fn; },
                get onerror() { return this._onerror; }
            };
            mockWS = ws;
            return ws;
        });
    });

    function createSession(opts = {}) {
        const onMessage = opts.onMessage || vi.fn();
        const onComplete = opts.onComplete || vi.fn();
        const onError = opts.onError || vi.fn();
        const session = new InteractiveSession(
            opts.command || 'test-cmd',
            onMessage,
            onComplete,
            onError
        );
        return { session, onMessage, onComplete, onError };
    }

    function connectSession(session) {
        session.connect();
        expect(mockWS).not.toBeNull();
        // Simulate onopen
        mockWS._onopen();
        return mockWS;
    }

    describe('constructor', () => {
        it('initialises with defaults', () => {
            const { session } = createSession();
            expect(session.connected).toBe(false);
            expect(session.completed).toBe(false);
            expect(session.command).toBe('test-cmd');
            expect(session.ws).toBeNull();
        });
    });

    describe('connect', () => {
        it('creates WebSocket and sends start message on open', () => {
            const { session } = createSession({ command: 'status' });
            const ws = connectSession(session);
            expect(ws.send).toHaveBeenCalledWith(
                JSON.stringify({ type: 'start', command: 'status' })
            );
            expect(session.connected).toBe(true);
        });
    });

    describe('handleMessage — normal complete', () => {
        it('calls onComplete and marks completed on "complete" message', () => {
            const { session, onComplete } = createSession();
            connectSession(session);

            const msg = {
                type: 'complete',
                success: true,
                exitCode: 0,
                output: 'done'
            };
            session.handleMessage(msg);

            expect(onComplete).toHaveBeenCalledWith(msg);
            expect(session.connected).toBe(false);
            expect(session.completed).toBe(true);
        });

        it('calls onMessage for output and error types', () => {
            const { session, onMessage } = createSession();
            connectSession(session);

            session.handleMessage({ type: 'output', text: 'hello' });
            expect(onMessage).toHaveBeenCalledWith({ type: 'output', text: 'hello' });

            session.handleMessage({ type: 'error', text: 'oops' });
            expect(onMessage).toHaveBeenCalledWith({ type: 'error', text: 'oops' });
        });
    });

    describe('onclose — completion fallback', () => {
        it('calls onComplete with failure when ws closes without "complete" message', () => {
            const { session, onComplete } = createSession();
            connectSession(session);

            // WS closes without sending 'complete'
            mockWS._onclose();

            expect(onComplete).toHaveBeenCalledWith({
                success: false,
                exitCode: -1
            });
            expect(session.connected).toBe(false);
            expect(session.completed).toBe(true);
        });

        it('does NOT call onComplete twice when complete arrives then ws closes', () => {
            const { session, onComplete } = createSession();
            connectSession(session);

            // Normal complete arrives
            session.handleMessage({
                type: 'complete',
                success: true,
                exitCode: 0
            });
            expect(onComplete).toHaveBeenCalledTimes(1);

            // WS close fires after server-side close
            mockWS._onclose();
            // onComplete should NOT be called again (completed flag)
            expect(onComplete).toHaveBeenCalledTimes(1);
        });

        it('calls onComplete only once even if close fires multiple times', () => {
            const { session, onComplete } = createSession();
            connectSession(session);

            mockWS._onclose();
            expect(onComplete).toHaveBeenCalledTimes(1);

            // Second close should be no-op
            mockWS._onclose();
            expect(onComplete).toHaveBeenCalledTimes(1);
        });
    });

    describe('ws.onerror', () => {
        it('calls onError but does NOT trigger onComplete', () => {
            const { session, onError, onComplete } = createSession();
            connectSession(session);

            const error = new Event('error');
            mockWS._onerror(error);

            expect(onError).toHaveBeenCalledWith(error);
            // onclose will fire after error and trigger onComplete
            expect(onComplete).not.toHaveBeenCalled();
        });

        it('triggers onComplete via onclose after error (error -> close sequence)', () => {
            const { session, onError, onComplete } = createSession();
            connectSession(session);

            mockWS._onerror(new Event('error'));
            mockWS._onclose();

            // onComplete should fire from onclose
            expect(onComplete).toHaveBeenCalledWith({
                success: false,
                exitCode: -1
            });
            expect(onComplete).toHaveBeenCalledTimes(1);
        });
    });

    describe('send / sendSignal / close', () => {
        it('sends input message when connected', () => {
            const { session } = createSession();
            connectSession(session);

            session.send('hello');
            expect(mockWS.send).toHaveBeenCalledWith(
                JSON.stringify({ type: 'input', text: 'hello' })
            );
        });

        it('does not send when not connected', () => {
            const { session } = createSession();
            session.send('hello');
            // ws.send should not have been called (no ws)
            // or send not called on mock
            expect(mockWS?.send).toBeUndefined();
        });

        it('sends signal', () => {
            const { session } = createSession();
            connectSession(session);

            session.sendSignal('SIGINT');
            expect(mockWS.send).toHaveBeenCalledWith(
                JSON.stringify({ type: 'signal', signal: 'SIGINT' })
            );
        });

        it('close cleans up', () => {
            const { session } = createSession();
            connectSession(session);

            session.close();
            expect(mockWS.close).toHaveBeenCalled();
            expect(session.ws).toBeNull();
            expect(session.connected).toBe(false);
        });
    });
});
