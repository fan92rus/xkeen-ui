<script setup>
import { ref, onMounted, onUnmounted, watch, nextTick } from 'vue';
import { useAppStore } from '../stores/app.js';
import { createLogStream } from '../services/logs.js';

const app = useAppStore();
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
        <option value="/opt/var/log/xray/access.log" v-show="app.currentMode === 'xray'">Access</option>
        <option value="/opt/var/log/xray/error.log" v-show="app.currentMode === 'xray'">Error</option>
        <option value="/opt/var/log/mihomo/access.log" v-show="app.currentMode === 'mihomo'">Access</option>
        <option value="/opt/var/log/mihomo/error.log" v-show="app.currentMode === 'mihomo'">Error</option>
      </select>
      <input type="text" v-model="app.logSearch" placeholder="Поиск…">
      <select v-model="app.logFilter">
        <option value="all">Все</option>
        <option value="error">Ошибки</option>
        <option value="warn">Warning</option>
        <option value="info">Info</option>
      </select>
      <button @click="app.clearLogs()" class="btn btn-sm">Очистить</button>
    </div>
    <div class="logs-container" ref="logsEl" @scroll="onScroll">
      <div v-for="(log, index) in app.filteredLogs" :key="log.timestamp + '-' + index" :class="'log-entry log-' + log.level">
        <span class="log-time">{{ log.timestamp }}</span>
        <span class="log-level">{{ log.level.toUpperCase() }}</span>
        <span class="log-message">{{ log.message }}</span>
      </div>
    </div>
  </div>
</template>
