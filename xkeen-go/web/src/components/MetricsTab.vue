<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { getMetricsStats, getMetricsObservatory } from '../services/metrics.js';

/* ---- state ---- */
const stats = ref(null);
const observatory = ref(null);
const available = ref(false);
const loading = ref(true);
const lastUpdate = ref(null);
let pollTimer = null;

/* ---- helpers ---- */
function formatBytes(bytes) {
    if (bytes == null || bytes === 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(1024));
    const val = (Math.abs(bytes) / Math.pow(1024, i)).toFixed(1);
    return val + ' ' + units[i];
}

function delayColor(ms) {
    if (ms < 200) return 'green';
    if (ms < 500) return 'yellow';
    return 'red';
}

function timeAgo(unix) {
    if (!unix) return '—';
    const now = Date.now() / 1000;
    const diff = Math.floor(now - unix);
    if (diff < 60) return diff + ' сек назад';
    if (diff < 3600) return Math.floor(diff / 60) + ' мин назад';
    return Math.floor(diff / 3600) + ' ч назад';
}

/* ---- computed ---- */
const totalDown = computed(() => {
    if (!stats.value?.outbound) return 0;
    let sum = 0;
    for (const v of Object.values(stats.value.outbound)) {
        sum += (v?.downlink || 0);
    }
    return sum;
});

const totalUp = computed(() => {
    if (!stats.value?.outbound) return 0;
    let sum = 0;
    for (const v of Object.values(stats.value.outbound)) {
        sum += (v?.uplink || 0);
    }
    return sum;
});

const outboundList = computed(() => {
    if (!stats.value?.outbound) return [];
    const entries = Object.entries(stats.value.outbound);
    entries.sort((a, b) => (b[1]?.downlink || 0) - (a[1]?.downlink || 0));
    return entries;
});

const observatoryList = computed(() => {
    if (!observatory.value) return [];
    return Object.entries(observatory.value);
});

/* ---- polling ---- */
async function poll() {
    try {
        const [s, o] = await Promise.all([getMetricsStats(), getMetricsObservatory()]);
        if (s?.available) {
            stats.value = s;
            observatory.value = o?.available ? o.results : null;
            available.value = true;
            lastUpdate.value = new Date();
        } else {
            available.value = false;
        }
    } catch (e) {
        available.value = false;
    } finally {
        loading.value = false;
    }
}

onMounted(() => {
    poll();
    pollTimer = setInterval(poll, 3000);
});

onUnmounted(() => {
    if (pollTimer) clearInterval(pollTimer);
});
</script>

<template>
  <div class="metrics-wrapper">
    <!-- Unavailable -->
    <div v-if="!loading && !available" class="metrics-unavailable">
      <p>⚠️ Метрики недоступны.</p>
      <p class="metrics-hint">Проверьте настройку <code>metrics_port</code> в конфигурации.</p>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="metrics-loading">
      <p>Загрузка метрик…</p>
    </div>

    <!-- Dashboard -->
    <div v-if="available" class="metrics-content">
      <div class="metrics-header">
        <h2>Метрики Xray</h2>
        <span class="metrics-updated" v-if="lastUpdate">Обновлено: {{ lastUpdate.toLocaleTimeString() }}</span>
      </div>

      <!-- Summary cards -->
      <div class="metrics-cards">
        <div class="metric-card">
          <div class="metric-icon">⬇️</div>
          <div class="metric-label">Общий downlink</div>
          <div class="metric-value">{{ formatBytes(totalDown) }}</div>
        </div>
        <div class="metric-card">
          <div class="metric-icon">⬆️</div>
          <div class="metric-label">Общий uplink</div>
          <div class="metric-value">{{ formatBytes(totalUp) }}</div>
        </div>
        <div class="metric-card">
          <div class="metric-icon">🔗</div>
          <div class="metric-label">Outbound тегов</div>
          <div class="metric-value">{{ outboundList.length }}</div>
        </div>
        <div class="metric-card">
          <div class="metric-icon">🔬</div>
          <div class="metric-label">Observatory прокси</div>
          <div class="metric-value">{{ observatoryList.length }}</div>
        </div>
      </div>

      <!-- Outbound table -->
      <div class="metrics-section">
        <h3>Трафик по outbound</h3>
        <table class="metrics-table">
          <thead>
            <tr>
              <th>Тег</th>
              <th class="num">Downlink</th>
              <th class="num">Uplink</th>
              <th class="num">Всего</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="[tag, data] in outboundList" :key="tag">
              <td class="tag-cell">{{ tag }}</td>
              <td class="num">{{ formatBytes(data?.downlink || 0) }}</td>
              <td class="num">{{ formatBytes(data?.uplink || 0) }}</td>
              <td class="num">{{ formatBytes((data?.downlink || 0) + (data?.uplink || 0)) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- Observatory table -->
      <div v-if="observatoryList.length" class="metrics-section">
        <h3>Observatory — результаты</h3>
        <table class="metrics-table">
          <thead>
            <tr>
              <th>Тег</th>
              <th>Статус</th>
              <th class="num">Задержка</th>
              <th>Последняя проверка</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="[tag, data] in observatoryList" :key="tag">
              <td class="tag-cell">{{ tag }}</td>
              <td>
                <span v-if="data?.alive" class="status-badge alive">✅ Alive</span>
                <span v-else class="status-badge dead">❌ Dead</span>
              </td>
              <td class="num">
                <span v-if="data?.delay != null" :class="'delay-indicator ' + delayColor(data.delay)">
                  {{ data.delay }} ms
                </span>
                <span v-else>—</span>
              </td>
              <td>{{ timeAgo(data?.last_try_time) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<style scoped>
.metrics-wrapper {
    padding: 16px;
    min-height: 100%;
}

.metrics-unavailable {
    text-align: center;
    padding: 60px 20px;
    color: var(--color-danger, #e74c3c);
}

.metrics-unavailable p {
    margin: 8px 0;
    font-size: 1.1em;
}

.metrics-hint {
    font-size: 0.9em;
    opacity: 0.8;
}

.metrics-hint code {
    background: var(--bg-secondary, #2a2a2a);
    padding: 2px 6px;
    border-radius: 4px;
    font-family: monospace;
}

.metrics-loading {
    text-align: center;
    padding: 60px 20px;
    opacity: 0.7;
}

.metrics-content {
    max-width: 1200px;
}

.metrics-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;
}

.metrics-header h2 {
    margin: 0;
    font-size: 1.4em;
}

.metrics-updated {
    font-size: 0.85em;
    opacity: 0.6;
}

/* Cards */
.metrics-cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 16px;
    margin-bottom: 28px;
}

.metric-card {
    background: var(--bg-card, #2a2a2a);
    border-radius: 10px;
    padding: 20px;
    text-align: center;
    border: 1px solid var(--border-color, #444);
}

.metric-icon {
    font-size: 1.8em;
    margin-bottom: 6px;
}

.metric-label {
    font-size: 0.85em;
    opacity: 0.7;
    margin-bottom: 8px;
}

.metric-value {
    font-size: 1.5em;
    font-weight: bold;
    color: var(--color-accent, #6cb4ee);
}

/* Sections */
.metrics-section {
    margin-bottom: 28px;
}

.metrics-section h3 {
    margin: 0 0 12px;
    font-size: 1.1em;
    opacity: 0.9;
}

/* Tables */
.metrics-table {
    width: 100%;
    border-collapse: collapse;
    background: var(--bg-card, #2a2a2a);
    border-radius: 8px;
    overflow: hidden;
}

.metrics-table thead {
    background: var(--bg-header, #333);
}

.metrics-table th,
.metrics-table td {
    padding: 10px 14px;
    text-align: left;
    border-bottom: 1px solid var(--border-color, #444);
}

.metrics-table th {
    font-weight: 600;
    font-size: 0.85em;
    text-transform: uppercase;
    opacity: 0.7;
}

.metrics-table .num {
    text-align: right;
    font-variant-numeric: tabular-nums;
}

.metrics-table tbody tr:hover {
    background: var(--bg-hover, #3a3a3a);
}

.metrics-table .tag-cell {
    font-family: monospace;
    font-weight: 600;
}

/* Status badges */
.status-badge {
    padding: 3px 10px;
    border-radius: 12px;
    font-size: 0.85em;
    font-weight: 600;
}

.status-badge.alive {
    background: rgba(39, 174, 96, 0.2);
    color: #27ae60;
}

.status-badge.dead {
    background: rgba(231, 76, 60, 0.2);
    color: #e74c3c;
}

/* Delay indicator */
.delay-indicator.green {
    color: #27ae60;
}

.delay-indicator.yellow {
    color: #f39c12;
}

.delay-indicator.red {
    color: #e74c3c;
}
</style>
