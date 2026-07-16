// services/update.js - Update API service
import { error as logError } from '../utils/logger.js';
import * as api from './api.js';

export async function checkUpdate(prerelease = false) {
    const url = prerelease ? '/api/update/check?prerelease=true' : '/api/update/check';
    return api.get(url);
}

export function startUpdate(options) {
    const { prerelease = false, onProgress, onComplete, onError } = options;
    const url = prerelease ? '/api/update/start?prerelease=true' : '/api/update/start';

    return new Promise((resolve, reject) => {
        (async () => {
            let res;
            try {
                res = await api.request(url, { method: 'POST' });
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
                                    case 'progress': onProgress?.(data); break;
                                    case 'complete': onComplete?.(data); resolve(data); return;
                                    case 'error': onError?.(data); reject(new Error(data.error)); return;
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
        })();
    });
}
