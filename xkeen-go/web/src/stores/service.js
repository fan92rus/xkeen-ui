// stores/service.js - Service status, Xray settings, and logs.

import { defineStore } from 'pinia';
import { ref, reactive, computed } from 'vue';
import * as xkeenService from '../services/xkeen.js';
import * as logsService from '../services/logs.js';
import { filterLogs } from '../utils/log-filter.js';
import { useAppStore } from './app.js';
import { useI18nStore } from './i18n.js';

export const useServiceStore = defineStore('service', () => {
	// ── Service ──
	const i18n = useI18nStore();
	const serviceStatus = ref('unknown');

	async function fetchServiceStatus() {
		try { serviceStatus.value = await xkeenService.getStatus(); }
		catch { serviceStatus.value = 'unknown'; }
	}

	async function startService() {
		const app = useAppStore();
		try { await xkeenService.start(); app.showToast(i18n.t('toast.service_starting'), 'success'); }
		catch { app.showToast(i18n.t('toast.service_start_failed'), 'error'); }
	}

	async function stopService() {
		const app = useAppStore();
		try { await xkeenService.stop(); app.showToast(i18n.t('toast.service_stopping'), 'success'); }
		catch { app.showToast(i18n.t('toast.service_stop_failed'), 'error'); }
	}

	async function restartService() {
		const app = useAppStore();
		try { await xkeenService.restart(); app.showToast(i18n.t('toast.service_restarting'), 'success'); }
		catch { app.showToast(i18n.t('toast.service_restart_failed'), 'error'); }
	}

	// ── Xray settings ──
	const xraySettings = reactive({
		logLevel: 'none',
		logLevels: ['debug', 'info', 'warning', 'error', 'none'],
		accessLog: '',
		errorLog: '',
	});

	async function loadXraySettings() {
		const app = useAppStore();
		try {
			const data = await xkeenService.getSettings();
			if (data.log_level !== undefined) {
				xraySettings.logLevel = data.log_level;
				xraySettings.logLevels = data.log_levels || xraySettings.logLevels;
				xraySettings.accessLog = data.access_log || '';
				xraySettings.errorLog = data.error_log || '';
			}
		} catch { app.showToast(i18n.t('toast.xray_settings_error'), 'error'); }
	}

	async function updateLogLevel() {
		const app = useAppStore();
		try {
			const result = await xkeenService.setLogLevel(xraySettings.logLevel);
			app.showToast(result.message || i18n.t('toast.log_level_updated'), 'success');
		} catch (err) {
			app.showToast(err.message || i18n.t('toast.log_level_error'), 'error');
			loadXraySettings();
		}
	}

	// ── Logs ──
	const logs = ref([]);
	const logFilter = ref('all');
	const logSearch = ref('');
	const logFile = ref('/opt/var/log/xray/access.log');
	const filteredLogs = computed(() => filterLogs(logs.value, logFilter.value, logSearch.value));

	async function loadLogs() {
		const app = useAppStore();
		try { logs.value = await logsService.fetchLogs(logFile.value, 100); }
		catch { app.showToast(i18n.t('toast.log_load_error'), 'error'); }
	}

	function clearLogs() { logs.value = []; }

	return {
		serviceStatus,
		xraySettings,
		logs, logFilter, logSearch, logFile, filteredLogs,
		fetchServiceStatus, startService, stopService, restartService,
		loadXraySettings, updateLogLevel,
		loadLogs, clearLogs,
	};
});
