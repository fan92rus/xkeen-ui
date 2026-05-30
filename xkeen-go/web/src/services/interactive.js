// services/interactive.js - WebSocket client for interactive command execution

const WS_BASE = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
const WS_HOST = WS_BASE + '//' + window.location.host;

export class InteractiveSession {
    constructor(command, onMessage, onComplete, onError) {
        this.ws = null;
        this.command = command;
        this.onMessage = onMessage;
        this.onComplete = onComplete;
        this.onError = onError;
        this.connected = false;
    }

    connect() {
        this.ws = new WebSocket(`${WS_HOST}/ws/xkeen/interactive`);

        this.ws.onopen = () => {
            this.connected = true;
            this.ws.send(JSON.stringify({ type: 'start', command: this.command }));
        };

        this.ws.onmessage = (event) => {
            try {
                const msg = JSON.parse(event.data);
                this.handleMessage(msg);
            } catch (e) {
                console.warn('Failed to parse WebSocket message:', event.data);
            }
        };

        this.ws.onerror = (error) => {
            if (this.onError) this.onError(error);
        };

        this.ws.onclose = () => {
            this.connected = false;
        };
    }

    send(text) {
        if (this.ws && this.connected) {
            this.ws.send(JSON.stringify({ type: 'input', text }));
        }
    }

    sendSignal(signal) {
        if (this.ws && this.connected) {
            this.ws.send(JSON.stringify({ type: 'signal', signal }));
        }
    }

    close() {
        if (this.ws) {
            this.ws.close();
            this.ws = null;
            this.connected = false;
        }
    }

    handleMessage(msg) {
        if (msg.type === 'complete') {
            this.connected = false;
            this.onComplete?.(msg);
        } else if (msg.type === 'output' || msg.type === 'error') {
            this.onMessage?.(msg);
        }
    }
}
