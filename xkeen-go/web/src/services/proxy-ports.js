// services/proxy-ports.js - Proxy port list API (port_proxying.lst / port_exclude.lst)

import { get, put } from './api.js';

export async function getProxyPorts() {
	return get('/api/settings/proxy-ports');
}

export async function updateProxyPorts(payload) {
	return put('/api/settings/proxy-ports', payload);
}
