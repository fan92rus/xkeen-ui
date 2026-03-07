// services/interactive.js - WebSocket client for interactive command execution

const WS_BASE = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
const WS_HOST = WS_BASE + '//' + window.location.host;

/**
 * InteractiveSession manages a WebSocket connection for interactive command execution.
 */
export class InteractiveSession {
    /**
     * @param {string} command - Command to execute
     * @param {function} onMessage - Callback for output/error messages: (msg: {type, text}) => void
     * @param {function} onComplete - Callback when command completes: (msg: {success, exitCode}) => void
     * @param {function} onError - Callback for connection errors: (error) => void
     */
    constructor(command, onMessage, onComplete, onError) {
        this.ws = null;
        this.command = command;
        this.onMessage = onMessage;
        this.onComplete = onComplete;
        this.onError = onError;
        this.connected = false;
    }

    /**
     * Connect to WebSocket and start the command.
     */
    connect() {
        this.ws = new WebSocket(`${WS_HOST}/ws/xkeen/interactive`);

        this.ws.onopen = () => {
            console.log('Interactive WebSocket connected');
            this.connected = true;
            // Send start message
            this.ws.send(JSON.stringify({
                type: 'start',
                command: this.command
            }));
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
            console.error('WebSocket error:', error);
            if (this.onError) {
                this.onError(error);
            }
        };

        this.ws.onclose = (event) => {
            console.log('WebSocket closed:', event.code, event.reason);
            this.connected = false;
        };
    }

    /**
     * Send input to the running command.
     * @param {string} text - Text to send (should include \n for Enter)
     */
    send(text) {
        if (this.ws && this.connected) {
            this.ws.send(JSON.stringify({
                type: 'input',
                text: text
            }));
        }
    }

    /**
     * Send a signal to the running command.
     * @param {string} signal - Signal name (e.g., 'SIGTERM')
     */
    sendSignal(signal) {
        if (this.ws && this.connected) {
            this.ws.send(JSON.stringify({
                type: 'signal',
                signal: signal
            }));
        }
    }

    /**
     * Close the WebSocket connection.
     */
    close() {
        if (this.ws) {
            this.ws.close();
            this.ws = null;
            this.connected = false;
        }
    }

    /**
     * Handle incoming messages.
     */
    handleMessage(msg) {
        if (msg.type === 'complete') {
            this.connected = false;
            if (this.onComplete) {
                this.onComplete(msg);
            }
            // Server will close the connection
        } else if (msg.type === 'output' || msg.type === 'error') {
            if (this.onMessage) {
                this.onMessage(msg);
            }
        }
    }
}
