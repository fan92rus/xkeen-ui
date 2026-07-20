<script setup>
import { ref, onMounted, onUnmounted, watch, nextTick } from 'vue';
import { useAppStore } from '../stores/app.js';
import { useI18nStore } from '../stores/i18n.js';
import { createLogStream } from '../services/logs.js';

const app = useAppStore();
const i18n = useI18nStore();
const logsEl = ref(null);
const autoScroll = ref(true);
let stream = null;

function scrollToBottom() {
    if (autoScroll.value && logsEl.value) {
        nextTick(() => { logsEl.value.scrollTop = logsEl.value.scrollHeight; });
    }
}

function onScroll() {
    if (!logsEl.value) return;
    const { scrollTop, scrollHeight, clientHeight } = logsEl.value;
    autoScroll.value = scrollHeight - scrollTop - clientHeight < 40;
}

function connect() {
    app.loadLogs();
    if (stream && stream.isOpen()) return;
    stream = createLogStream(
        (msg) => {
            app.logs.push(msg);
            if (app.logs.length > 500) app.logs = app.logs.slice(-500);
            scrollToBottom();
        },
        () => { app.showToast('Log stream error', 'error'); }
    );
}

function disconnect() {
    if (stream) { stream.close(); stream = null; }
}

watch(() => app.activeTab, (tab) => {
    if (tab === 'logs') connect();
    else disconnect();
});

onMounted(() => { if (app.activeTab === 'logs') connect(); });
onUnmounted(disconnect);
</script>

<template>
  <div class="logs-wrapper">
    <div class="logs-toolbar">
      <select v-model="app.logFile" @change="app.loadLogs()">
        <option v-show="app.currentMode === 'xray'" value="/opt/var/log/xray/access.log">Access</option>
        <option v-show="app.currentMode === 'xray'" value="/opt/var/log/xray/error.log">Error</option>
        <option v-show="app.currentMode === 'mihomo'" value="/opt/var/log/mihomo/access.log">Access</option>
        <option v-show="app.currentMode === 'mihomo'" value="/opt/var/log/mihomo/error.log">Error</option>
      </select>
      <input v-model="app.logSearch" type="text" :placeholder="i18n.t('logs.search')">
      <select v-model="app.logFilter">
        <option value="all">{{ i18n.t('logs.all') }}</option>
        <option value="error">{{ i18n.t('logs.errors') }}</option>
        <option value="warn">{{ i18n.t('logs.warning') }}</option>
        <option value="info">{{ i18n.t('logs.info') }}</option>
      </select>
      <button class="btn btn-sm" @click="app.clearLogs()">{{ i18n.t('logs.clear') }}</button>
    </div>
    <div ref="logsEl" class="logs-container" @scroll="onScroll">
      <div v-for="(log, index) in app.filteredLogs" :key="log.timestamp + '-' + index" :class="'log-entry log-' + log.level">
        <span class="log-time">{{ log.timestamp }}</span>
        <span class="log-level">{{ log.level.toUpperCase() }}</span>
        <span class="log-message">{{ log.message }}</span>
      </div>
    </div>
  </div>
</template>
