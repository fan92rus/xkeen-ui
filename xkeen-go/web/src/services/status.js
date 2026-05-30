// services/status.js - SSE status streaming

let eventSource = null;

export function connectStatusStream(onStatus) {
    if (eventSource) eventSource.close();

    eventSource = new EventSource('/api/xkeen/status/stream');

    eventSource.addEventListener('status', (e) => {
        try {
            const data = JSON.parse(e.data);
            onStatus(data.running ? 'running' : 'stopped');
        } catch {
            onStatus('unknown');
        }
    });

    eventSource.onerror = () => { /* auto-reconnect */ };

    return () => {
        if (eventSource) { eventSource.close(); eventSource = null; }
    };
}

export function disconnectStatusStream() {
    if (eventSource) { eventSource.close(); eventSource = null; }
}
