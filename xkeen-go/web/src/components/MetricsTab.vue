<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { getMetricsStats, getMetricsObservatory } from '../services/metrics.js';

const stats = ref(null);
const observatory = ref(null);
const available = ref(false);
const loading = ref(true);
const lastUpdate = ref(null);
let pollTimer = null;

function formatBytes(bytes) {
    if (bytes == null || bytes === 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(1024));
    return (Math.abs(bytes) / Math.pow(1024, i)).toFixed(1) + ' ' + units[i];
}

function delayColor(ms) {
    if (ms < 200) return 'green';
    if (ms < 500) return 'yellow';
    return 'red';
}

function timeAgo(unix) {
    if (!unix) return '—';
    const diff = Math.floor(Date.now() / 1000 - unix);
    if (diff < 60) return diff + ' сек назад';
    if (diff < 3600) return Math.floor(diff / 60) + ' мин назад';
    return Math.floor(diff / 3600) + ' ч назад';
}

const totalDown = computed(() => {
    if (!stats.value?.outbound) return 0;
    let s = 0;
    for (const v of Object.values(stats.value.outbound)) s += v?.downlink || 0;
    return s;
});

const totalUp = computed(() => {
    if (!stats.value?.outbound) return 0;
    let s = 0;
    for (const v of Object.values(stats.value.outbound)) s += v?.uplink || 0;
    return s;
});

const outboundList = computed(() => {
    if (!stats.value?.outbound) return [];
    return Object.entries(stats.value.outbound).sort((a, b) => (b[1]?.downlink || 0) - (a[1]?.downlink || 0));
});

const observatoryList = computed(() => {
    if (!observatory.value) return [];
    return Object.entries(observatory.value);
});

const maxDown = computed(() => {
    if (!stats.value?.outbound) return 1;
    let m = 0;
    for (const v of Object.values(stats.value.outbound)) if (v?.downlink > m) m = v.downlink;
    return m || 1;
});

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
    } catch {
        available.value = false;
    } finally {
        loading.value = false;
    }
}

onMounted(() => { poll(); pollTimer = setInterval(poll, 3000); });
onUnmounted(() => { if (pollTimer) clearInterval(pollTimer); });
</script>

<template>
  <div class="metrics-page">
    <!-- Unavailable -->
    <div v-if="!loading && !available" class="metrics-empty">
      <svg xmlns="http://www.w3.org/2000/svg" width="36" height="36" viewBox="0 0 24 24" fill="none"
           stroke="var(--error)" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
        <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
        <line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
      </svg>
      <p>Метрики недоступны</p>
      <span>Включите <code>metrics_port</code> в настройках и перезапустите Xray</span>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="metrics-loading">
      <span></span><span></span><span></span>
    </div>

    <!-- Dashboard -->
    <template v-if="available">
      <!-- Header -->
      <div class="metrics-header">
        <h2>Метрики Xray</h2>
        <span v-if="lastUpdate" class="metrics-updated">{{ lastUpdate.toLocaleTimeString() }}</span>
      </div>

      <!-- Summary cards -->
      <div class="metrics-cards">
        <div class="m-card">
          <div class="m-card-icon">
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
              <polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>
            </svg>
          </div>
          <div class="m-card-body">
            <div class="m-card-val">{{ formatBytes(totalDown) }}</div>
            <div class="m-card-lbl">Downlink</div>
          </div>
        </div>

        <div class="m-card">
          <div class="m-card-icon up">
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
              <polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/>
            </svg>
          </div>
          <div class="m-card-body">
            <div class="m-card-val">{{ formatBytes(totalUp) }}</div>
            <div class="m-card-lbl">Uplink</div>
          </div>
        </div>

        <div class="m-card">
          <div class="m-card-icon out">
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 2L2 7l10 5 10-5-10-5z"/>
              <path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/>
            </svg>
          </div>
          <div class="m-card-body">
            <div class="m-card-val">{{ outboundList.length }}</div>
            <div class="m-card-lbl">Outbound</div>
          </div>
        </div>

        <div class="m-card">
          <div class="m-card-icon obs">
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
              <circle cx="12" cy="12" r="3"/>
            </svg>
          </div>
          <div class="m-card-body">
            <div class="m-card-val">{{ observatoryList.length }}</div>
            <div class="m-card-lbl">Observatory</div>
          </div>
        </div>
      </div>

      <!-- Outbound traffic -->
      <div v-if="outboundList.length" class="metrics-section">
        <h3>Трафик по outbound</h3>
        <div class="metrics-table-wrap">
          <table>
            <thead>
              <tr><th>Тег</th><th class="r">Downlink</th><th class="r">Uplink</th><th class="r">Всего</th></tr>
            </thead>
            <tbody>
              <tr v-for="[tag, d] in outboundList" :key="tag">
                <td class="tag">{{ tag }}</td>
                <td class="r">
                  <span>{{ formatBytes(d?.downlink || 0) }}</span>
                  <div class="bar" :style="{ width: ((d?.downlink || 0) / maxDown * 100) + '%' }"></div>
                </td>
                <td class="r">{{ formatBytes(d?.uplink || 0) }}</td>
                <td class="r">{{ formatBytes((d?.downlink || 0) + (d?.uplink || 0)) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Observatory -->
      <div v-if="observatoryList.length" class="metrics-section">
        <h3>Observatory</h3>
        <div class="metrics-table-wrap">
          <table>
            <thead>
              <tr><th>Тег</th><th>Статус</th><th class="r">Задержка</th><th>Проверка</th></tr>
            </thead>
            <tbody>
              <tr v-for="[tag, d] in observatoryList" :key="tag">
                <td class="tag">{{ tag }}</td>
                <td>
                  <span :class="['badge', d?.alive ? 'alive' : 'dead']">{{ d?.alive ? 'Alive' : 'Dead' }}</span>
                </td>
                <td class="r">
                  <span v-if="d?.delay != null" :class="['delay', delayColor(d.delay)]">{{ d.delay }} ms</span>
                  <span v-else>—</span>
                </td>
                <td class="r light">{{ timeAgo(d?.last_try_time) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Debug info -->
      <div v-if="stats?.debug" class="metrics-debug">{{ stats.debug }}</div>
    </template>
  </div>
</template>

<style scoped>
/* Page */
.metrics-page {
  padding: 16px;
  overflow-y: auto;
  font-family: var(--font);
  color: var(--primary-text);
}

/* Empty state */
.metrics-empty {
  text-align: center;
  padding: 64px 16px;
  color: var(--text-gray);
}
.metrics-empty svg { opacity: .5; margin-bottom: 12px; }
.metrics-empty p { font-size: var(--text-h4); font-weight: 500; color: var(--primary-text); margin: 0 0 6px; }
.metrics-empty span { font-size: var(--text-small); color: var(--help-text); }
.metrics-empty code {
  background: var(--menu-background);
  border: 1px solid var(--stroke);
  border-radius: var(--radius-sm);
  padding: 1px 5px;
  font-family: var(--font-mono);
  font-size: var(--text-small);
}

/* Loading */
.metrics-loading {
  display: flex;
  justify-content: center;
  gap: 6px;
  padding: 64px 16px;
}
.metrics-loading span {
  width: 8px; height: 8px;
  border-radius: 50%;
  background: var(--primary-color);
  animation: dotPulse 1.4s ease-in-out infinite both;
}
.metrics-loading span:nth-child(2) { animation-delay: .16s; }
.metrics-loading span:nth-child(3) { animation-delay: .32s; }
@keyframes dotPulse {
  0%, 80%, 100% { transform: scale(.35); opacity: .25; }
  40% { transform: scale(1); opacity: 1; }
}

/* Header */
.metrics-header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 16px;
}
.metrics-header h2 {
  margin: 0;
  font-size: var(--text-h4);
  font-weight: 600;
}
.metrics-updated {
  font-size: var(--text-small);
  color: var(--help-text);
}

/* ---- Cards ---- */
.metrics-cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 20px;
}
@media (max-width: 640px) { .metrics-cards { grid-template-columns: repeat(2, 1fr); } }

.m-card {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  padding: 14px 16px;
  display: flex;
  align-items: center;
  gap: 12px;
  box-shadow: var(--box-shadow-3);
}
.m-card-icon {
  flex-shrink: 0;
  width: 36px; height: 36px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--status-special-background);
  color: var(--primary-color);
}
.m-card-icon.up { background: var(--status-caution-background); color: var(--status-caution-text); }
.m-card-icon.out { background: var(--status-success-background); color: var(--status-success-text); }
.m-card-icon.obs { background: var(--status-special-background); color: var(--primary-color); }

.m-card-body { min-width: 0; }
.m-card-val {
  font-size: var(--text-h4);
  font-weight: 700;
  color: var(--primary-text);
  line-height: 1.3;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.m-card-lbl {
  font-size: var(--text-small);
  color: var(--help-text);
  line-height: 1.3;
}

/* ---- Sections ---- */
.metrics-section {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  padding: 0;
  margin-bottom: 16px;
  box-shadow: var(--box-shadow-3);
}
.metrics-section h3 {
  font-size: var(--text-body);
  margin: 0;
  padding: 12px 16px 8px;
  font-weight: 600;
  color: var(--primary-text);
}

/* ---- Table ---- */
.metrics-table-wrap {
  border-radius: 0 0 var(--radius) var(--radius);
  overflow-x: auto;
}
.metrics-section table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--text-body);
}
.metrics-section thead {
  background: var(--table-header);
}
.metrics-section th {
  padding: 8px 16px;
  text-align: left;
  font-size: var(--text-small);
  text-transform: uppercase;
  letter-spacing: .04em;
  color: var(--help-text);
  font-weight: 600;
  border-bottom: 1px solid var(--table-border);
}
.metrics-section td {
  padding: 8px 16px;
  border-bottom: 1px solid var(--table-border);
}
.metrics-section tbody tr:last-child td { border-bottom: none; }
.metrics-section tbody tr:hover { background: var(--table-row-hover); }
.metrics-section .r { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }
.metrics-section .tag {
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--primary-color);
  font-weight: 500;
}
.metrics-section .light { color: var(--help-text); }

/* Sparkline */
.bar {
  height: 3px;
  border-radius: 2px;
  background: var(--primary-color);
  opacity: .2;
  margin-top: 3px;
  min-width: 2px;
}

/* Badges */
.badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 10px;
  font-size: var(--text-small);
  font-weight: 600;
  line-height: 1.5;
}
.badge.alive { color: var(--status-success-text); background: var(--status-success-background); }
.badge.dead  { color: var(--error); background: var(--status-warning-background); }

/* Delay */
.delay { font-variant-numeric: tabular-nums; }
.delay.green  { color: var(--indicator-online); }
.delay.yellow { color: var(--status-caution-text); }
.delay.red    { color: var(--error); }

/* Debug */
.metrics-debug {
  margin-top: 8px;
  padding: 8px 12px;
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius-sm);
  font-size: var(--text-small);
  color: var(--help-text);
  font-family: var(--font-mono);
}
</style>
