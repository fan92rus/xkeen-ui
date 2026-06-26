// services/awg.js — AWG interface management API
import * as api from './api.js';

export async function listInterfaces() {
    const res = await api.get('/api/awg/interfaces');
    return res.interfaces;
}

export async function upInterface(name) {
    return api.post('/api/awg/up', { name });
}

export async function downInterface(name) {
    return api.post('/api/awg/down', { name });
}

export async function deleteConfig(name) {
    return api.del('/api/awg/config/' + encodeURIComponent(name));
}

export async function uploadConfig(file) {
    const formData = new FormData();
    formData.append('file', file);
    const res = await fetch('/api/awg/upload', {
        method: 'POST',
        credentials: 'same-origin',
        headers: {
            'X-CSRF-Token': api.getCSRFToken(),
        },
        body: formData,
    });
    if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error(data.error || `Upload failed: ${res.status}`);
    }
    return res.json();
}
