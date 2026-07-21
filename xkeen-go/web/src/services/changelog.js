import { get } from './api.js';

export function getChangelog() {
    return get('/api/changelog');
}
