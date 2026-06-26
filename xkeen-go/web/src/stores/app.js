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

function safeLS(key, fallback) {
	try { return localStorage.getItem(key) ?? fallback; } catch { return fallback; }
}

export const useAppStore = defineStore('app', () => {
	// ── Sub-stores (delegated domain logic) ──
	const configStore = useConfigStore();
	const serviceStore = useServiceStore();
	const updateStore = useUpdateStore();

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
		if (mode === 'mihomo' && !mihomoAvailable.value) { showToast('Mihomo не установлен', 'error'); return; }
		if (mode === 'xray' && !xrayAvailable.value) { showToast('Xray не установлен', 'error'); return; }
		try {
			await modeService.setMode(mode);
			configStore.currentFile = null;
			currentMode.value = mode;
			serviceStore.logFile = mode === 'mihomo' ? '/opt/var/log/mihomo/access.log' : '/opt/var/log/xray/access.log';
			await configStore.loadFiles(mode);
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
				body: JSON.stringify({ current_password: p.currentPassword, new_password: p.newPassword }),
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
	const backupsModal = reactive({ show: false, fileName: '', backups: [], selectedBackup: null, diffContent: '' });
	const diffModal = reactive({ show: false, diffContent: '' });

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
