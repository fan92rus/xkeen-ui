// services/mode.js - Mode management API

import { get, post } from './api.js';

export async function getModeInfo() {
    return get('/api/config/mode');
}

export async function setMode(mode) {
    return post('/api/config/mode', { mode });
}
