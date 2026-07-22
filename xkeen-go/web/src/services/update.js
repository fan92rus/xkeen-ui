// services/update.js — Update API service
import * as api from './api.js';
import { readSSEStream } from '../utils/sse-stream.js';

export async function checkUpdate(prerelease = false, branch = '') {
    let url = '/api/update/check';
    const params = [];
    if (prerelease) params.push('prerelease=true');
    if (branch) params.push('branch=' + encodeURIComponent(branch));
    url = params.length ? url + '?' + params.join('&') : url;
    return api.get(url);
}

export async function getBranches() {
    return api.get('/api/update/branches');
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
