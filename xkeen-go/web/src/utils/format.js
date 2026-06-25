export function fmtTime(t) {
    if (!t || t === '0001-01-01T00:00:00Z') return '';
    return new Date(t).toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
}

export function formatBackupTime(timestamp) {
    if (timestamp == null || timestamp === '' || isNaN(timestamp)) return '—';
    return new Date(timestamp * 1000).toLocaleString();
}
