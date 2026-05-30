// services/subscription.js - API client for subscription management

import * as api from './api.js';

const BASE = '/api/subscriptions';

export async function listSubscriptions() {
    return api.get(BASE);
}

export async function addSubscription(data) {
    return api.post(BASE, data);
}

export async function updateSubscription(id, data) {
    return api.put(`${BASE}/${id}`, data);
}

export async function deleteSubscription(id) {
    return api.del(`${BASE}/${id}`);
}

export async function fetchSubscription(id) {
    return api.post(`${BASE}/${id}/fetch`);
}

export async function getProxies() {
    return api.get(`${BASE}/proxies`);
}

export async function getFilters() {
    return api.get(`${BASE}/filters`);
}

export async function updateFilters(filters) {
    return api.put(`${BASE}/filters`, filters);
}

export async function getStrategy() {
    return api.get(`${BASE}/strategy`);
}

export async function updateStrategy(strategy) {
    return api.put(`${BASE}/strategy`, strategy);
}

export async function applySubscriptions() {
    return api.post(`${BASE}/apply`);
}

export async function previewSubscriptions() {
    return api.get(`${BASE}/preview`);
}
