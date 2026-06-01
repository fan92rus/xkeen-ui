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

const maxDownlink = computed(() => {
    if (!stats.value?.outbound) return 1;
    let max = 0;
    for (const v of Object.values(stats.value.outbound)) {
        if (v?.downlink > max) max = v.downlink;
    }
    return max || 1;
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
      <svg viewBox="0 0 24 24" fill="none" stroke="var(--error)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="unavailable-icon">
        <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
        <line x1="12" y1="9" x2="12" y2="13"/>
        <line x1="12" y1="17" x2="12.01" y2="17"/>
      </svg>
      <p>Метрики недоступны</p>
      <p class="metrics-hint">Проверьте настройку <code>metrics_port</code> в конфигурации</p>
    </div>

    <!-- Loading dots -->
    <div v-if="loading" class="metrics-loading">
      <div class="loading-dots"><span></span><span></span><span></span></div>
    </div>

    <!-- Dashboard -->
    <div v-if="available" class="metrics-content">
      <div class="metrics-header">
        <h2>Метрики Xray</h2>
        <span class="metrics-updated" v-if="lastUpdate">{{ lastUpdate.toLocaleTimeString() }}</span>
      </div>

      <!-- Summary cards -->
      <div class="metrics-cards">
        <div class="metric-card">
          <svg viewBox="0 0 24 24" fill="none" stroke="var(--primary-color)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="metric-icon">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
            <polyline points="7 10 12 15 17 10"/>
            <line x1="12" y1="15" x2="12" y2="3"/>
          </svg>
          <div class="metric-value">{{ formatBytes(totalDown) }}</div>
          <div class="metric-label">Общий downlink</div>
        </div>
        <div class="metric-card">
          <svg viewBox="0 0 24 24" fill="none" stroke="var(--primary-color)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="metric-icon">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
            <polyline points="17 8 12 3 7 8"/>
            <line x1="12" y1="3" x2="12" y2="15"/>
          </svg>
          <div class="metric-value">{{ formatBytes(totalUp) }}</div>
          <div class="metric-label">Общий uplink</div>
        </div>
        <div class="metric-card">
          <svg viewBox="0 0 24 24" fill="none" stroke="var(--primary-color)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="metric-icon">
            <path d="M12 2L2 7l10 5 10-5-10-5z"/>
            <path d="M2 17l10 5 10-5"/>
            <path d="M2 12l10 5 10-5"/>
          </svg>
          <div class="metric-value">{{ outboundList.length }}</div>
          <div class="metric-label">Outbound тегов</div>
        </div>
        <div class="metric-card">
          <svg viewBox="0 0 24 24" fill="none" stroke="var(--primary-color)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" class="metric-icon">
            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
            <circle cx="12" cy="12" r="3"/>
          </svg>
          <div class="metric-value">{{ observatoryList.length }}</div>
          <div class="metric-label">Observatory прокси</div>
        </div>
      </div>

      <!-- Outbound table -->
      <div class="metrics-section">
        <h3>Трафик по outbound</h3>
        <div class="metrics-table-wrapper">
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
                <td class="num">
                  {{ formatBytes(data?.downlink || 0) }}
                  <div class="sparkline" :style="'width:' + ((data?.downlink || 0) / maxDownlink * 100) + '%'" title="{{ data?.downlink || 0 }}"/>
                </td>
                <td class="num">{{ formatBytes(data?.uplink || 0) }}</td>
                <td class="num">{{ formatBytes((data?.downlink || 0) + (data?.uplink || 0)) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Observatory table -->
      <div v-if="observatoryList.length" class="metrics-section">
        <h3>Observatory — результаты</h3>
        <div class="metrics-table-wrapper">
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
                  <span v-if="data?.alive" class="status-badge status-alive">Alive</span>
                  <span v-else class="status-badge status-dead">Dead</span>
                </td>
                <td class="num">
                  <span v-if="data?.delay != null" :class="['delay-cell', delayColor(data.delay)]">
                    {{ data.delay }} ms
                  </span>
                  <span v-else>—</span>
                </td>
                <td class="num">{{ timeAgo(data?.last_try_time) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* ---- Wrapper ---- */
.metrics-wrapper {
    padding: 16px;
    min-height: 100%;
    font-family: var(--font);
    color: var(--primary-text);
}

/* ---- Unavailable ---- */
.metrics-unavailable {
    text-align: center;
    padding: 60px 16px;
    color: var(--text-gray);
}

.unavailable-icon {
    margin-bottom: 16px;
    opacity: 0.7;
}

.metrics-unavailable p {
    margin: 8px 0;
    font-size: var(--text-body);
}

.metrics-unavailable p:first-of-type {
    font-size: var(--text-h3);
    font-weight: 500;
    color: var(--primary-text);
}

.metrics-hint {
    font-size: var(--text-small);
    color: var(--help-text);
}

.metrics-hint code {
    background: var(--menu-background);
    border: 1px solid var(--stroke);
    border-radius: var(--radius-sm);
    padding: 2px 6px;
    font-family: var(--font-mono);
    font-size: var(--text-small);
}

/* ---- Loading dots ---- */
.metrics-loading {
    text-align: center;
    padding: 60px 16px;
}

.loading-dots {
    display: inline-flex;
    gap: 8px;
}

.loading-dots span {
    display: block;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--primary-color);
    animation: dot-pulse 1.4s ease-in-out infinite both;
}

.loading-dots span:nth-child(1) { animation-delay: 0s; }
.loading-dots span:nth-child(2) { animation-delay: 0.16s; }
.loading-dots span:nth-child(3) { animation-delay: 0.32s; }

@keyframes dot-pulse {
    0%, 80%, 100% { transform: scale(0.4); opacity: 0.3; }
    40% { transform: scale(1); opacity: 1; }
}

/* ---- Content ---- */
.metrics-content {
    max-width: 100%;
}

.metrics-header {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    margin-bottom: 24px;
}

.metrics-header h2 {
    margin: 0;
    font-size: var(--text-h2);
    font-weight: 600;
    color: var(--primary-text);
}

.metrics-updated {
    font-size: var(--text-small);
    color: var(--text-dim);
}

/* ---- Cards grid ---- */
.metrics-cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 16px;
    margin-bottom: 24px;
}

.metric-card {
    background: var(--menu-background);
    border: 1px solid var(--stroke);
    border-radius: var(--radius);
    padding: 20px;
    text-align: center;
    box-shadow: var(--shadow-md);
    transition: box-shadow 0.2s ease;
}

.metric-card:hover {
    box-shadow: var(--shadow);
}

.metric-icon {
    width: 24px;
    height: 24px;
    margin: 0 auto 12px;
    stroke-width: 1.5;
    opacity: 0.85;
}

.metric-value {
    font-size: var(--text-h3);
    font-weight: 700;
    color: var(--primary-color);
    line-height: var(--lh-h3);
}

.metric-label {
    font-size: var(--text-small);
    color: var(--text-dim);
    margin-top: 6px;
}

/* ---- Sections ---- */
.metrics-section {
    margin-bottom: 24px;
}

.metrics-section h3 {
    margin: 0 0 12px;
    font-size: var(--text-h3);
    font-weight: 500;
    color: var(--primary-text);
}

/* ---- Table wrapper (rounded corners) ---- */
.metrics-table-wrapper {
    border-radius: var(--radius);
    overflow: hidden;
    border: 1px solid var(--table-border);
}

/* ---- Table ---- */
.metrics-table {
    width: 100%;
    border-collapse: collapse;
    background: var(--menu-background);
    font-size: var(--text-body);
}

.metrics-table thead {
    background: var(--table-header);
}

.metrics-table th {
    padding: 10px 16px;
    text-align: left;
    font-size: var(--text-small);
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--text-dim);
    font-weight: 600;
}

.metrics-table td {
    padding: 10px 16px;
    text-align: left;
    border-bottom: 1px solid var(--table-border);
    color: var(--primary-text);
}

.metrics-table tbody tr:last-child td {
    border-bottom: none;
}

.metrics-table tbody tr:hover {
    background: var(--table-row-hover);
}

.metrics-table .num {
    text-align: right;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
}

.metrics-table .tag-cell {
    font-family: var(--font-mono);
    color: var(--primary-color);
    font-weight: 500;
}

/* ---- Sparkline bar ---- */
.sparkline {
    height: 4px;
    border-radius: 2px;
    background: var(--primary-color);
    opacity: 0.3;
    min-width: 2px;
    margin-top: 4px;
    transition: width 0.3s ease;
}

/* ---- Status badges ---- */
.status-badge {
    display: inline-block;
    padding: 3px 10px;
    border-radius: 12px;
    font-size: var(--text-small);
    font-weight: 600;
    line-height: 1.5;
}

.status-badge.status-alive {
    color: var(--status-success-text);
    background: var(--status-success-background);
}

.status-badge.status-dead {
    color: var(--error);
    background: var(--status-warning-background);
}

/* ---- Delay indicator ---- */
.delay-cell {
    font-variant-numeric: tabular-nums;
}

.delay-cell.green {
    color: var(--indicator-online);
    font-weight: 600;
}

.delay-cell.yellow {
    color: var(--status-caution-text);
    font-weight: 600;
}

.delay-cell.red {
    color: var(--error);
    font-weight: 600;
}
</style>
