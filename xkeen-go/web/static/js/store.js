// store.js - Global Alpine store for application state

import * as configService from './services/config.js';
import * as xkeenService from './services/xkeen.js';
import * as logsService from './services/logs.js';

document.addEventListener('alpine:init', () => {
    Alpine.store('app', {
        // UI state
        activeTab: 'editor',
        toast: { message: '', type: '', show: false },
        loading: false,

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

        // Modal state
        modal: {
            show: false,
            command: '',
            output: '',
            error: ''
        },

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
                this.files = await configService.listFiles();
            } catch (err) {
                this.showToast('Failed to load files', 'error');
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
                this.showToast('Service started', 'success');
                this.fetchServiceStatus();
            } catch (err) {
                this.showToast('Failed to start service', 'error');
            }
        },

        async stopService() {
            try {
                await xkeenService.stop();
                this.showToast('Service stopped', 'success');
                this.fetchServiceStatus();
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

        // Init
        init() {
            this.loadFiles();
            this.loadXraySettings();
        }
    });
});
