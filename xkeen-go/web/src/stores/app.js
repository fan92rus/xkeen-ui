// stores/app.js - Core application state (Pinia).
// Delegates domain-specific logic to sub-stores (config, service, update).
// Components can import useAppStore and get everything, or import
// sub-stores directly for focused access.

import { defineStore } from 'pinia';
import { ref, reactive, shallowRef } from 'vue';
import * as statusService from '../services/status.js';
import * as modeService from '../services/mode.js';
import { storeToRefs } from 'pinia';
import { formatBackupTime } from '../utils/format.js';
import { error as logError } from '../utils/logger.js';
import { useConfigStore } from './config.js';
import { useServiceStore } from './service.js';
import { useUpdateStore } from './update.js';
import { useI18nStore } from './i18n.js';

function safeLS(key, fallback) {
	try { return localStorage.getItem(key) ?? fallback; } catch { return fallback; }
}

export const useAppStore = defineStore('app', () => {
	// ── Sub-stores (delegated domain logic) ──
	const configStore = useConfigStore();
	const serviceStore = useServiceStore();
	const updateStore = useUpdateStore();
	const t = (key, p) => useI18nStore().t(key, p);

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

	async function switchMode(mode) {
		if (mode === currentMode.value) return;
		if (mode === 'mihomo' && !mihomoAvailable.value) { showToast(t('app.mihomo_missing'), 'error'); return; }
		if (mode === 'xray' && !xrayAvailable.value) { showToast(t('app.xray_missing'), 'error'); return; }
		try {
			await modeService.setMode(mode);
			configStore.currentFile = null;
			currentMode.value = mode;
			serviceStore.logFile = mode === 'mihomo' ? '/opt/var/log/mihomo/access.log' : '/opt/var/log/xray/access.log';
			await configStore.loadFiles(mode);
			showToast(t('app.mode_switched', { mode }), 'success');
		} catch (err) {
			showToast(err.message || t('app.mode_switch_error'), 'error');
		}
	}

	async function checkModeAvailability() {
		try {
			const data = await modeService.getModeInfo();
			xrayAvailable.value = data.xray_available;
			mihomoAvailable.value = data.mihomo_available;
			if (data.mode) {
				currentMode.value = data.mode;
				serviceStore.logFile = data.mode === 'mihomo' ? '/opt/var/log/mihomo/access.log' : '/opt/var/log/xray/access.log';
			}
		} catch (err) { logError('Failed to check mode availability:', err); }
	}

	// ── Auth ──
	const passwordChange = reactive({
		currentPassword: '', newPassword: '', confirmPassword: '',
		loading: false, error: '', success: false,
	});

	async function changePassword() {
		const p = passwordChange;
		if (!p.currentPassword || !p.newPassword || !p.confirmPassword) { showToast(t('settings.password_required'), 'error'); return false; }
		if (p.newPassword.length < 8) { showToast(t('settings.password_minlength'), 'error'); return false; }
		if (p.newPassword !== p.confirmPassword) { showToast(t('settings.password_mismatch'), 'error'); return false; }
		if (p.currentPassword === p.newPassword) { showToast(t('settings.password_same'), 'error'); return false; }
		p.loading = true; p.error = ''; p.success = false;
		try {
			const csrfToken = document.cookie.match(/csrf_token=([^;]+)/)?.[1] || '';
			const response = await fetch('/api/auth/change-password', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken },
				body: JSON.stringify({ current_password: p.currentPassword, new_password: p.newPassword }),
			});
			const data = await response.json();
			if (!response.ok || !data.ok) {
				p.error = data.error || t('settings.password_change_error');
				showToast(p.error, 'error');
				return false;
			}
			p.success = true;
			showToast(t('settings.password_changed_ok'), 'success');
			clearPasswordForm();
			return true;
		} catch (err) {
			p.error = err.message || t('settings.password_change_fail');
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

	async function logout() {
		statusService.disconnectStatusStream();
		try { await fetch('/api/auth/logout', { method: 'POST' }); } catch {}
		window.location.href = '/login';
	}

	// ── Modals ──
	const modal = reactive({ show: false, command: '', output: '', error: '' });
	const interactiveSession = shallowRef(null);
	const inputValue = ref('');
	const commandComplete = ref(false);
	const confirm = reactive({ show: false, command: '', description: '', onConfirm: null });

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
			// Send '\r' (carriage return) for Enter, not '\n'. Real terminals send
		// 0x0D on Enter; the PTY line-discipline (ICRNL) converts it to '\n' in
		// canonical mode, and raw-mode TUI apps (like xkeen's full-screen menu)
		// expect '\r' directly. '\n' is silently ignored by raw-mode apps.
		interactiveSession.value.send(inputValue.value + '\r');
			inputValue.value = '';
		}
	}

	async function copyModalOutput() {
		try {
			await navigator.clipboard.writeText(modal.output);
			showToast(t('toast.copied'), 'success');
		} catch { showToast(t('toast.copy_failed'), 'error'); }
	}

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

	// ── Init ──
	async function init() {
		await checkModeAvailability();
		configStore.loadGroupedFiles();
		serviceStore.loadXraySettings();
		updateStore.checkUpdate();
		statusService.connectStatusStream((status) => { serviceStore.serviceStatus = status; });
	}

	return {
		// UI
		activeTab, loading, toast, showToast,
		// Mode
		currentMode, xrayAvailable, mihomoAvailable,
		// Auth
		passwordChange,
		// Modal
		modal, interactiveSession, inputValue, commandComplete,
		confirm,
		// Sub-store refs
		...storeToRefs(configStore),
		...storeToRefs(serviceStore),
		...storeToRefs(updateStore),
		// Actions
		switchMode, checkModeAvailability,
		changePassword, clearPasswordForm, logout,
		closeModal, canSendInput, sendInput, copyModalOutput,
		cancelConfirm, executeConfirm,
		// Delegated actions from sub-stores
		loadFiles: configStore.loadFiles,
		loadGroupedFiles: configStore.loadGroupedFiles,
		loadFile: configStore.loadFile,
		saveFile: configStore.saveFile,
		showBackups: configStore.showBackups,
		closeBackupsModal: configStore.closeBackupsModal,
		selectBackup: configStore.selectBackup,
		copyBackupContent: configStore.copyBackupContent,
		loadBackupToEditor: configStore.loadBackupToEditor,
		openDiffModal: configStore.openDiffModal,
		closeDiffModal: configStore.closeDiffModal,
		fetchServiceStatus: serviceStore.fetchServiceStatus,
		startService: serviceStore.startService,
		stopService: serviceStore.stopService,
		restartService: serviceStore.restartService,
		loadXraySettings: serviceStore.loadXraySettings,
		updateLogLevel: serviceStore.updateLogLevel,
		loadLogs: serviceStore.loadLogs,
		clearLogs: serviceStore.clearLogs,
		checkUpdate: updateStore.checkUpdate,
		startUpdate: updateStore.startUpdate,
		// Pure imports
		formatBackupTime,
		// Init
		init,
	};
});
