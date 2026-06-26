<script setup>
import { defineAsyncComponent } from 'vue';
import { onMounted, onUnmounted, ref, computed, provide, watch } from 'vue';
import { useAppStore } from './stores/app.js';
import { renderAnsi } from './utils/ansi-format.js';
const EditorTab = defineAsyncComponent(() => import('./components/EditorTab.vue'));
import SubscriptionsTab from './components/SubscriptionsTab.vue';
import LogsTab from './components/LogsTab.vue';
import SettingsTab from './components/SettingsTab.vue';
const CommandsTab = defineAsyncComponent(() => import('./components/CommandsTab.vue'));
const MetricsTab = defineAsyncComponent(() => import('./components/MetricsTab.vue'));
const AwgTab = defineAsyncComponent(() => import('./components/AwgTab.vue'));

const app = useAppStore();

// Persist active tab across page reloads
watch(() => app.activeTab, (tab) => {
    localStorage.setItem('xkeen_active_tab', tab);
    location.hash = tab;
});

const tabs = computed(() => {
    const list = [
        { id: 'editor', label: 'Редактор' },
        { id: 'subscriptions', label: 'Подписки' },
        { id: 'awg', label: 'AWG' },
        { id: 'logs', label: 'Логи' },
        { id: 'settings', label: 'Настройки' },
        { id: 'commands', label: 'Команды' },
        { id: 'metrics', label: 'Монитор' },
    ];
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
    play: 'M6 3l14 9-14 9V3z',
    stop: 'M3.6 3.6h16.8v16.8H3.6z',
    restart: 'M21 12a9 9 0 1 1-6.219-8.56M21 3v6h-6',
    logout: 'M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9',
    sun: 'M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0z',
    moon: 'M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z',
    logo: 'M12 2L2 7l10 5 10-5-10-5zM2 17l10 5 10-5M2 12l10 5 10-5',
};

const editorRef = ref(null);
provide('isDark', isDark);

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
    <nav class="sidebar-nav" role="navigation" aria-label="Основная навигация">
      <div class="sidebar-logo">
        <svg viewBox="0 0 24 24" fill="none" stroke="var(--blue)" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path v-for="(d, i) in icons.logo.split('M').filter(Boolean)" :key="i" :d="'M' + d" />
        </svg>
      </div>
      <div class="sidebar-nav-items">
        <button v-for="t in tabs" :key="t.id"
                class="nav-btn" :class="{ active: app.activeTab === t.id }"
                :title="t.label" :aria-label="t.label" :aria-current="app.activeTab === t.id ? 'page' : undefined"
                @click="app.activeTab = t.id">
          <svg viewBox="0 0 24 24"><path :d="icons[t.id]" /></svg>
        </button>
      </div>
      <div class="sidebar-bottom">
        <button class="sidebar-btn theme-toggle" :title="isDark ? 'Светлая тема' : 'Тёмная тема'" :aria-label="isDark ? 'Переключить на светлую тему' : 'Переключить на тёмную тему'" @click="toggleTheme">
          <svg v-if="isDark" viewBox="0 0 24 24"><path :d="icons.sun" /></svg>
          <svg v-else viewBox="0 0 24 24"><path :d="icons.moon" /></svg>
        </button>
        <button class="sidebar-btn" title="Выйти" aria-label="Выйти" @click="app.logout()">
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
            <select class="file-select" :value="app.currentFile?.path || ''"
                    @change="app.loadFile($event.target.value)">
              <option value="" disabled>Выберите файл…</option>
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
            <span class="status-dot" :class="app.serviceStatus"></span>
            <span class="service-label">{{ app.serviceStatus === 'running' ? 'Запущен' : app.serviceStatus === 'stopped' ? 'Остановлен' : '…' }}</span>
            <button class="btn btn-sm" @click="app.startService()" :disabled="app.serviceStatus === 'running'" title="Запустить" aria-label="Запустить XKeen">
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path :d="icons.play" /></svg>
            </button>
            <button class="btn btn-sm" @click="app.stopService()" :disabled="app.serviceStatus === 'stopped'" title="Остановить" aria-label="Остановить XKeen">
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path :d="icons.stop" /></svg>
            </button>
            <button class="btn btn-sm" @click="app.restartService()" title="Перезапустить" aria-label="Перезапустить XKeen">
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path :d="icons.restart" /></svg>
            </button>
          </div>
          <!-- Editor actions -->
          <template v-if="app.activeTab === 'editor' && app.currentFile">
            <span v-if="app.currentFile" class="status-badge" :class="app.isValidJson === false ? 'invalid' : 'valid'">
              {{ app.isValidJson === false ? '✗ JSON' : '✓ JSON' }}
            </span>
            <button class="btn btn-sm" @click="doDiff()">Diff</button>
            <button class="btn btn-sm" @click="app.showBackups()">Бэкапы</button>
            <button class="btn btn-sm btn-primary" @click="doSave()">Сохранить</button>
          </template>
        </div>
      </div>

      <!-- Tabs -->
      <EditorTab v-if="app.activeTab === 'editor'" ref="editorRef" class="tab-content" />
      <SubscriptionsTab v-if="app.activeTab === 'subscriptions'" class="tab-content" />
      <LogsTab v-if="app.activeTab === 'logs'" class="tab-content" />
      <AwgTab v-if="app.activeTab === 'awg'" class="tab-content" />
      <SettingsTab v-if="app.activeTab === 'settings'" class="tab-content" />
      <CommandsTab v-if="app.activeTab === 'commands'" class="tab-content" />
      <MetricsTab v-if="app.activeTab === 'metrics'" :active="app.activeTab === 'metrics'" class="tab-content" />
    </div>

    <!-- Output Modal -->
    <div class="modal-overlay" v-show="app.modal.show" @click.self="app.closeModal()">
      <div class="modal">
        <div class="modal-header">
          <h3>Вывод: <span>{{ app.modal.command }}</span></h3>
          <button class="modal-close" @click="app.closeModal()">&times;</button>
        </div>
        <div class="modal-body">
          <pre v-show="app.modal.error" class="modal-error" v-html="safeModalError"></pre>
          <pre id="modal-output" class="modal-output" v-html="safeModalOutput"></pre>
        </div>
        <div class="modal-input" v-show="app.canSendInput()">
          <input type="text" v-model="app.inputValue" @keydown.enter="app.sendInput()"
                 placeholder="Введите данные и нажмите Enter..." class="modal-input-field">
          <button class="btn btn-primary" @click="app.sendInput()">Отправить</button>
        </div>
        <div class="modal-footer">
          <button class="btn" @click="app.copyModalOutput()">Скопировать</button>
          <button class="btn btn-primary" @click="app.closeModal()">Закрыть</button>
        </div>
      </div>
    </div>

    <!-- Confirm Dialog -->
    <div class="modal-overlay" v-show="app.confirm.show" @click.self="app.cancelConfirm()">
      <div class="modal">
        <div class="modal-header"><h3>Подтверждение</h3></div>
        <div class="modal-body">
          <p>Вы уверены, что хотите выполнить эту команду?</p>
          <p class="confirm-description">{{ app.confirm.description }}</p>
        </div>
        <div class="modal-footer">
          <button class="btn" @click="app.cancelConfirm()">Отмена</button>
          <button class="btn btn-danger" @click="app.executeConfirm()">Выполнить</button>
        </div>
      </div>
    </div>

    <!-- Backups Modal -->
    <div class="modal-overlay" v-show="app.backupsModal.show" @click.self="app.closeBackupsModal()">
      <div class="modal modal-large">
        <div class="modal-header">
          <h3>Резервные копии: <span>{{ app.backupsModal.fileName }}</span></h3>
          <button class="modal-close" @click="app.closeBackupsModal()">&times;</button>
        </div>
        <div class="modal-body">
          <div class="backups-list" v-show="app.backupsModal.backups.length > 0">
            <div v-for="(backup, index) in app.backupsModal.backups" :key="backup.path" class="backup-item"
                 :class="{ selected: app.backupsModal.selectedBackup?.path === backup.path }"
                 @click="app.selectBackup(backup)">
              <span class="backup-time">{{ app.formatBackupTime(backup.modified) }}</span>
              <div class="backup-actions">
                <button class="btn btn-sm" @click.stop="app.copyBackupContent(backup)">Копировать</button>
                <button class="btn btn-sm btn-primary" @click.stop="app.loadBackupToEditor(backup)">Загрузить</button>
              </div>
            </div>
          </div>
          <div class="backups-empty" v-show="app.backupsModal.backups.length === 0">
            <p>Нет доступных резервных копий</p>
          </div>
          <div class="backup-diff" v-show="app.backupsModal.selectedBackup && app.backupsModal.diffContent">
            <h4>Сравнение с текущим файлом</h4>
            <pre class="diff-content" v-html="app.backupsModal.diffContent"></pre>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn" @click="app.closeBackupsModal()">Закрыть</button>
        </div>
      </div>
    </div>

    <!-- Diff Modal -->
    <div class="modal-overlay" v-show="app.diffModal.show" @click.self="app.closeDiffModal()">
      <div class="modal modal-large">
        <div class="modal-header">
          <h3>Изменения с последнего сохранения</h3>
          <button class="modal-close" @click="app.closeDiffModal()">&times;</button>
        </div>
        <div class="modal-body">
          <pre class="diff-content" v-html="app.diffModal.diffContent"></pre>
        </div>
        <div class="modal-footer">
          <button class="btn btn-primary" @click="app.closeDiffModal()">Закрыть</button>
        </div>
      </div>
    </div>

    <!-- Toast -->
    <div v-show="app.toast.show" :class="'toast ' + (app.toast.type || '')">
      {{ app.toast.message }}
    </div>
  </div>
</template>
