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
                this.showToast('Failed to load files', 'error');
            }
        },

        async switchMode(mode) {
            if (mode === this.currentMode) {
                return;
            }

            if (mode === 'mihomo' && !this.mihomoAvailable) {
                this.showToast('Mihomo is not installed', 'error');
                return;
            }
            if (mode === 'xray' && !this.xrayAvailable) {
                this.showToast('Xray is not installed', 'error');
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

                this.showToast(`Switched to ${mode}`, 'success');
            } catch (err) {
                this.showToast(err.message || 'Failed to switch mode', 'error');
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
                this.showToast('Failed to load file', 'error');
            }
        },

        async saveFile(content) {
            if (!this.currentFile) {
                this.showToast('No file selected', 'error');
                return false;
            }

            try {
                await configService.saveFile(this.currentFile.path, content);
                this.lastSavedContent = content;  // Update after successful save
                this.showToast('Saved successfully', 'success');
                return true;
            } catch (err) {
                this.showToast(err.message || 'Save failed', 'error');
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
                this.showToast('Service starting...', 'success');
                // Status will be updated via SSE
            } catch (err) {
                this.showToast('Failed to start service', 'error');
            }
        },

        async stopService() {
            try {
                await xkeenService.stop();
                this.showToast('Service stopping...', 'success');
                // Status will be updated via SSE
            } catch (err) {
                this.showToast('Failed to stop service', 'error');
            }
        },

        async restartService() {
            try {
                await xkeenService.restart();
                this.showToast('Xkeen restarting...', 'success');
            } catch (err) {
                this.showToast('Restart failed', 'error');
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
                this.showToast('Failed to load Xray settings', 'error');
            }
        },

        async updateLogLevel() {
            try {
                const result = await xkeenService.setLogLevel(this.xraySettings.logLevel);
                this.showToast(result.message || 'Log level updated', 'success');
            } catch (err) {
                this.showToast(err.message || 'Failed to update log level', 'error');
                this.loadXraySettings();
            }
        },

        // Logs actions
        async loadLogs() {
            try {
                this.logs = await logsService.fetchLogs(this.logFile, 100);
            } catch (err) {
                this.showToast('Failed to load logs', 'error');
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
                this.showToast('Output copied to clipboard', 'success');
            } catch (err) {
                this.showToast('Failed to copy to clipboard', 'error');
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
                this.showToast('Failed to load backups', 'error');
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
                this.showToast('Failed to load backup content', 'error');
            }
        },

        async copyBackupContent(backup) {
            try {
                const content = await configService.getBackupContent(backup.path);
                await navigator.clipboard.writeText(content);
                this.showToast('Backup copied to clipboard', 'success');
            } catch (err) {
                this.showToast('Failed to copy backup', 'error');
            }
        },

        async loadBackupToEditor(backup) {
            try {
                const content = await configService.getBackupContent(backup.path);
                // Dispatch event for editor to handle
                window.dispatchEvent(new CustomEvent('editor:loadContent', { detail: content }));
                this.closeBackupsModal();
                this.showToast('Backup loaded into editor', 'success');
            } catch (err) {
                this.showToast('Failed to load backup', 'error');
            }
        },

        formatBackupTime(timestamp) {
            const date = new Date(timestamp * 1000);
            return date.toLocaleString();
        },

        // Diff modal actions
        openDiffModal(currentContent, savedContent) {
            if (currentContent === savedContent) {
                this.showToast('No changes since last save', '');
                return;
            }
            this.diffModal.diffContent = this.computeDiff(currentContent, savedContent);
            this.diffModal.show = true;
        },

        closeDiffModal() {
            this.diffModal.show = false;
            this.diffModal.diffContent = '';
        },

        // Diff computation - returns HTML with colored +/- markers
        computeDiff(a, b) {
            const linesA = a.split('\n');
            const linesB = b.split('\n');
            let result = [];

            const escapeHtml = (str) => {
                return str.replace(/&/g, '&amp;')
                          .replace(/</g, '&lt;')
                          .replace(/>/g, '&gt;');
            };

            const maxLen = Math.max(linesA.length, linesB.length);
            for (let i = 0; i < maxLen; i++) {
                const lineA = linesA[i];
                const lineB = linesB[i];

                if (lineA === lineB) {
                    result.push('  ' + escapeHtml(lineA || ''));
                } else {
                    if (lineB !== undefined) {
                        result.push('<span class="diff-removed">- ' + escapeHtml(lineB) + '</span>');
                    }
                    if (lineA !== undefined) {
                        result.push('<span class="diff-added">+ ' + escapeHtml(lineA) + '</span>');
                    }
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
                    this.showToast('Update check: ' + data.error, 'error');
                }
            } catch (err) {
                this.showToast('Failed to check for updates', 'error');
            } finally {
                this.updateChecking = false;
            }
        },

        async startUpdate() {
            this.updating = true;
            this.updateProgress = 0;
            this.updateStatus = 'Starting update...';

            try {
                const prerelease = this.checkDevUpdates;
                await updateService.startUpdate({
                    prerelease: prerelease,
                    onProgress: (data) => {
                        this.updateProgress = data.percent;
                        this.updateStatus = data.status;
                    },
                    onComplete: (data) => {
                        this.showToast(data.message || 'Update complete!', 'success');
                        // Page will reload when service restarts
                    },
                    onError: (data) => {
                        this.showToast('Update failed: ' + data.error, 'error');
                        this.updating = false;
                    }
                });
            } catch (err) {
                this.showToast('Update failed: ' + err.message, 'error');
                this.updating = false;
            }
        },

        // Password change actions
        async changePassword() {
            // Client-side validation
            if (!this.passwordChange.currentPassword || !this.passwordChange.newPassword || !this.passwordChange.confirmPassword) {
                this.showToast('All password fields are required', 'error');
                return false;
            }

            if (this.passwordChange.newPassword.length < 8) {
                this.showToast('New password must be at least 8 characters', 'error');
                return false;
            }

            if (this.passwordChange.newPassword !== this.passwordChange.confirmPassword) {
                this.showToast('New passwords do not match', 'error');
                return false;
            }

            if (this.passwordChange.currentPassword === this.passwordChange.newPassword) {
                this.showToast('New password must be different from current password', 'error');
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
                    this.passwordChange.error = data.error || 'Failed to change password';
                    this.showToast(data.error || 'Failed to change password', 'error');
                    return false;
                }

                this.passwordChange.success = true;
                this.showToast('Password changed successfully', 'success');
                this.clearPasswordForm();
                return true;
            } catch (err) {
                this.passwordChange.error = err.message || 'Failed to change password';
                this.showToast('Failed to change password', 'error');
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
