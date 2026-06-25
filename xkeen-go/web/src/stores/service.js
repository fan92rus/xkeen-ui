// stores/service.js - Service status, Xray settings, and logs.

import { defineStore } from 'pinia';
import { ref, reactive, computed } from 'vue';
import * as xkeenService from '../services/xkeen.js';
import * as logsService from '../services/logs.js';
import { filterLogs } from '../utils/log-filter.js';
import { useAppStore } from './app.js';

export const useServiceStore = defineStore('service', () => {
	// ── Service ──
	const serviceStatus = ref('unknown');

	async function fetchServiceStatus() {
		try { serviceStatus.value = await xkeenService.getStatus(); }
		catch { serviceStatus.value = 'unknown'; }
	}

	async function startService() {
		const app = useAppStore();
		try { await xkeenService.start(); app.showToast('Запуск сервиса...', 'success'); }
		catch { app.showToast('Не удалось запустить сервис', 'error'); }
	}

	async function stopService() {
		const app = useAppStore();
		try { await xkeenService.stop(); app.showToast('Остановка сервиса...', 'success'); }
		catch { app.showToast('Не удалось остановить сервис', 'error'); }
	}

	async function restartService() {
		const app = useAppStore();
		try { await xkeenService.restart(); app.showToast('Перезапуск Xkeen...', 'success'); }
		catch { app.showToast('Ошибка перезапуска', 'error'); }
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
		} catch { app.showToast('Не удалось загрузить настройки Xray', 'error'); }
	}

	async function updateLogLevel() {
		const app = useAppStore();
		try {
			const result = await xkeenService.setLogLevel(xraySettings.logLevel);
			app.showToast(result.message || 'Уровень логирования обновлён', 'success');
		} catch (err) {
			app.showToast(err.message || 'Не удалось обновить уровень логирования', 'error');
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
		catch { app.showToast('Не удалось загрузить логи', 'error'); }
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
