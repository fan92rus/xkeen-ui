// services/install.js - AWG installation API
import * as api from './api.js';
import { error as logError } from '../utils/logger.js';

export async function getAWGStatus() {
    return api.get('/api/install/awg/status');
}

export async function setupAWGInit() {
    return api.post('/api/install/awg/init', {});
}

export function uninstallAWG(options) {
    const { onProgress, onComplete, onError } = options;

    return new Promise(async (resolve, reject) => {
        let res;
        try {
            res = await api.request('/api/install/awg/uninstall', { method: 'POST' });
        } catch (e) { reject(e); return; }

        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        try {
            while (true) {
                const { done, value } = await reader.read();
                if (done) { resolve(); return; }

                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n');
                buffer = lines.pop() || '';

                let currentEvent = '';
                for (const line of lines) {
                    if (line.startsWith('event: ')) {
                        currentEvent = line.substring(7);
                    } else if (line.startsWith('data: ')) {
                        try {
                            const data = JSON.parse(line.substring(6));
                            switch (currentEvent) {
                                case 'progress':
                                    onProgress?.(data);
                                    break;
                                case 'error':
                                    onError?.(data);
                                    reject(new Error(data.error));
                                    return;
                            }
                        } catch (e) {
                            logError('Failed to parse SSE data:', e);
                        }
                    }
                }
            }
        } catch (e) {
            reject(e);
        } finally {
            reader.releaseLock();
        }
    });
}

export function installAWG(options) {
    const { onProgress, onComplete, onError } = options;

    return new Promise(async (resolve, reject) => {
        let res;
        try {
            res = await api.request('/api/install/awg/install', { method: 'POST' });
        } catch (e) { reject(e); return; }

        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        try {
            while (true) {
                const { done, value } = await reader.read();
                if (done) { resolve(); return; }

                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n');
                buffer = lines.pop() || '';

                let currentEvent = '';
                for (const line of lines) {
                    if (line.startsWith('event: ')) {
                        currentEvent = line.substring(7);
                    } else if (line.startsWith('data: ')) {
                        try {
                            const data = JSON.parse(line.substring(6));
                            switch (currentEvent) {
                                case 'progress':
                                    onProgress?.(data);
                                    break;
                                case 'complete':
                                    onComplete?.(data);
                                    resolve(data);
                                    return;
                                case 'error':
                                    onError?.(data);
                                    reject(new Error(data.error));
                                    return;
                            }
                        } catch (e) {
                            logError('Failed to parse SSE data:', e);
                        }
                    }
                }
            }
        } catch (e) {
            reject(e);
        } finally {
            reader.releaseLock();
        }
    });
}
