// store.js - Global Alpine store for application state

import * as configService from './services/config.js';
import * as xkeenService from './services/xkeen.js';
import * as logsService from './services/logs.js';
import * as updateService from './services/update.js';
import * as statusService from './services/status.js';
import * as modeService from './services/mode.js';
import { get } from './services/api.js';

/**
 * Get CSRF token from cookie
 * @returns {string} CSRF token
 */
function getCsrfToken() {
    return document.cookie.match(/csrf_token=([^;]+)/)?.[1] || '';
}

document.addEventListener('alpine:init', () => {
    Alpine.store('app', {
        // UI state
        activeTab: 'editor',
        toast: { message: '', type: '', show: false },
        loading: false,

        // Mode state
        currentMode: 'xray',        // 'xray' | 'mihomo'
        xrayAvailable: true,
        mihomoAvailable: false,

        // Data
        files: [],
        currentFile: null,
        logs: [],
        serviceStatus: 'unknown',
        settings: {},
        isValidJson: true,
        lastSavedContent: '',  // Content of last saved file for diff

        // Logs state
        logFilter: 'all',
        logSearch: '',
        logFile: '/opt/var/log/xray/access.log',

        // Xray settings
        xraySettings: {
            logLevel: 'none',
            logLevels: ['debug', 'info', 'warning', 'error', 'none'],
            accessLog: '',
            errorLog: ''
        },

        // Update state
        currentVersion: 'unknown',
        checkDevUpdates: false,
        updateInfo: {
            update_available: false,
            is_prerelease: false,
            current_version: '',
            latest_version: '',
            release_url: '',
            release_notes: ''
        },
        updateChecking: false,
        updating: false,
        updateProgress: 0,
        updateStatus: '',

        // Password change state
        passwordChange: {
            currentPassword: '',
            newPassword: '',
            confirmPassword: '',
            loading: false,
            error: '',
            success: false
        },

        // Modal state
        modal: {
            show: false,
            command: '',
            output: '',
            error: ''
        },

        // Interactive command state
        interactiveSession: null,
        inputValue: '',
        commandComplete: false,

        // Confirm dialog state
        confirm: {
            show: false,
            command: '',
            description: '',
            onConfirm: null
        },

        // Backups modal state
        backupsModal: {
            show: false,
            fileName: '',
            backups: [],
            selectedBackup: null,
            diffContent: ''
        },

        // Diff modal state
        diffModal: {
            show: false,
            diffContent: ''  // Computed diff text with +/- markers
        },

        // Computed: filtered logs
        get filteredLogs() {
            let logs = this.logs;

            if (this.logFilter !== 'all') {
                logs = logs.filter(log => log.level === this.logFilter);
            }

            if (this.logSearch) {
                const term = this.logSearch.toLowerCase();
                logs = logs.filter(log =>
                    log.message.toLowerCase().includes(term)
                );
            }

            return logs;
        },

        // Toast
        showToast(message, type = '') {
            this.toast = { message, type, show: true };
            setTimeout(() => {
                this.toast.show = false;
            }, 3000);
        },

        // Config actions
        async loadFiles() {
            try {
                const data = await get(`/api/config/files?mode=${this.currentMode}`);
                this.files = data.files || [];
            } catch (err) {
                this.showToast('Не удалось загрузить файлы', 'error');
            }
        },

        async switchMode(mode) {
            if (mode === this.currentMode) {
                return;
            }

            if (mode === 'mihomo' && !this.mihomoAvailable) {
                this.showToast('Mihomo не установлен', 'error');
                return;
            }
            if (mode === 'xray' && !this.xrayAvailable) {
                this.showToast('Xray не установлен', 'error');
                return;
            }

            try {
                // Save mode to backend
                await modeService.setMode(mode);

                const previousMode = this.currentMode;
                this.currentFile = null;
                this.currentMode = mode;

                // Update default log file based on mode
                if (mode === 'mihomo') {
                    this.logFile = '/opt/var/log/mihomo/access.log';
                } else {
                    this.logFile = '/opt/var/log/xray/access.log';
                }

                // Reload files for new mode
                await this.loadFiles();

                // Dispatch mode change event for editor
                window.dispatchEvent(new CustomEvent('mode:change', { detail: mode }));

                this.showToast(`Переключено на ${mode}`, 'success');
            } catch (err) {
                this.showToast(err.message || 'Не удалось переключить режим', 'error');
            }
        },

        async checkModeAvailability() {
            try {
                const data = await modeService.getModeInfo();
                this.xrayAvailable = data.xray_available;
                this.mihomoAvailable = data.mihomo_available;
                // Load saved mode from backend
                if (data.mode) {
                    this.currentMode = data.mode;
                    // Update default log file based on mode
                    if (data.mode === 'mihomo') {
                        this.logFile = '/opt/var/log/mihomo/access.log';
                    } else {
                        this.logFile = '/opt/var/log/xray/access.log';
                    }
                }
            } catch (err) {
                console.error('Failed to check mode availability:', err);
            }
        },

        async loadFile(path) {
            try {
                const data = await configService.getFile(path);
                if (data.path) {
                    this.currentFile = {
                        path: data.path,
                        content: data.content,
                        valid: data.valid
                    };
                    this.isValidJson = data.valid;
                    this.lastSavedContent = data.content;  // Store for diff
                }
            } catch (err) {
                this.showToast('Не удалось загрузить файл', 'error');
            }
        },

        async saveFile(content) {
            if (!this.currentFile) {
                this.showToast('Файл не выбран', 'error');
                return false;
            }

            try {
                await configService.saveFile(this.currentFile.path, content);
                this.lastSavedContent = content;  // Update after successful save
                this.showToast('Сохранено успешно', 'success');
                return true;
            } catch (err) {
                this.showToast(err.message || 'Ошибка сохранения', 'error');
                return false;
            }
        },

        // XKeen actions
        async fetchServiceStatus() {
            try {
                this.serviceStatus = await xkeenService.getStatus();
            } catch (err) {
                this.serviceStatus = 'unknown';
            }
        },

        async startService() {
            try {
                await xkeenService.start();
                this.showToast('Запуск сервиса...', 'success');
                // Status will be updated via SSE
            } catch (err) {
                this.showToast('Не удалось запустить сервис', 'error');
            }
        },

        async stopService() {
            try {
                await xkeenService.stop();
                this.showToast('Остановка сервиса...', 'success');
                // Status will be updated via SSE
            } catch (err) {
                this.showToast('Не удалось остановить сервис', 'error');
            }
        },

        async restartService() {
            try {
                await xkeenService.restart();
                this.showToast('Перезапуск Xkeen...', 'success');
            } catch (err) {
                this.showToast('Ошибка перезапуска', 'error');
            }
        },

        async loadXraySettings() {
            try {
                const data = await xkeenService.getSettings();
                if (data.log_level !== undefined) {
                    this.xraySettings.logLevel = data.log_level;
                    this.xraySettings.logLevels = data.log_levels || this.xraySettings.logLevels;
                    this.xraySettings.accessLog = data.access_log || '';
                    this.xraySettings.errorLog = data.error_log || '';
                }
            } catch (err) {
                this.showToast('Не удалось загрузить настройки Xray', 'error');
            }
        },

        async updateLogLevel() {
            try {
                const result = await xkeenService.setLogLevel(this.xraySettings.logLevel);
                this.showToast(result.message || 'Уровень логирования обновлён', 'success');
            } catch (err) {
                this.showToast(err.message || 'Не удалось обновить уровень логирования', 'error');
                this.loadXraySettings();
            }
        },

        // Logs actions
        async loadLogs() {
            try {
                this.logs = await logsService.fetchLogs(this.logFile, 100);
            } catch (err) {
                this.showToast('Не удалось загрузить логи', 'error');
            }
        },

        clearLogs() {
            this.logs = [];
        },

        // Auth
        async logout() {
            try {
                await fetch('/api/auth/logout', { method: 'POST' });
            } catch (err) {
                // Ignore logout errors
            }
            window.location.href = '/login';
        },

        // Modal actions
        closeModal() {
            this.modal.show = false;
            this.modal.output = '';
            this.modal.command = '';
            this.modal.error = '';
            // Reset interactive state
            if (this.interactiveSession) {
                this.interactiveSession.close();
                this.interactiveSession = null;
            }
            this.inputValue = '';
            this.commandComplete = false;
        },

        // Interactive command methods
        canSendInput() {
            return this.interactiveSession && this.interactiveSession.connected && !this.commandComplete;
        },

        sendInput() {
            if (this.canSendInput() && this.inputValue) {
                this.interactiveSession.send(this.inputValue + '\n');
                this.inputValue = '';
            }
        },

        async copyModalOutput() {
            try {
                await navigator.clipboard.writeText(this.modal.output);
                this.showToast('Вывод скопирован в буфер обмена', 'success');
            } catch (err) {
                this.showToast('Не удалось скопировать в буфер обмена', 'error');
            }
        },

        cancelConfirm() {
            this.confirm.show = false;
            this.confirm.command = '';
            this.confirm.description = '';
            this.confirm.onConfirm = null;
        },

        executeConfirm() {
            if (this.confirm.onConfirm) {
                this.confirm.show = false;
                this.confirm.onConfirm();
                this.confirm.onConfirm = null;
                this.confirm.command = '';
                this.confirm.description = '';
            }
        },

        // Backups modal actions
        async showBackups() {
            if (!this.currentFile) return;

            try {
                this.backupsModal.fileName = this.currentFile.path.split('/').pop();
                this.backupsModal.backups = await configService.getBackups(this.currentFile.path);
                this.backupsModal.selectedBackup = null;
                this.backupsModal.diffContent = '';
                this.backupsModal.show = true;

                // Auto-select first backup if available
                if (this.backupsModal.backups.length > 0) {
                    await this.selectBackup(this.backupsModal.backups[0]);
                }
            } catch (err) {
                this.showToast('Не удалось загрузить резервные копии', 'error');
            }
        },

        closeBackupsModal() {
            this.backupsModal.show = false;
            this.backupsModal.selectedBackup = null;
            this.backupsModal.diffContent = '';
        },

        async selectBackup(backup) {
            this.backupsModal.selectedBackup = backup;

            try {
                // Get backup content and show diff
                const backupContent = await configService.getBackupContent(backup.path);
                const currentContent = this.currentFile?.content || '';

                // Compute simple diff
                this.backupsModal.diffContent = this.computeDiff(currentContent, backupContent);
            } catch (err) {
                this.showToast('Не удалось загрузить содержимое резервной копии', 'error');
            }
        },

        async copyBackupContent(backup) {
            try {
                const content = await configService.getBackupContent(backup.path);
                await navigator.clipboard.writeText(content);
                this.showToast('Резервная копия скопирована в буфер обмена', 'success');
            } catch (err) {
                this.showToast('Не удалось скопировать резервную копию', 'error');
            }
        },

        async loadBackupToEditor(backup) {
            try {
                const content = await configService.getBackupContent(backup.path);
                // Dispatch event for editor to handle
                window.dispatchEvent(new CustomEvent('editor:loadContent', { detail: content }));
                this.closeBackupsModal();
                this.showToast('Резервная копия загружена в редактор', 'success');
            } catch (err) {
                this.showToast('Не удалось загрузить резервную копию', 'error');
            }
        },

        formatBackupTime(timestamp) {
            const date = new Date(timestamp * 1000);
            return date.toLocaleString();
        },

        // Diff modal actions
        openDiffModal(currentContent, savedContent) {
            if (currentContent === savedContent) {
                this.showToast('Нет изменений с последнего сохранения', '');
                return;
            }
            this.diffModal.diffContent = this.computeDiff(currentContent, savedContent);
            this.diffModal.show = true;
        },

        closeDiffModal() {
            this.diffModal.show = false;
            this.diffModal.diffContent = '';
        },

        // Diff computation using LCS (Longest Common Subsequence) algorithm
        // Returns HTML with colored +/- markers
        computeDiff(a, b) {
            const linesA = a.split('\n');
            const linesB = b.split('\n');

            const escapeHtml = (str) => {
                return str.replace(/&/g, '&amp;')
                          .replace(/</g, '&lt;')
                          .replace(/>/g, '&gt;');
            };

            // Build LCS (Longest Common Subsequence) matrix
            const m = linesA.length;
            const n = linesB.length;

            // Create a matrix to store LCS lengths
            const dp = Array(m + 1).fill(null).map(() => Array(n + 1).fill(0));

            // Fill the matrix
            for (let i = 1; i <= m; i++) {
                for (let j = 1; j <= n; j++) {
                    if (linesA[i - 1] === linesB[j - 1]) {
                        dp[i][j] = dp[i - 1][j - 1] + 1;
                    } else {
                        dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
                    }
                }
            }

            // Backtrack to find the diff
            const result = [];
            let i = m, j = n;

            // Collect operations in reverse order
            const ops = [];
            while (i > 0 || j > 0) {
                if (i > 0 && j > 0 && linesA[i - 1] === linesB[j - 1]) {
                    ops.push({ type: 'equal', line: linesA[i - 1] });
                    i--;
                    j--;
                } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
                    ops.push({ type: 'removed', line: linesB[j - 1] });
                    j--;
                } else if (i > 0) {
                    ops.push({ type: 'added', line: linesA[i - 1] });
                    i--;
                }
            }

            // Reverse to get correct order and format
            for (let k = ops.length - 1; k >= 0; k--) {
                const op = ops[k];
                const escaped = escapeHtml(op.line);
                if (op.type === 'equal') {
                    result.push('  ' + escaped);
                } else if (op.type === 'removed') {
                    result.push('<span class="diff-removed">- ' + escaped + '</span>');
                } else {
                    result.push('<span class="diff-added">+ ' + escaped + '</span>');
                }
            }

            return result.join('\n');
        },

        // Update actions
        async checkUpdate() {
            this.updateChecking = true;
            try {
                const prerelease = this.checkDevUpdates;
                const data = await updateService.checkUpdate(prerelease);
                this.currentVersion = data.current_version;
                this.updateInfo = {
                    update_available: data.update_available,
                    is_prerelease: data.is_prerelease || false,
                    current_version: data.current_version,
                    latest_version: data.latest_version,
                    release_url: data.release_url || '',
                    release_notes: data.release_notes || ''
                };
                if (data.error) {
                    this.showToast('Проверка обновлений: ' + data.error, 'error');
                }
            } catch (err) {
                this.showToast('Не удалось проверить обновления', 'error');
            } finally {
                this.updateChecking = false;
            }
        },

        async startUpdate() {
            this.updating = true;
            this.updateProgress = 0;
            this.updateStatus = 'Запуск обновления...';

            try {
                const prerelease = this.checkDevUpdates;
                await updateService.startUpdate({
                    prerelease: prerelease,
                    onProgress: (data) => {
                        this.updateProgress = data.percent;
                        this.updateStatus = data.status;
                    },
                    onComplete: (data) => {
                        this.showToast(data.message || 'Обновление завершено!', 'success');
                        // Page will reload when service restarts
                    },
                    onError: (data) => {
                        this.showToast('Ошибка обновления: ' + data.error, 'error');
                        this.updating = false;
                    }
                });
            } catch (err) {
                this.showToast('Ошибка обновления: ' + err.message, 'error');
                this.updating = false;
            }
        },

        // Password change actions
        async changePassword() {
            // Client-side validation
            if (!this.passwordChange.currentPassword || !this.passwordChange.newPassword || !this.passwordChange.confirmPassword) {
                this.showToast('Все поля пароля обязательны', 'error');
                return false;
            }

            if (this.passwordChange.newPassword.length < 8) {
                this.showToast('Новый пароль должен содержать минимум 8 символов', 'error');
                return false;
            }

            if (this.passwordChange.newPassword !== this.passwordChange.confirmPassword) {
                this.showToast('Новые пароли не совпадают', 'error');
                return false;
            }

            if (this.passwordChange.currentPassword === this.passwordChange.newPassword) {
                this.showToast('Новый пароль должен отличаться от текущего', 'error');
                return false;
            }

            this.passwordChange.loading = true;
            this.passwordChange.error = '';
            this.passwordChange.success = false;

            try {
                const response = await fetch('/api/auth/change-password', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'X-CSRF-Token': getCsrfToken()
                    },
                    body: JSON.stringify({
                        current_password: this.passwordChange.currentPassword,
                        new_password: this.passwordChange.newPassword
                    })
                });

                const data = await response.json();

                if (!response.ok || !data.ok) {
                    this.passwordChange.error = data.error || 'Не удалось изменить пароль';
                    this.showToast(data.error || 'Не удалось изменить пароль', 'error');
                    return false;
                }

                this.passwordChange.success = true;
                this.showToast('Пароль успешно изменён', 'success');
                this.clearPasswordForm();
                return true;
            } catch (err) {
                this.passwordChange.error = err.message || 'Не удалось изменить пароль';
                this.showToast('Не удалось изменить пароль', 'error');
                return false;
            } finally {
                this.passwordChange.loading = false;
            }
        },

        clearPasswordForm() {
            this.passwordChange.currentPassword = '';
            this.passwordChange.newPassword = '';
            this.passwordChange.confirmPassword = '';
            this.passwordChange.error = '';
            this.passwordChange.success = false;
        },

        // Init
        async init() {
            await this.checkModeAvailability();  // Wait for mode to load from backend
            this.loadFiles();                     // Then load files with correct mode
            this.loadXraySettings();
            this.checkUpdate();
            // Connect to SSE status stream
            statusService.connectStatusStream((status) => {
                this.serviceStatus = status;
            });
        }
    });
});
