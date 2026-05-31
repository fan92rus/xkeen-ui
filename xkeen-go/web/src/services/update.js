// services/update.js - Update API service
import * as api from './api.js';

export async function checkUpdate(prerelease = false) {
    const url = prerelease ? '/api/update/check?prerelease=true' : '/api/update/check';
    return api.get(url);
}

export function startUpdate(options) {
    const { prerelease = false, onProgress, onComplete, onError } = options;
    const url = prerelease ? '/api/update/start?prerelease=true' : '/api/update/start';

    return new Promise((resolve, reject) => {
        api.postStream(url).then(({ reader, decoder }) => {
            let buffer = '';

            function read() {
                reader.read().then(({ done, value }) => {
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
                                console.error('Failed to parse SSE data:', e);
                            }
                        }
                    }
                    read();
                }).catch(reject);
            }
            read();
        }).catch(reject);
    });
}
