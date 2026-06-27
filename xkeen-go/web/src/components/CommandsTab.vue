<script setup>
import { ref, nextTick, onMounted } from 'vue';
import { useAppStore } from '../stores/app.js';
import { InteractiveSession } from '../services/interactive.js';
import { getCommands, refreshCommands } from '../services/xkeen.js';
import { groupCommandsByCategory } from '../utils/commands-grouping.js';
import { useI18nStore } from '../stores/i18n.js';

const app = useAppStore();
const i18n = useI18nStore();
const executingCommand = ref('');

// Command palette comes from the backend registry (GET /api/xkeen/commands),
// NOT a hardcoded list. The backend parses `xkeen -help`, so the UI always
// reflects the actually-available, actually-allowed commands.
const categories = ref([]);
const loading = ref(true);
const refreshing = ref(false);
const loadError = ref('');

// Flat lookup of { name → { name, description, dangerous } } for the confirm
// dialog text, rebuilt whenever the palette loads.
const commandIndex = ref({});

function isDangerous(cmd) {
    return !!commandIndex.value[cmd]?.dangerous;
}

async function loadCommandPalette() {
    loading.value = true;
    loadError.value = '';
    try {
        const { commands, error } = await getCommands();
        categories.value = groupCommandsByCategory(commands);
        const idx = {};
        for (const cat of categories.value) {
            for (const c of cat.commands) idx[c.name] = c;
        }
        commandIndex.value = idx;
        if (!commands.length && error) {
            loadError.value = error;
        }
    } catch (err) {
        categories.value = [];
        commandIndex.value = {};
        loadError.value = err?.message || i18n.t('commands.unknown_reason');
    } finally {
        loading.value = false;
    }
}

async function refreshCommandPalette() {
    refreshing.value = true;
    loadError.value = '';
    try {
        const { commands, error } = await refreshCommands();
        categories.value = groupCommandsByCategory(commands);
        const idx = {};
        for (const cat of categories.value) {
            for (const c of cat.commands) idx[c.name] = c;
        }
        commandIndex.value = idx;
        if (!commands.length && error) {
            loadError.value = error;
        }
    } catch (err) {
        loadError.value = err?.message || i18n.t('commands.unknown_reason');
    } finally {
        refreshing.value = false;
    }
}

onMounted(loadCommandPalette);

function executeCommand(command) {
    if (isDangerous(command)) {
        const info = commandIndex.value[command];
        app.confirm.description = info?.description || `Выполнить команду ${command}`;
        app.confirm.onConfirm = () => doExecute(command);
        app.confirm.show = true;
    } else {
        doExecute(command);
    }
}

async function doExecute(command) {
    executingCommand.value = command;
    app.modal.error = ''; app.modal.output = ''; app.modal.command = command;
    app.modal.show = true; app.commandComplete = false; app.inputValue = '';

    try {
        await new Promise((resolve, reject) => {
            app.interactiveSession = new InteractiveSession(
                command,
                (msg) => handleStreamMessage(msg),
                (msg) => {
                    app.interactiveSession = null; app.commandComplete = true;
                    if (!msg.success && !app.modal.error) app.modal.error = i18n.t('commands.completed', { code: msg.exitCode });
                    resolve();
                },
                () => {
                    app.interactiveSession = null; app.commandComplete = true;
                    reject(new Error(i18n.t('commands.ws_error')));
                }
            );
            app.interactiveSession.connect();
        });
    } catch (err) {
        app.modal.error = i18n.t('commands.exec_error') + err.message;
    } finally {
        executingCommand.value = '';
        app.commandComplete = true;
    }
}

function handleStreamMessage(msg) {
    if (msg.type === 'output') {
        app.modal.output += msg.text;
        scrollToBottom();
    } else if (msg.type === 'error') {
        app.modal.error += (app.modal.error ? '\n' : '') + msg.text;
        scrollToBottom();
    } else if (msg.type === 'complete') {
        app.commandComplete = true;
        if (!msg.success && !app.modal.error) app.modal.error = i18n.t('commands.completed', { code: msg.exitCode });
    }
}

function scrollToBottom() {
    nextTick(() => {
        const el = document.getElementById('modal-output');
        if (el) el.scrollTop = el.scrollHeight;
    });
}

// Set native title tooltip ONLY when the description text is truncated (ellipsis).
// Avoids showing tooltips for short descriptions that fit.
function onDescHover(e) {
  const el = e.target;
  if (el.scrollWidth > el.clientWidth) {
    el.title = el.textContent;
  } else {
    el.removeAttribute('title');
  }
}

function isLoading(command) { return executingCommand.value === command; }
</script>

<template>
  <div class="commands-container">
    <div v-if="loading" class="commands-loading">{{ i18n.t('commands.loading') }}</div>
    <div v-else-if="loadError" class="commands-error">
      {{ i18n.t('commands.exec_error') }}{{ loadError }}
    </div>
    <div v-else-if="categories.length === 0" class="commands-empty">
      {{ i18n.t('commands.no_commands') }}
    </div>
    <template v-else>
      <!-- Toolbar with refresh -->
      <div class="commands-toolbar">
        <span class="commands-count">{{ i18n.t('commands.count', { count: categories.flatMap(c => c.commands).length }) }}</span>
        <button class="btn btn-sm" @click="refreshCommandPalette" :disabled="refreshing" style="margin-left:auto">
          {{ refreshing ? i18n.t('commands.refreshing') : i18n.t('commands.refresh') }}
        </button>
      </div>
      <div class="commands-grid">
      <div v-for="category in categories" :key="category.name" class="command-category-block">
        <h3 class="category-title">{{ i18n.t('cat.' + category.localeKey) }}</h3>
        <div class="category-commands-list">
          <div v-for="cmd in category.commands" :key="cmd.name" class="command-item">
            <div class="command-info">
              <span class="command-name">{{ cmd.name }}</span>
              <span class="command-desc" @mouseenter="onDescHover">{{ cmd.description }}</span>
            </div>
            <button class="btn"
                    :class="isDangerous(cmd.name) ? 'btn-danger' : 'btn-primary'"
                    @click="executeCommand(cmd.name)"
                    :disabled="isLoading(cmd.name)">
              {{ isLoading(cmd.name) ? i18n.t('commands.executing') : (isDangerous(cmd.name) ? i18n.t('commands.execute_btn') : i18n.t('commands.run_btn')) }}
            </button>
          </div>
        </div>
      </div>
    </div>
    </template>
  </div>
</template>

<style scoped>
.commands-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}
</style>
