// services/metrics.js - API client for Xray metrics

import * as api from './api.js';

export const getMetricsStats = () => api.get('/api/metrics/stats');
export const getMetricsObservatory = () => api.get('/api/metrics/observatory');

// Settings
export const getMetricsPort = () => api.get('/api/settings/metrics');
export const updateMetricsPort = (port) => api.put('/api/settings/metrics', { metrics_port: port });
