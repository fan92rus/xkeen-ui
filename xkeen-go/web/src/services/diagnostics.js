// services/diagnostics.js - Network diagnostics API
import * as api from './api.js';

export async function checkNetwork() {
    return api.get('/api/diagnostics/network');
}
