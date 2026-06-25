// stores/update.js - Update check and install state.

import { defineStore } from 'pinia';
import { ref, reactive } from 'vue';
import * as updateService from '../services/update.js';
import { useAppStore } from './app.js';

export const useUpdateStore = defineStore('update', () => {
	const currentVersion = ref('unknown');
	const checkDevUpdates = ref(false);

	const updateInfo = reactive({
		update_available: false,
		is_prerelease: false,
		current_version: '',
		latest_version: '',
		release_url: '',
		release_notes: '',
	});

	const updateChecking = ref(false);
	const updating = ref(false);
	const updateProgress = ref(0);
	const updateStatus = ref('');

	async function checkUpdate() {
		const app = useAppStore();
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
				release_notes: data.release_notes || '',
			});
			if (data.error) app.showToast('Проверка обновлений: ' + data.error, 'error');
		} catch { app.showToast('Не удалось проверить обновления', 'error'); }
		finally { updateChecking.value = false; }
	}

	async function startUpdate() {
		const app = useAppStore();
		updating.value = true; updateProgress.value = 0; updateStatus.value = 'Запуск обновления...';
		try {
			await updateService.startUpdate({
				prerelease: checkDevUpdates.value,
				onProgress: (data) => { updateProgress.value = data.percent; updateStatus.value = data.status; },
				onComplete: (data) => {
					app.showToast(data.message || 'Обновление завершено!', 'success');
					updating.value = false;
				},
				onError: (data) => {
					app.showToast('Ошибка обновления: ' + data.error, 'error');
					updating.value = false;
				},
			});
		} catch (err) {
			app.showToast('Ошибка обновления: ' + err.message, 'error');
			updating.value = false;
		}
	}

	return {
		currentVersion, checkDevUpdates,
		updateInfo, updateChecking, updating, updateProgress, updateStatus,
		checkUpdate, startUpdate,
	};
});
