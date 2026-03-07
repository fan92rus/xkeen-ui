// services/config.js - Config file operations

import { get, post } from './api.js';

export async function listFiles() {
    const data = await get('/api/config/files');
    return data.files || [];
}

export async function getFile(path) {
    return get(`/api/config/file?path=${encodeURIComponent(path)}`);
}

export async function saveFile(path, content) {
    return post('/api/config/file', { path, content });
}

export async function getBackups(filePath) {
    const data = await get(`/api/config/backups?path=${encodeURIComponent(filePath)}`);
    return data.backups || [];
}

export async function getBackupContent(backupPath) {
    const data = await get(`/api/config/backups/content?backup_path=${encodeURIComponent(backupPath)}`);
    return data.content || '';
}
