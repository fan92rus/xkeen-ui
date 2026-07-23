// stores/update.js - Update check and install state.

import { defineStore } from 'pinia';
import { ref, reactive } from 'vue';
import * as updateService from '../services/update.js';
import { useAppStore } from './app.js';
import { useI18nStore } from './i18n.js';

export const useUpdateStore = defineStore('update', () => {
	const i18n = useI18nStore();
	const currentVersion = ref('unknown');
	const currentBranch = ref('');
	const availableBranches = ref([]);
	const selectedBranch = ref('');
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
			// 1. Fetch branches
			const branchesData = await updateService.getBranches();
			currentBranch.value = branchesData.current_branch || '';
			availableBranches.value = branchesData.branches || [];

			// Init selectedBranch on first call
			if (!selectedBranch.value) {
				selectedBranch.value = currentBranch.value;
			}

			// 2. Check update for selected branch
			const data = await updateService.checkUpdate(
				checkDevUpdates.value,
				selectedBranch.value,
			);
			currentVersion.value = data.current_version;
			Object.assign(updateInfo, {
				update_available: data.update_available,
				is_prerelease: data.is_prerelease || false,
				current_version: data.current_version,
				latest_version: data.latest_version,
				release_url: data.release_url || '',
				release_notes: data.release_notes || '',
			});
			if (data.error) app.showToast(i18n.t('toast.update_checking') + data.error, 'error');
		} catch { app.showToast(i18n.t('toast.update_check_error'), 'error'); }
		finally { updateChecking.value = false; }
	}

	async function startUpdate() {
		const app = useAppStore();
		updating.value = true; updateProgress.value = 0; updateStatus.value = i18n.t('toast.update_starting');
		try {
			await updateService.startUpdate({
				prerelease: checkDevUpdates.value,
				branch: selectedBranch.value,
				onProgress: (data) => { updateProgress.value = data.percent; updateStatus.value = data.status; },
				onComplete: (data) => {
					app.showToast(data.message || i18n.t('toast.update_done'), 'success');
					updating.value = false;
				},
				onError: (data) => {
					app.showToast(i18n.t('toast.update_error') + data.error, 'error');
					updating.value = false;
				},
			});
		} catch (err) {
			app.showToast(i18n.t('toast.update_error_short') + err.message, 'error');
			updating.value = false;
		}
	}

	return {
		currentVersion, currentBranch, availableBranches, selectedBranch, checkDevUpdates,
		updateInfo, updateChecking, updating, updateProgress, updateStatus,
		checkUpdate, startUpdate,
	};
});
