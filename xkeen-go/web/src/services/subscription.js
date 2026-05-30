// services/subscription.js - API client for subscription management

import * as api from './api.js';

const BASE = '/api/subscriptions';

export const listSubscriptions = () => api.get(BASE);
export const addSubscription = (data) => api.post(BASE, data);
export const updateSubscription = (id, data) => api.put(`${BASE}/${id}`, data);
export const deleteSubscription = (id) => api.del(`${BASE}/${id}`);
export const fetchSubscription = (id) => api.post(`${BASE}/${id}/fetch`);
export const getProxies = () => api.get(`${BASE}/proxies`);
export const getFilters = () => api.get(`${BASE}/filters`);
export const updateFilters = (filters) => api.put(`${BASE}/filters`, filters);
export const getStrategy = () => api.get(`${BASE}/strategy`);
export const updateStrategy = (strategy) => api.put(`${BASE}/strategy`, strategy);
export const applySubscriptions = () => api.post(`${BASE}/apply`);
export const previewSubscriptions = () => api.get(`${BASE}/preview`);
export const getAutoApply = () => api.get(`${BASE}/auto-apply`);
export const updateAutoApply = (data) => api.put(`${BASE}/auto-apply`, data);
