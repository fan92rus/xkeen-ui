// services/install.js — AmneziaWG installation service
// Matches the backend InstallHandler SSE protocol.

import * as api from './api.js';
import { readSSEStream } from '../utils/sse-stream.js';

export async function getAWGStatus() {
    return api.get('/api/install/awg/status');
}

/**
 * Uninstall AmneziaWG via SSE streaming.
 * @param {Object} options - { onProgress, onComplete, onError }
 * @returns {Promise<Object|void>}
 */
export function uninstallAWG(options = {}) {
    const { onProgress, onComplete, onError } = options;
    return api
        .request('/api/install/awg/uninstall', { method: 'POST' })
        .then((res) => readSSEStream(res, { onProgress, onComplete, onError }));
}

/**
 * Install AmneziaWG via SSE streaming.
 * @param {Object} options - { onProgress, onComplete, onError }
 * @returns {Promise<Object|void>}
 */
export function installAWG(options = {}) {
    const { onProgress, onComplete, onError } = options;
    return api
        .request('/api/install/awg/install', { method: 'POST' })
        .then((res) => readSSEStream(res, { onProgress, onComplete, onError }));
}
