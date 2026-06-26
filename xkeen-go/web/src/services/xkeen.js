// services/xkeen.js - XKeen service control

import { get, post, request, put } from './api.js';

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

export async function getStatus() {
    const data = await get('/api/xkeen/status');
    return data.status || 'unknown';
}

/**
 * Fetch the list of available XKeen commands from the backend registry
 * (generated from `xkeen -help`). Returns the flat list as the backend sends
 * it: [{ cmd, description, category, dangerous }]. Empty when xkeen is not
 * installed. Use utils/commands-grouping.js to shape it for rendering.
 */
export async function getCommands() {
    const data = await get('/api/xkeen/commands');
    return { commands: data.commands || [], error: data.error || '' };
}

export async function refreshCommands() {
    const data = await post('/api/xkeen/commands/refresh', {});
    return { commands: data.commands || [], error: data.error || '' };
}

// --- AWG interface settings ---

export async function getAWGInterfaces() {
    return get('/api/settings/awg-interfaces');
}

export async function updateAWGInterfaces(lanIface, wanIface) {
    return put('/api/settings/awg-interfaces', { lan_iface: lanIface, wan_iface: wanIface });
}
