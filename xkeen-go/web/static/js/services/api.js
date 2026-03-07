// services/api.js - Base HTTP client with CSRF and error handling

const API_BASE = '';

export async function request(path, options = {}) {
    const csrfToken = document.cookie.match(/csrf_token=([^;]+)/)?.[1] || '';

    const res = await fetch(API_BASE + path, {
        ...options,
        headers: {
            'Content-Type': 'application/json',
            'X-CSRF-Token': csrfToken,
            ...options.headers
        }
    });

    if (!res.ok) {
        const error = await res.json().catch(() => ({}));
        throw new ApiError(res.status, error.message || error.error || 'Request failed', error);
    }

    return res;
}

export async function get(path) {
    const res = await request(path);
    return res.json();
}

export async function post(path, data) {
    const res = await request(path, {
        method: 'POST',
        body: JSON.stringify(data)
    });
    return res.json();
}

export async function postStream(path, data, onMessage) {
    const res = await request(path, {
        method: 'POST',
        body: JSON.stringify(data)
    });

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    try {
        while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop();

        for (const line of lines) {
            if (line.trim()) {
                try {
                    onMessage(JSON.parse(line));
                } catch (e) {
                    console.warn('Failed to parse stream line:', line);
                }
            }
        }
    }

    if (buffer.trim()) {
        try {
            onMessage(JSON.parse(buffer));
        } catch (e) {
            console.warn('Failed to parse final buffer:', buffer);
        }
    }
    } finally {
    reader.releaseLock();
    }
}

export class ApiError extends Error {
    constructor(status, message, data) {
        super(message);
        this.name = 'ApiError';
        this.status = status;
        this.data = data;
    }
}
