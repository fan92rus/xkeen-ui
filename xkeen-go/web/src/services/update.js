// services/update.js — Update API service
import * as api from './api.js';
import { readSSEStream } from '../utils/sse-stream.js';

export async function checkUpdate(prerelease = false) {
    const url = prerelease ? '/api/update/check?prerelease=true' : '/api/update/check';
    return api.get(url);
}

/**
 * Start an update via SSE streaming.
 * @param {Object} options - { prerelease, onProgress, onComplete, onError }
 * @returns {Promise<Object|void>}
 */
export function startUpdate(options = {}) {
    const { prerelease = false, onProgress, onComplete, onError } = options;
    const url = prerelease ? '/api/update/start?prerelease=true' : '/api/update/start';
    return api
        .request(url, { method: 'POST' })
        .then((res) => readSSEStream(res, { onProgress, onComplete, onError }));
}

// --- Auto-update settings ---

export function getAutoUpdate() {
    return api.get('/api/settings/auto-update');
}

export function updateAutoUpdate(enabled) {
    return api.put('/api/settings/auto-update', { enabled });
}
