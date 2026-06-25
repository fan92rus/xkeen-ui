// stores/app.js - Global application state (Pinia)

import { defineStore } from 'pinia';
import { ref, reactive, computed } from 'vue';
import * as configService from '../services/config.js';
import * as xkeenService from '../services/xkeen.js';
import * as logsService from '../services/logs.js';
import * as updateService from '../services/update.js';
import * as statusService from '../services/status.js';
import * as modeService from '../services/mode.js';
import { filterLogs } from '../utils/log-filter.js';
import { computeDiff as computeDiffHtml } from '../utils/diff.js';
import { formatBackupTime } from '../utils/format.js';
import { error as logError } from '../utils/logger.js';

function safeLS(key, fallback) {
    try { return localStorage.getItem(key) ?? fallback; } catch { return fallback; }
}

export const useAppStore = defineStore('app', () => {
    // ── UI state ──
    const activeTab = ref(location.hash.slice(1) || safeLS('xkeen_active_tab', 'editor'));
    const loading = ref(false);

    // ── Toast ──
    const toast = reactive({ message: '', type: '', show: false });
    function showToast(message, type = '') {
        toast.message = message;
        toast.type = type;
        toast.show = true;
        setTimeout(() => { toast.show = false; }, 3000);
    }

    // ── Mode ──
    const currentMode = ref('xray');
    const xrayAvailable = ref(true);
    const mihomoAvailable = ref(false);

    // ── Files ──
    const files = ref([]);
    const currentFile = ref(null);
    const isValidJson = ref(true);
    const lastSavedContent = ref('');
    const editorLoadContent = ref(null);

    // ── Logs ──
    const logs = ref([]);
    const logFilter = ref('all');
    const logSearch = ref('');
    const logFile = ref('/opt/var/log/xray/access.log');
    const filteredLogs = computed(() => filterLogs(logs.value, logFilter.value, logSearch.value));

    // ── Service ──
    const serviceStatus = ref('unknown');

    // ── Xray settings ──
    const xraySettings = reactive({
        logLevel: 'none',
        logLevels: ['debug', 'info', 'warning', 'error', 'none'],
        accessLog: '',
        errorLog: ''
    });

    // ── Update ──
    const currentVersion = ref('unknown');
    const checkDevUpdates = ref(false);
    const updateInfo = reactive({
        update_available: false,
        is_prerelease: false,
        current_version: '',
        latest_version: '',
        release_url: '',
        release_notes: ''
    });
    const updateChecking = ref(false);
    const updating = ref(false);
    const updateProgress = ref(0);
    const updateStatus = ref('');

    // ── Password ──
    const passwordChange = reactive({
        currentPassword: '',
        newPassword: '',
        confirmPassword: '',
        loading: false,
        error: '',
        success: false
    });

    // ── Modals ──
    const modal = reactive({ show: false, command: '', output: '', error: '' });
    const interactiveSession = ref(null);
    const inputValue = ref('');
    const commandComplete = ref(false);

    const confirm = reactive({ show: false, command: '', description: '', onConfirm: null });
    const backupsModal = reactive({ show: false, fileName: '', backups: [], selectedBackup: null, diffContent: '' });
    const diffModal = reactive({ show: false, diffContent: '' });

    // ── Actions: Config ──
    async function loadFiles() {
        try {
            const data = await configService.listFiles(currentMode.value);
            files.value = data;
        } catch { showToast('Не удалось загрузить файлы', 'error'); }
    }

    async function loadFile(path) {
        try {
            const data = await configService.getFile(path);
            if (data.path) {
                currentFile.value = {
                    path: data.path,
                    content: data.content,
                    valid: data.valid,
                    modified: data.modified
                };
                isValidJson.value = data.valid;
                lastSavedContent.value = data.content;
            }
        } catch { showToast('Не удалось загрузить файл', 'error'); }
    }

    async function saveFile(content) {
        if (!currentFile.value) { showToast('Файл не выбран', 'error'); return false; }
        try {
            const data = await configService.saveFile(
                currentFile.value.path,
                content,
                currentFile.value.modified
            );
            // Update local cache after successful save
            currentFile.value.content = content;
            if (data && data.modified !== undefined) {
                currentFile.value.modified = data.modified;
            }
            lastSavedContent.value = content;
            showToast('Сохранено успешно', 'success');
            return true;
        } catch (err) {
            if (err.status === 409) {
                showToast('Файл изменён на диске, перезагружаем…', 'error');
                await loadFile(currentFile.value.path);
                return false;
            }
            showToast(err.message || 'Ошибка сохранения', 'error');
            return false;
        }
    }

    // ── Actions: Mode ──
    async function switchMode(mode) {
        if (mode === currentMode.value) return;
        if (mode === 'mihomo' && !mihomoAvailable.value) { showToast('Mihomo не установлен', 'error'); return; }
        if (mode === 'xray' && !xrayAvailable.value) { showToast('Xray не установлен', 'error'); return; }

        try {
            await modeService.setMode(mode);
            currentFile.value = null;
            currentMode.value = mode;
            logFile.value = mode === 'mihomo' ? '/opt/var/log/mihomo/access.log' : '/opt/var/log/xray/access.log';
            await loadFiles();
            showToast(`Переключено на ${mode}`, 'success');
        } catch (err) {
            showToast(err.message || 'Не удалось переключить режим', 'error');
        }
    }

    async function checkModeAvailability() {
        try {
            const data = await modeService.getModeInfo();
            xrayAvailable.value = data.xray_available;
            mihomoAvailable.value = data.mihomo_available;
            if (data.mode) {
                currentMode.value = data.mode;
                logFile.value = data.mode === 'mihomo' ? '/opt/var/log/mihomo/access.log' : '/opt/var/log/xray/access.log';
            }
        } catch (err) { logError('Failed to check mode availability:', err); }
    }

    // ── Actions: Service ──
    async function fetchServiceStatus() {
        try { serviceStatus.value = await xkeenService.getStatus(); }
        catch { serviceStatus.value = 'unknown'; }
    }

    async function startService() {
        try { await xkeenService.start(); showToast('Запуск сервиса...', 'success'); }
        catch { showToast('Не удалось запустить сервис', 'error'); }
    }

    async function stopService() {
        try { await xkeenService.stop(); showToast('Остановка сервиса...', 'success'); }
        catch { showToast('Не удалось остановить сервис', 'error'); }
    }

    async function restartService() {
        try { await xkeenService.restart(); showToast('Перезапуск Xkeen...', 'success'); }
        catch { showToast('Ошибка перезапуска', 'error'); }
    }

    async function loadXraySettings() {
        try {
            const data = await xkeenService.getSettings();
            if (data.log_level !== undefined) {
                xraySettings.logLevel = data.log_level;
                xraySettings.logLevels = data.log_levels || xraySettings.logLevels;
                xraySettings.accessLog = data.access_log || '';
                xraySettings.errorLog = data.error_log || '';
            }
        } catch { showToast('Не удалось загрузить настройки Xray', 'error'); }
    }

    async function updateLogLevel() {
        try {
            const result = await xkeenService.setLogLevel(xraySettings.logLevel);
            showToast(result.message || 'Уровень логирования обновлён', 'success');
        } catch (err) {
            showToast(err.message || 'Не удалось обновить уровень логирования', 'error');
            loadXraySettings();
        }
    }

    // ── Actions: Logs ──
    async function loadLogs() {
        try { logs.value = await logsService.fetchLogs(logFile.value, 100); }
        catch { showToast('Не удалось загрузить логи', 'error'); }
    }

    function clearLogs() { logs.value = []; }

    // ── Actions: Auth ──
    async function logout() {
        statusService.disconnectStatusStream();
        try { await fetch('/api/auth/logout', { method: 'POST' }); } catch {}
        window.location.href = '/login';
    }

    // ── Actions: Modal ──
    function closeModal() {
        modal.show = false; modal.output = ''; modal.command = ''; modal.error = '';
        if (interactiveSession.value) { interactiveSession.value.close(); interactiveSession.value = null; }
        inputValue.value = '';
        commandComplete.value = false;
    }

    function canSendInput() {
        return interactiveSession.value && interactiveSession.value.connected && !commandComplete.value;
    }

    function sendInput() {
        if (canSendInput() && inputValue.value) {
            interactiveSession.value.send(inputValue.value + '\n');
            inputValue.value = '';
        }
    }

    async function copyModalOutput() {
        try {
            await navigator.clipboard.writeText(modal.output);
            showToast('Вывод скопирован в буфер обмена', 'success');
        } catch { showToast('Не удалось скопировать', 'error'); }
    }

    // ── Actions: Confirm ──
    function cancelConfirm() {
        confirm.show = false; confirm.command = ''; confirm.description = ''; confirm.onConfirm = null;
    }

    function executeConfirm() {
        if (confirm.onConfirm) {
            confirm.show = false;
            confirm.onConfirm();
            confirm.onConfirm = null; confirm.command = ''; confirm.description = '';
        }
    }

    // ── Actions: Backups ──
    async function showBackups() {
        if (!currentFile.value) return;
        try {
            backupsModal.fileName = currentFile.value.path.split('/').pop();
            backupsModal.backups = await configService.getBackups(currentFile.value.path);
            backupsModal.selectedBackup = null;
            backupsModal.diffContent = '';
            backupsModal.show = true;
            if (backupsModal.backups.length > 0) await selectBackup(backupsModal.backups[0]);
        } catch { showToast('Не удалось загрузить резервные копии', 'error'); }
    }

    function closeBackupsModal() {
        backupsModal.show = false; backupsModal.selectedBackup = null; backupsModal.diffContent = '';
    }

    async function selectBackup(backup) {
        backupsModal.selectedBackup = backup;
        try {
            const backupContent = await configService.getBackupContent(backup.path);
            backupsModal.diffContent = computeDiffHtml(lastSavedContent.value || '', backupContent);
        } catch { showToast('Не удалось загрузить содержимое', 'error'); }
    }

    async function copyBackupContent(backup) {
        try {
            const content = await configService.getBackupContent(backup.path);
            await navigator.clipboard.writeText(content);
            showToast('Резервная копия скопирована', 'success');
        } catch { showToast('Не удалось скопировать', 'error'); }
    }

    async function loadBackupToEditor(backup) {
        try {
            const content = await configService.getBackupContent(backup.path);
            editorLoadContent.value = content;
            closeBackupsModal();
            showToast('Резервная копия загружена в редактор', 'success');
        } catch { showToast('Не удалось загрузить', 'error'); }
    }

    // formatBackupTime imported from ../utils/format.js

    // ── Actions: Diff ──
    function openDiffModal(currentContent, savedContent) {
        if (currentContent === savedContent) { showToast('Нет изменений с последнего сохранения'); return; }
        diffModal.diffContent = computeDiffHtml(currentContent, savedContent);
        diffModal.show = true;
    }

    function closeDiffModal() { diffModal.show = false; diffModal.diffContent = ''; }

    // ── Actions: Update ──
    async function checkUpdate() {
        updateChecking.value = true;
        try {
            const data = await updateService.checkUpdate(checkDevUpdates.value);
            currentVersion.value = data.current_version;
            Object.assign(updateInfo, {
                update_available: data.update_available,
                is_prerelease: data.is_prerelease || false,
                current_version: data.current_version,
                latest_version: data.latest_version,
                release_url: data.release_url || '',
                release_notes: data.release_notes || ''
            });
            if (data.error) showToast('Проверка обновлений: ' + data.error, 'error');
        } catch { showToast('Не удалось проверить обновления', 'error'); }
        finally { updateChecking.value = false; }
    }

    async function startUpdate() {
        updating.value = true; updateProgress.value = 0; updateStatus.value = 'Запуск обновления...';
        try {
            await updateService.startUpdate({
                prerelease: checkDevUpdates.value,
                onProgress: (data) => { updateProgress.value = data.percent; updateStatus.value = data.status; },
                onComplete: (data) => { showToast(data.message || 'Обновление завершено!', 'success'); },
                onError: (data) => { showToast('Ошибка обновления: ' + data.error, 'error'); updating.value = false; }
            });
        } catch (err) {
            showToast('Ошибка обновления: ' + err.message, 'error');
            updating.value = false;
        }
    }

    // ── Actions: Password ──
    async function changePassword() {
        const p = passwordChange;
        if (!p.currentPassword || !p.newPassword || !p.confirmPassword) { showToast('Все поля пароля обязательны', 'error'); return false; }
        if (p.newPassword.length < 8) { showToast('Новый пароль должен содержать минимум 8 символов', 'error'); return false; }
        if (p.newPassword !== p.confirmPassword) { showToast('Новые пароли не совпадают', 'error'); return false; }
        if (p.currentPassword === p.newPassword) { showToast('Новый пароль должен отличаться от текущего', 'error'); return false; }

        p.loading = true; p.error = ''; p.success = false;
        try {
            const csrfToken = document.cookie.match(/csrf_token=([^;]+)/)?.[1] || '';
            const response = await fetch('/api/auth/change-password', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
                body: JSON.stringify({ current_password: p.currentPassword, new_password: p.newPassword })
            });
            const data = await response.json();
            if (!response.ok || !data.ok) {
                p.error = data.error || 'Не удалось изменить пароль';
                showToast(p.error, 'error');
                return false;
            }
            p.success = true;
            showToast('Пароль успешно изменён', 'success');
            clearPasswordForm();
            return true;
        } catch (err) {
            p.error = err.message || 'Не удалось изменить пароль';
            showToast(p.error, 'error');
            return false;
        } finally { p.loading = false; }
    }

    function clearPasswordForm() {
        passwordChange.currentPassword = '';
        passwordChange.newPassword = '';
        passwordChange.confirmPassword = '';
        passwordChange.error = '';
        passwordChange.success = false;
    }

    // ── Init ──
    async function init() {
        await checkModeAvailability();
        loadFiles();
        loadXraySettings();
        checkUpdate();
        statusService.connectStatusStream((status) => { serviceStatus.value = status; });
    }

    return {
        // State
        activeTab, loading, toast,
        currentMode, xrayAvailable, mihomoAvailable,
        files, currentFile, isValidJson, lastSavedContent, editorLoadContent,
        logs, logFilter, logSearch, logFile, filteredLogs,
        serviceStatus,
        xraySettings,
        currentVersion, checkDevUpdates, updateInfo, updateChecking, updating, updateProgress, updateStatus,
        passwordChange,
        modal, interactiveSession, inputValue, commandComplete,
        confirm, backupsModal, diffModal,
        // Actions
        showToast, loadFiles, loadFile, saveFile,
        switchMode, checkModeAvailability,
        fetchServiceStatus, startService, stopService, restartService, loadXraySettings, updateLogLevel,
        loadLogs, clearLogs,
        logout,
        closeModal, canSendInput, sendInput, copyModalOutput,
        cancelConfirm, executeConfirm,
        showBackups, closeBackupsModal, selectBackup, copyBackupContent, loadBackupToEditor, formatBackupTime,
        openDiffModal, closeDiffModal,
        checkUpdate, startUpdate,
        changePassword, clearPasswordForm,
        init
    };
});
