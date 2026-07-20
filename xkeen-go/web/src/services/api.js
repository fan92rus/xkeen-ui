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

export async function put(path, data) {
    const res = await request(path, {
        method: 'PUT',
        body: JSON.stringify(data)
    });
    return res.json();
}

export async function del(path, data) {
    const options = { method: 'DELETE' };
    if (data !== undefined) {
        options.body = JSON.stringify(data);
    }
    const res = await request(path, options);
    return res.json().catch(() => ({}));
}

export function getCSRFToken() {
    return document.cookie.match(/csrf_token=([^;]+)/)?.[1] || '';
}

export class ApiError extends Error {
    constructor(status, message, data) {
        super(message);
        this.name = 'ApiError';
        this.status = status;
        this.data = data;
    }
}
