// stores/config.js - File operations, editor state, and backup management.

import { defineStore } from 'pinia';
import { ref, reactive } from 'vue';
import * as configService from '../services/config.js';
import { computeDiff as computeDiffHtml } from '../utils/diff.js';
import { useAppStore } from './app.js';
import { useI18nStore } from './i18n.js';

export const useConfigStore = defineStore('config', () => {
	const i18n = useI18nStore();

	// ── State ──
	const files = ref([]);
	const fileGroups = ref([]);
	const currentFile = ref(null);
	const isValidJson = ref(true);
	const lastSavedContent = ref('');
	const editorLoadContent = ref(null);

	const backupsModal = reactive({ show: false, fileName: '', backups: [], selectedBackup: null, diffContent: '' });
	const diffModal = reactive({ show: false, diffContent: '' });

	// ── Actions: Config ──
	async function loadFiles(mode) {
		const app = useAppStore();
		try {
			const data = await configService.listFiles(mode);
			files.value = data;
		} catch { app.showToast(i18n.t('toast.file_load_error'), 'error'); }
	}

	async function loadGroupedFiles() {
		const app = useAppStore();
		try {
			const groups = await configService.listFilesGrouped();
			fileGroups.value = groups;
			// Build flat files list for backward compat
			const all = [];
			for (const g of groups) {
				for (const f of g.files) {
					f._section = g.section;
					f._label = g.label;
					all.push(f);
				}
			}
			files.value = all;
		} catch { app.showToast(i18n.t('toast.file_load_error'), 'error'); }
	}

	async function loadFile(path) {
		const app = useAppStore();
		try {
			const data = await configService.getFile(path);
			if (data.path) {
				currentFile.value = {
					path: data.path,
					content: data.content,
					valid: data.valid,
					modified: data.modified,
				};
				isValidJson.value = data.valid;
				lastSavedContent.value = data.content;
			}
		} catch { app.showToast(i18n.t('toast.file_load_error_single'), 'error'); }
	}

	async function saveFile(content) {
		const app = useAppStore();
		if (!currentFile.value) { app.showToast(i18n.t('toast.file_not_selected'), 'error'); return false; }
		try {
			const data = await configService.saveFile(
				currentFile.value.path,
				content,
				currentFile.value.modified,
			);
			currentFile.value.content = content;
			if (data && data.modified !== undefined) {
				currentFile.value.modified = data.modified;
			}
			lastSavedContent.value = content;
			app.showToast(i18n.t('toast.saved_ok'), 'success');
			return true;
		} catch (err) {
			if (err.status === 409) {
				app.showToast(i18n.t('toast.file_changed_disk'), 'error');
				await loadFile(currentFile.value.path);
				return false;
			}
			app.showToast(err.message || i18n.t('toast.save_error'), 'error');
			return false;
		}
	}

	// ── Actions: Backups ──
	async function showBackups() {
		const app = useAppStore();
		if (!currentFile.value) return;
		try {
			backupsModal.fileName = currentFile.value.path.split('/').pop();
			backupsModal.backups = await configService.getBackups(currentFile.value.path);
			backupsModal.selectedBackup = null;
			backupsModal.diffContent = '';
			backupsModal.show = true;
			if (backupsModal.backups.length > 0) await selectBackup(backupsModal.backups[0]);
		} catch { app.showToast(i18n.t('toast.backup_load_error'), 'error'); }
	}

	function closeBackupsModal() {
		backupsModal.show = false; backupsModal.selectedBackup = null; backupsModal.diffContent = '';
	}

	async function selectBackup(backup) {
		const app = useAppStore();
		backupsModal.selectedBackup = backup;
		try {
			const backupContent = await configService.getBackupContent(backup.path);
			backupsModal.diffContent = computeDiffHtml(lastSavedContent.value || '', backupContent);
		} catch { app.showToast(i18n.t('toast.backup_content_error'), 'error'); }
	}

	async function copyBackupContent(backup) {
		const app = useAppStore();
		try {
			const content = await configService.getBackupContent(backup.path);
			await navigator.clipboard.writeText(content);
			app.showToast(i18n.t('toast.backup_copied'), 'success');
		} catch { app.showToast(i18n.t('toast.backup_copy_failed'), 'error'); }
	}

	async function loadBackupToEditor(backup) {
		const app = useAppStore();
		try {
			const content = await configService.getBackupContent(backup.path);
			editorLoadContent.value = content;
			closeBackupsModal();
			app.showToast(i18n.t('toast.backup_loaded'), 'success');
		} catch { app.showToast(i18n.t('toast.backup_load_failed'), 'error'); }
	}

	// ── Actions: Diff ──
	function openDiffModal(currentContent, savedContent) {
		const app = useAppStore();
		if (currentContent === savedContent) { app.showToast(i18n.t('toast.no_changes')); return; }
		diffModal.diffContent = computeDiffHtml(currentContent, savedContent);
		diffModal.show = true;
	}

	function closeDiffModal() { diffModal.show = false; diffModal.diffContent = ''; }

	return {
		files, fileGroups, currentFile, isValidJson, lastSavedContent, editorLoadContent,
		backupsModal, diffModal,
		loadFiles, loadGroupedFiles, loadFile, saveFile,
		showBackups, closeBackupsModal, selectBackup, copyBackupContent, loadBackupToEditor,
		openDiffModal, closeDiffModal,
	};
});
