// utils/sse-stream.js — Shared SSE (Server-Sent Events) stream reader.
//
// Backend endpoints (/api/install/awg/install, /api/update/start, etc.)
// respond with SSE protocol: lines prefixed with "event: <type>" and
// "data: <json>".  This utility parses the stream and dispatches to
// callbacks, replacing 4 near-identical inline implementations.

import { error as logError } from './logger.js';

/**
 * Read an SSE stream from a fetch Response.
 *
 * @param {Response} response - The fetch Response object (with a readable body).
 * @param {Object} handlers - Event handlers.
 * @param {function(Object): void} [handlers.onProgress] - Called on 'progress' events.
 * @param {function(Object): void} [handlers.onComplete] - Called on 'complete' events.
 * @param {function(Object): void} [handlers.onError] - Called on 'error' events.
 * @returns {Promise<Object|void>} Resolves with the complete event data, or void if the stream ends without one.
 * @rejects {Error} On stream read error or backend 'error' event.
 */
export function readSSEStream(response, { onProgress, onComplete, onError } = {}) {
    return new Promise((resolve, reject) => {
        (async () => {
            const reader = response.body.getReader();
            const decoder = new TextDecoder();
            let buffer = '';
            let currentEvent = '';  // persists across read chunks

            try {
                while (true) {
                    const { done, value } = await reader.read();
                    if (done) {
                        // Stream ended without an explicit 'complete' event.
                        // Treat as success (some endpoints just close the stream).
                        onComplete?.({ percent: 100, status: 'complete' });
                        resolve();
                        return;
                    }

                    buffer += decoder.decode(value, { stream: true });
                    const lines = buffer.split('\n');
                    buffer = lines.pop() || '';

                    for (const line of lines) {
                        if (line.startsWith('event: ')) {
                            currentEvent = line.substring(7);
                        } else if (line.startsWith('data: ')) {
                            let data;
                            try {
                                data = JSON.parse(line.substring(6));
                            } catch (e) {
                                logError('Failed to parse SSE data:', e);
                                continue; // skip malformed line, keep reading
                            }
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
                                    reject(new Error(data.error || 'Unknown error'));
                                    return;
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
