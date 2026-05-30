<script setup>
import { onMounted } from 'vue';
import { useAppStore } from './stores/app.js';
import ServiceCtrl from './components/ServiceCtrl.vue';
import EditorTab from './components/EditorTab.vue';
import SubscriptionsTab from './components/SubscriptionsTab.vue';
import LogsTab from './components/LogsTab.vue';
import SettingsTab from './components/SettingsTab.vue';
import CommandsTab from './components/CommandsTab.vue';

const app = useAppStore();

const tabs = [
    { id: 'editor', icon: '📝', label: 'Редактор' },
    { id: 'subscriptions', icon: '⭐', label: 'Подписки' },
    { id: 'logs', icon: '📋', label: 'Логи' },
    { id: 'settings', icon: '⚙', label: 'Настройки' },
    { id: 'commands', icon: '💻', label: 'Команды' },
];

function onKeydown(e) {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault();
        window.dispatchEvent(new CustomEvent('editor:save'));
    }
}

function doSave() {
    window.dispatchEvent(new CustomEvent('editor:save'));
}

onMounted(() => {
    window.addEventListener('keydown', onKeydown);
    app.init();
});
</script>

<template>
  <div class="app">
    <!-- Sidebar Nav -->
    <nav class="sidebar-nav">
      <div class="sidebar-logo">⚙</div>
      <div class="sidebar-nav-items">
        <button v-for="t in tabs" :key="t.id"
                class="nav-btn" :class="{ active: app.activeTab === t.id }"
                :title="t.label"
                @click="app.activeTab = t.id">
          {{ t.icon }}
        </button>
      </div>
      <div class="sidebar-bottom">
        <div class="status-dot" :class="app.serviceStatus"
             :title="'XKeen: ' + app.serviceStatus"
             @click="app.serviceStatus === 'running' ? app.stopService() : app.startService()"></div>
        <button class="sidebar-btn" title="Выйти" @click="app.logout()">⏻</button>
      </div>
    </nav>

    <!-- Main Area -->
    <div class="main-area">
      <!-- Toolbar -->
      <div class="toolbar">
        <div class="toolbar-left">
          <!-- Editor: file selector -->
          <template v-if="app.activeTab === 'editor'">
            <select class="file-select" :value="app.currentFile?.path || ''"
                    @change="app.loadFile($event.target.value)">
              <option value="" disabled>Выберите файл…</option>
              <option v-for="f in app.files" :key="f.path" :value="f.path">{{ f.name }}</option>
            </select>
            <span class="toolbar-title">{{ app.currentFile?.path || '' }}</span>
          </template>
          <!-- Other tabs: label -->
          <template v-else>
            <span class="toolbar-title">{{ tabs.find(t => t.id === app.activeTab)?.label || '' }}</span>
          </template>
        </div>
        <div class="toolbar-right">
          <!-- Editor actions -->
          <template v-if="app.activeTab === 'editor' && app.currentFile">
            <span class="status-badge" :class="app.isValidJson === false ? 'invalid' : 'valid'">
              {{ app.isValidJson === false ? '✗ JSON' : '✓ JSON' }}
            </span>
            <button class="btn btn-sm" @click="window.dispatchEvent(new CustomEvent('editor:diff'))">Diff</button>
            <button class="btn btn-sm" @click="app.showBackups()">Бэкапы</button>
            <button class="btn btn-sm btn-primary" @click="doSave()">Сохранить</button>
          </template>
          <!-- Service actions (non-editor) -->
          <template v-if="app.activeTab !== 'editor'">
            <button @click="app.restartService()" class="btn btn-sm btn-danger" title="Перезапуск Xkeen">↻ Xkeen</button>
          </template>
        </div>
      </div>

      <!-- Tab Content -->
      <EditorTab v-if="app.activeTab === 'editor'" class="tab-content" />
      <SubscriptionsTab v-if="app.activeTab === 'subscriptions'" class="tab-content" />
      <LogsTab v-if="app.activeTab === 'logs'" class="tab-content" />
      <SettingsTab v-if="app.activeTab === 'settings'" class="tab-content" />
      <CommandsTab v-if="app.activeTab === 'commands'" class="tab-content" />

    </div>

    <!-- Output Modal -->
    <div class="modal-overlay" v-show="app.modal.show" @click.self="app.closeModal()">
      <div class="modal">
        <div class="modal-header">
          <h3>Вывод: <span>{{ app.modal.command }}</span></h3>
          <button class="modal-close" @click="app.closeModal()">&times;</button>
        </div>
        <div class="modal-body">
          <pre v-show="app.modal.error" class="modal-error" v-html="app.modal.error"></pre>
          <pre id="modal-output" class="modal-output" v-html="app.modal.output"></pre>
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

    <!-- Confirmation Dialog -->
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
