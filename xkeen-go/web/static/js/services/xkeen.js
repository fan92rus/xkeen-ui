// services/xkeen.js - XKeen service control

import { get, post } from './api.js';

export async function getStatus() {
    const data = await get('/api/xkeen/status');
    if (data.status && data.status.running !== undefined) {
        return data.status.running ? 'running' : 'stopped';
    }
    return 'unknown';
}

export async function start() {
    return post('/api/xkeen/start', {});
}

export async function stop() {
    return post('/api/xkeen/stop', {});
}

export async function restart() {
    return post('/api/xkeen/restart', {});
}

export async function getSettings() {
    return get('/api/xray/settings');
}

export async function setLogLevel(level) {
    return post('/api/xray/settings/log-level', { log_level: level });
}
