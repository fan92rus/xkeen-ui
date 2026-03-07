// update.js - Update API service

const API_BASE = '/api';

/**
 * Check for available updates
 * @returns {Promise<Object>} Update info with current_version, latest_version, update_available, etc.
 */
export async function checkUpdate() {
    const response = await fetch(`${API_BASE}/update/check`, {
        method: 'GET',
        headers: {
            'Content-Type': 'application/json'
        }
    });

    if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
    }

    return response.json();
}

/**
 * Start update and listen to SSE events
 * @param {Object} callbacks - Event callbacks
 * @param {Function} callbacks.onProgress - Called with {percent, status}
 * @param {Function} callbacks.onComplete - Called with {success, message}
 * @param {Function} callbacks.onError - Called with {error}
 * @returns {Promise<void>}
 */
export function startUpdate(callbacks) {
    return new Promise((resolve, reject) => {
        // Use fetch with POST and manually parse SSE
        fetch(`${API_BASE}/update/start`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
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
                    if (done) {
                        resolve();
                        return;
                    }

                    buffer += decoder.decode(value, { stream: true });

                    // Parse SSE events
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
                                        if (callbacks.onProgress) {
                                            callbacks.onProgress(data);
                                        }
                                        break;
                                    case 'complete':
                                        if (callbacks.onComplete) {
                                            callbacks.onComplete(data);
                                        }
                                        resolve(data);
                                        return;
                                    case 'error':
                                        if (callbacks.onError) {
                                            callbacks.onError(data);
                                        }
                                        reject(new Error(data.error));
                                        return;
                                }
                            } catch (e) {
                                console.error('Failed to parse SSE data:', e);
                            }
                        }
                    }

                    read();
                }).catch(err => {
                    reject(err);
                });
            }

            read();
        }).catch(err => {
            reject(err);
        });
    });
}
