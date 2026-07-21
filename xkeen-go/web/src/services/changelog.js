import { get, post } from './api.js';

export function getWhatsNew() {
    return get('/api/changelog/whatsnew');
}

export function ackWhatsNew() {
    return post('/api/changelog/ack', {});
}

export function getChangelog() {
    return get('/api/changelog');
}
