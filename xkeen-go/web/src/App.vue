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

// Keyboard shortcut
function onKeydown(e) {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') {
        e.preventDefault();
        window.dispatchEvent(new CustomEvent('editor:save'));
    }
}

onMounted(() => {
    window.addEventListener('keydown', onKeydown);
    app.init();
});
</script>

<template>
  <div class="app">
    <!-- Header -->
    <header class="header">
      <h1>XKEEN Редактор конфигураций</h1>
      <div class="actions">
        <ServiceCtrl />
        <button @click="window.dispatchEvent(new CustomEvent('editor:save'))" class="btn btn-primary">Сохранить</button>
        <button @click="app.restartService()" class="btn btn-danger">Перезапуск Xkeen</button>
        <button @click="app.logout()" class="btn">Выход</button>
      </div>
    </header>

    <!-- Tabs -->
    <nav class="tabs">
      <button class="tab" :class="{ active: app.activeTab === 'editor' }" @click="app.activeTab = 'editor'">Редактор</button>
      <button class="tab" :class="{ active: app.activeTab === 'subscriptions' }" @click="app.activeTab = 'subscriptions'">⭐ Подписки</button>
      <button class="tab" :class="{ active: app.activeTab === 'logs' }" @click="app.activeTab = 'logs'">Логи</button>
      <button class="tab" :class="{ active: app.activeTab === 'settings' }" @click="app.activeTab = 'settings'">Настройки</button>
      <button class="tab" :class="{ active: app.activeTab === 'commands' }" @click="app.activeTab = 'commands'">Команды</button>
    </nav>

    <!-- Main Content -->
    <main class="main">
      <EditorTab v-if="app.activeTab === 'editor'" class="tab-content active" />
      <SubscriptionsTab v-if="app.activeTab === 'subscriptions'" class="tab-content tab-subscriptions" />
      <LogsTab v-if="app.activeTab === 'logs'" class="tab-content tab-logs" />
      <SettingsTab v-if="app.activeTab === 'settings'" class="tab-content tab-settings" />
      <CommandsTab v-if="app.activeTab === 'commands'" class="tab-content tab-commands" />
    </main>

    <!-- Output Modal -->
    <div class="modal-overlay" v-show="app.modal.show" @click.self="app.closeModal()">
      <div class="modal">
        <div class="modal-header">
          <h3>Вывод команды: <span>{{ app.modal.command }}</span></h3>
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
      <div class="modal confirm-dialog">
        <div class="modal-header"><h3>Подтверждение команды</h3></div>
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
