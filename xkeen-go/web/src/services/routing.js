// services/routing.js - Routing settings API (proxy_entware)
import * as api from './api.js';

export async function getProxyEntware() {
    return api.get('/api/settings/proxy-entware');
}

export async function setProxyEntware(enabled) {
    return api.post('/api/settings/proxy-entware', { enabled });
}
