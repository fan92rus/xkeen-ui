<script setup>
import { defineAsyncComponent } from 'vue';
import { onMounted, onUnmounted, ref, computed, provide, watch } from 'vue';
import { useAppStore } from './stores/app.js';
import { useI18nStore } from './stores/i18n.js';
import { renderAnsi } from './utils/ansi-format.js';
import { getAWGStatus } from './services/install.js';
import { getMetricsPort } from './services/metrics.js';
import { useFocusTrap } from './composables/useFocusTrap.js';
const EditorTab = defineAsyncComponent(() => import('./components/EditorTab.vue'));
import SubscriptionsTab from './components/SubscriptionsTab.vue';
import LogsTab from './components/LogsTab.vue';
import SettingsTab from './components/SettingsTab.vue';
const CommandsTab = defineAsyncComponent(() => import('./components/CommandsTab.vue'));
const MetricsTab = defineAsyncComponent(() => import('./components/MetricsTab.vue'));
const AwgTab = defineAsyncComponent(() => import('./components/AwgTab.vue'));
const RoutingTab = defineAsyncComponent(() => import('./components/RoutingTab.vue'));

const app = useAppStore();
const i18n = useI18nStore();

// Persist active tab across page reloads
watch(() => app.activeTab, (tab) => {
    localStorage.setItem('xkeen_active_tab', tab);
    location.hash = tab;
});

const awgInstalled = ref(false);
const metricsEnabled = ref(true);

onMounted(async () => {
    try {
        const status = await getAWGStatus();
        awgInstalled.value = status.installed;
    } catch {
        awgInstalled.value = false;
    }
    try {
        const m = await getMetricsPort();
        metricsEnabled.value = m.enabled;
    } catch {
        metricsEnabled.value = true; // assume enabled on error to avoid hiding
    }
    redirectInvalidTab();
});

function redirectInvalidTab() {
    // If current tab is no longer available, redirect to first valid tab
    const validIds = tabs.value.map(t => t.id);
    if (!validIds.includes(app.activeTab)) {
        app.activeTab = tabs.value[0]?.id || 'editor';
    }
}

// Reload the metrics-enabled flag. Called by SettingsTab after the user
// toggles metrics on/off so the Metrics tab appears/disappears immediately
// without requiring a page reload (injected as 'reloadMetricsState').
async function reloadMetricsState() {
    try {
        const m = await getMetricsPort();
        metricsEnabled.value = m.enabled;
    } catch {
        // keep current state on error
    }
    // If the active tab is no longer valid (e.g. metrics disabled while
    // viewing it), bounce to the first tab.
    redirectInvalidTab();
}

const tabs = computed(() => {
		const list = [
			{ id: 'editor', label: i18n.t('nav.editor') },
			{ id: 'routing', label: i18n.t('nav.routing') },
			{ id: 'subscriptions', label: i18n.t('nav.subscriptions') },
			{ id: 'logs', label: i18n.t('nav.logs') },
			{ id: 'settings', label: i18n.t('nav.settings') },
			{ id: 'commands', label: i18n.t('nav.commands') },
		];
		if (metricsEnabled.value) {
			list.push({ id: 'metrics', label: i18n.t('nav.metrics') });
		}
		if (awgInstalled.value) {
			list.splice(3, 0, { id: 'awg', label: 'AWG' });
		}
		return list;
	});

/* SVG icon paths (24x24 viewBox, stroke-based, Lucide-style) */
const theme = ref(localStorage.getItem('theme') || 'dark');
const isDark = computed(() => theme.value === 'dark');
function toggleTheme() {
    theme.value = isDark.value ? 'light' : 'dark';
    localStorage.setItem('theme', theme.value);
    document.documentElement.classList.toggle('light', !isDark.value);
}

const icons = {
    editor: 'M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z',
    subscriptions: 'M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z',
    logs: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8zM14 2v6h6M16 13H8M16 17H8M10 9H8',
    settings: 'M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8z',
    commands: 'M4 17l6-6-6-6M12 19h8',
    metrics: 'M18 20V10M12 20V4M6 20v-6',
    awg: 'M13 2L3 14h9l-1 8 10-12h-9l1-8z',
    routing: 'M3 6h18M3 12h18M3 18h12',
    play: 'M6 3l14 9-14 9V3z',
    stop: 'M3.6 3.6h16.8v16.8H3.6z',
    restart: 'M21 12a9 9 0 1 1-6.219-8.56M21 3v6h-6',
    logout: 'M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9',
    sun: 'M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0z',
    moon: 'M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z',
    logo: 'M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5',
};

const editorRef = ref(null);
const outputModalRef = ref(null);
provide('isDark', isDark);
provide('reloadMetricsState', reloadMetricsState);

useFocusTrap(outputModalRef, computed(() => app.modal.show));

function onKeydown(e) {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault();
        editorRef.value?.save();
    }
}

function doSave() { editorRef.value?.save(); }
function doDiff() { editorRef.value?.diff(); }

/* -- safe ANSI rendering for modal output -- */
const safeModalOutput = computed(() => app.modal.output ? renderAnsi(app.modal.output) : '');
const safeModalError = computed(() => app.modal.error ? renderAnsi(app.modal.error) : '');

onMounted(() => {
    document.documentElement.classList.toggle('light', theme.value === 'light');
    window.addEventListener('keydown', onKeydown);
    app.init();
});
onUnmounted(() => {
    window.removeEventListener('keydown', onKeydown);
});
</script>

<template>
  <div class="app">
    <!-- Sidebar -->
    <nav class="sidebar-nav" role="navigation" :aria-label="i18n.t('nav.main_nav')">
      <div class="sidebar-logo">
        <svg viewBox="0 0 24 24" fill="none" stroke="var(--blue)" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path v-for="(d, i) in icons.logo.split('M').filter(Boolean)" :key="i" :d="'M' + d" />
        </svg>
      </div>
      <div class="sidebar-nav-items">
        <button
          v-for="t in tabs" :key="t.id"
          class="nav-btn" :class="{ active: app.activeTab === t.id }"
          :title="t.label" :aria-label="t.label" :aria-current="app.activeTab === t.id ? 'page' : undefined"
          @click="app.activeTab = t.id"
        >
          <svg viewBox="0 0 24 24"><path :d="icons[t.id]" /></svg>
        </button>
      </div>
      <div class="sidebar-bottom">
        <button class="sidebar-btn theme-toggle" :title="isDark ? i18n.t('nav.light_theme') : i18n.t('nav.dark_theme')" :aria-label="isDark ? i18n.t('nav.switch_light') : i18n.t('nav.switch_dark')" @click="toggleTheme">
          <svg v-if="isDark" viewBox="0 0 24 24"><path :d="icons.sun" /></svg>
          <svg v-else viewBox="0 0 24 24"><path :d="icons.moon" /></svg>
        </button>
        <button class="sidebar-btn" :title="i18n.t('nav.logout')" :aria-label="i18n.t('nav.logout')" @click="app.logout()">
          <svg viewBox="0 0 24 24"><path :d="icons.logout" /></svg>
        </button>
      </div>
    </nav>

    <!-- Main Area -->
    <div class="main-area" role="main">
      <!-- Toolbar -->
      <div class="toolbar">
        <div class="toolbar-left">
          <template v-if="app.activeTab === 'editor'">
            <select
              class="file-select" :value="app.currentFile?.path || ''"
              @change="app.loadFile($event.target.value)"
            >
              <option value="" disabled>{{ i18n.t('app.choose_file') }}</option>
              <template v-for="g in app.fileGroups" :key="g.section">
                <optgroup v-if="g.files.length" :label="g.label">
                  <option v-for="f in g.files" :key="f.path" :value="f.path">{{ f.name }}</option>
                </optgroup>
              </template>
            </select>
          </template>
          <template v-else>
            <span class="toolbar-title">{{ tabs.find(t => t.id === app.activeTab)?.label || '' }}</span>
          </template>
        </div>
        <div class="toolbar-right">
          <!-- Service controls (always visible) -->
          <div class="service-bar">
            <span class="status-dot" :class="app.serviceStatus" />
            <span class="service-label">{{ i18n.t(app.serviceStatus === 'running' ? 'app.running' : app.serviceStatus === 'stopped' ? 'app.stopped' : 'app.unknown') }}</span>
            <button class="btn btn-sm" :disabled="app.serviceStatus === 'running'" :title="i18n.t('app.start')" :aria-label="i18n.t('app.start_title')" @click="app.startService()">
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path :d="icons.play" /></svg>
            </button>
            <button class="btn btn-sm" :disabled="app.serviceStatus === 'stopped'" :title="i18n.t('app.stop')" :aria-label="i18n.t('app.stop_title')" @click="app.stopService()">
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path :d="icons.stop" /></svg>
            </button>
            <button class="btn btn-sm" :title="i18n.t('app.restart')" :aria-label="i18n.t('app.restart_title')" @click="app.restartService()">
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path :d="icons.restart" /></svg>
            </button>
          </div>
          <!-- Editor actions -->
          <template v-if="app.activeTab === 'editor' && app.currentFile">
            <span v-if="app.currentFile" class="status-badge" :class="app.isValidJson === false ? 'invalid' : 'valid'">
              {{ app.isValidJson === false ? '✗ JSON' : '✓ JSON' }}
            </span>
            <button class="btn btn-sm" @click="doDiff()">{{ i18n.t('app.diff') }}</button>
            <button class="btn btn-sm" @click="app.showBackups()">{{ i18n.t('app.backups') }}</button>
            <button class="btn btn-sm btn-primary" @click="doSave()">{{ i18n.t('app.save') }}</button>
          </template>
        </div>
      </div>

      <!-- Tabs -->
      <EditorTab v-if="app.activeTab === 'editor'" ref="editorRef" class="tab-content" />
      <SubscriptionsTab v-if="app.activeTab === 'subscriptions'" class="tab-content" />
      <RoutingTab v-if="app.activeTab === 'routing'" class="tab-content" />
      <LogsTab v-if="app.activeTab === 'logs'" class="tab-content" />
      <AwgTab v-if="app.activeTab === 'awg'" class="tab-content" />
      <SettingsTab v-if="app.activeTab === 'settings'" class="tab-content" />
      <CommandsTab v-if="app.activeTab === 'commands'" class="tab-content" />
      <MetricsTab v-if="app.activeTab === 'metrics'" :active="app.activeTab === 'metrics'" class="tab-content" />
    </div>

    <!-- Output Modal -->
    <div v-show="app.modal.show" ref="outputModalRef" class="modal-overlay" @click.self="app.closeModal()">
      <div class="modal">
        <div class="modal-header">
          <h3>{{ i18n.t('app.output') }} <span>{{ app.modal.command }}</span></h3>
          <button class="modal-close" @click="app.closeModal()">&times;</button>
        </div>
        <div class="modal-body">
          <!-- eslint-disable vue/no-v-html -- content is escaped via renderAnsi() + escapeHtml() -->
          <pre v-show="app.modal.error" class="modal-error" v-html="safeModalError" />
          <pre id="modal-output" class="modal-output" v-html="safeModalOutput" />
          <!-- eslint-enable vue/no-v-html -->
        </div>
        <div v-show="app.canSendInput()" class="modal-input">
          <input
            v-model="app.inputValue" type="text" :placeholder="i18n.t('app.input_placeholder')"
            class="modal-input-field" @keydown.enter="app.sendInput()"
          >
          <button class="btn btn-primary" @click="app.sendInput()">{{ i18n.t('app.send') }}</button>
        </div>
        <div class="modal-footer">
          <button class="btn" @click="app.copyModalOutput()">{{ i18n.t('app.copy') }}</button>
          <button class="btn btn-primary" @click="app.closeModal()">{{ i18n.t('app.close') }}</button>
        </div>
      </div>
    </div>

    <!-- Confirm Dialog -->
    <div v-show="app.confirm.show" class="modal-overlay" @click.self="app.cancelConfirm()">
      <div class="modal">
        <div class="modal-header"><h3>{{ i18n.t('app.confirm_title') }}</h3></div>
        <div class="modal-body">
          <p>{{ i18n.t('app.confirm_text') }}</p>
          <p class="confirm-description">{{ app.confirm.description }}</p>
        </div>
        <div class="modal-footer">
          <button class="btn" @click="app.cancelConfirm()">{{ i18n.t('app.cancel') }}</button>
          <button class="btn btn-danger" @click="app.executeConfirm()">{{ i18n.t('app.execute') }}</button>
        </div>
      </div>
    </div>

    <!-- Backups Modal -->
    <div v-show="app.backupsModal.show" class="modal-overlay" @click.self="app.closeBackupsModal()">
      <div class="modal modal-large">
        <div class="modal-header">
          <h3>{{ i18n.t('app.backup_title') }} <span>{{ app.backupsModal.fileName }}</span></h3>
          <button class="modal-close" @click="app.closeBackupsModal()">&times;</button>
        </div>
        <div class="modal-body">
          <div v-show="app.backupsModal.backups.length > 0" class="backups-list">
            <div
              v-for="backup in app.backupsModal.backups" :key="backup.path" class="backup-item"
              :class="{ selected: app.backupsModal.selectedBackup?.path === backup.path }"
              @click="app.selectBackup(backup)"
            >
              <span class="backup-time">{{ app.formatBackupTime(backup.modified) }}</span>
              <div class="backup-actions">
                <button class="btn btn-sm" @click.stop="app.copyBackupContent(backup)">{{ i18n.t('app.backup_copy') }}</button>
                <button class="btn btn-sm btn-primary" @click.stop="app.loadBackupToEditor(backup)">{{ i18n.t('app.backup_download') }}</button>
              </div>
            </div>
          </div>
          <div v-show="app.backupsModal.backups.length === 0" class="backups-empty">
            <p>{{ i18n.t('app.backup_none') }}</p>
          </div>
          <div v-show="app.backupsModal.selectedBackup && app.backupsModal.diffContent" class="backup-diff">
            <h4>{{ i18n.t('app.diff_title') }}</h4>
            <pre class="diff-content" v-html="app.backupsModal.diffContent" /> <!-- eslint-disable-line vue/no-v-html -- content escaped via diff.js escapeHtml() -->
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn" @click="app.closeBackupsModal()">{{ i18n.t('app.diff_close') }}</button>
        </div>
      </div>
    </div>

    <!-- Diff Modal -->
    <div v-show="app.diffModal.show" class="modal-overlay" @click.self="app.closeDiffModal()">
      <div class="modal modal-large">
        <div class="modal-header">
          <h3>{{ i18n.t('app.unsaved_changes') }}</h3>
          <button class="modal-close" @click="app.closeDiffModal()">&times;</button>
        </div>
        <div class="modal-body">
          <pre class="diff-content" v-html="app.diffModal.diffContent" /> <!-- eslint-disable-line vue/no-v-html -- content escaped via diff.js escapeHtml() -->
        </div>
        <div class="modal-footer">
          <button class="btn btn-primary" @click="app.closeDiffModal()">{{ i18n.t('app.unsaved_close') }}</button>
        </div>
      </div>
    </div>

    <!-- Toast -->
    <div v-show="app.toast.show" :class="'toast ' + (app.toast.type || '')">
      {{ app.toast.message }}
    </div>
  </div>
</template>
