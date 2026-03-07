// services/logs.js - Logs fetching and WebSocket streaming

import { get } from './api.js';

export async function fetchLogs(path, lines = 100) {
    const data = await get(`/api/logs/xray?path=${encodeURIComponent(path)}&lines=${lines}`);
    return data.entries || [];
}

export function createLogStream(onMessage, onError) {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/ws/logs`;

    const ws = new WebSocket(wsUrl);

    ws.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            if (data.type !== 'ping') {
                onMessage(data);
            }
        } catch (e) {
            console.warn('Failed to parse WebSocket message:', e);
        }
    };

    ws.onerror = () => {
        onError?.();
    };

    return {
        close: () => ws.close(),
        isOpen: () => ws.readyState === WebSocket.OPEN
    };
}
