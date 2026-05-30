// services/update.js - Update API service

const API_BASE = '/api';

export async function checkUpdate(prerelease = false) {
    const url = prerelease
        ? `${API_BASE}/update/check?prerelease=true`
        : `${API_BASE}/update/check`;

    const response = await fetch(url, {
        method: 'GET',
        headers: { 'Content-Type': 'application/json' }
    });

    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    return response.json();
}

function getCSRFToken() {
    return document.cookie.match(/csrf_token=([^;]+)/)?.[1] || '';
}

export function startUpdate(options) {
    const { prerelease = false, onProgress, onComplete, onError } = options;

    return new Promise((resolve, reject) => {
        const url = prerelease
            ? `${API_BASE}/update/start?prerelease=true`
            : `${API_BASE}/update/start`;

        fetch(url, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': getCSRFToken()
            }
        }).then(response => {
            if (!response.ok) {
                reject(new Error(`HTTP ${response.status}`));
                return;
            }

            const reader = response.body.getReader();
            const decoder = new TextDecoder();
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
