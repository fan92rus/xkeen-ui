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
  <div class="m-wrap">
    <!-- Unavailable -->
    <div v-if="!loading && !available" class="m-empty">
      <svg xmlns="http://www.w3.org/2000/svg" width="40" height="40" viewBox="0 0 24 24" fill="none"
           stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
           style="color:var(--error);opacity:.6;margin-bottom:16px">
        <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
        <line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
      </svg>
      <p class="m-empty-title">Метрики недоступны</p>
      <p class="m-empty-hint">Проверьте настройку <code>metrics_port</code> в конфигурации</p>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="m-loading">
      <span class="dot"></span><span class="dot"></span><span class="dot"></span>
    </div>

    <!-- Content -->
    <div v-if="available" class="m-content">
      <div class="m-head">
        <h2>Метрики Xray</h2>
        <span v-if="lastUpdate" class="m-time">{{ lastUpdate.toLocaleTimeString() }}</span>
      </div>

      <!-- Cards -->
      <div class="m-cards">
        <div class="m-card">
          <div class="m-card-icon" style="color:var(--primary-color)">
            <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
              <polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>
            </svg>
          </div>
          <div class="m-card-val">{{ formatBytes(totalDown) }}</div>
          <div class="m-card-lbl">Downlink</div>
        </div>

        <div class="m-card">
          <div class="m-card-icon" style="color:var(--primary-color)">
            <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
              <polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/>
            </svg>
          </div>
          <div class="m-card-val">{{ formatBytes(totalUp) }}</div>
          <div class="m-card-lbl">Uplink</div>
        </div>

        <div class="m-card">
          <div class="m-card-icon" style="color:var(--primary-color)">
            <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 2L2 7l10 5 10-5-10-5z"/>
              <path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/>
            </svg>
          </div>
          <div class="m-card-val">{{ outboundList.length }}</div>
          <div class="m-card-lbl">Outbound</div>
        </div>

        <div class="m-card">
          <div class="m-card-icon" style="color:var(--primary-color)">
            <svg xmlns="http://www.w3.org/2000/svg" width="22" height="22" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
              <circle cx="12" cy="12" r="3"/>
            </svg>
          </div>
          <div class="m-card-val">{{ observatoryList.length }}</div>
          <div class="m-card-lbl">Observatory</div>
        </div>
      </div>

      <!-- Outbound table -->
      <div class="m-section">
        <h3>Трафик по outbound</h3>
        <div class="m-tbl-wrap">
          <table class="m-tbl">
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
                <td class="tag">{{ tag }}</td>
                <td class="num">
                  <span>{{ formatBytes(data?.downlink || 0) }}</span>
                  <div class="bar" :style="{width: ((data?.downlink || 0) / maxDown * 100) + '%'}"></div>
                </td>
                <td class="num">{{ formatBytes(data?.uplink || 0) }}</td>
                <td class="num">{{ formatBytes((data?.downlink || 0) + (data?.uplink || 0)) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- Observatory table -->
      <div v-if="observatoryList.length" class="m-section">
        <h3>Observatory</h3>
        <div class="m-tbl-wrap">
          <table class="m-tbl">
            <thead>
              <tr>
                <th>Тег</th>
                <th>Статус</th>
                <th class="num">Задержка</th>
                <th>Проверка</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="[tag, data] in observatoryList" :key="tag">
                <td class="tag">{{ tag }}</td>
                <td>
                  <span :class="['badge', data?.alive ? 'ok' : 'fail']">{{ data?.alive ? 'Alive' : 'Dead' }}</span>
                </td>
                <td class="num">
                  <span v-if="data?.delay != null" :class="['ms', delayColor(data.delay)]">{{ data.delay }} ms</span>
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
/* Wrapper */
.m-wrap {
  padding: 16px;
  font-family: var(--font);
  color: var(--primary-text);
}

/* Unavailable */
.m-empty {
  text-align: center;
  padding: 64px 16px;
}
.m-empty-title {
  font-size: var(--text-h3);
  font-weight: 500;
  color: var(--primary-text);
  margin: 0 0 8px;
}
.m-empty-hint {
  font-size: var(--text-small);
  color: var(--help-text);
  margin: 0;
}
.m-empty-hint code {
  background: var(--menu-background);
  border: 1px solid var(--stroke);
  border-radius: var(--radius-sm);
  padding: 2px 6px;
  font-family: var(--font-mono);
}

/* Loading */
.m-loading { text-align: center; padding: 64px 16px; }
.m-loading .dot {
  display: inline-block;
  width: 8px; height: 8px;
  border-radius: 50%;
  background: var(--primary-color);
  margin: 0 4px;
  animation: pulse 1.4s ease-in-out infinite both;
}
.m-loading .dot:nth-child(2) { animation-delay: .16s; }
.m-loading .dot:nth-child(3) { animation-delay: .32s; }
@keyframes pulse {
  0%, 80%, 100% { transform: scale(.4); opacity: .3; }
  40% { transform: scale(1); opacity: 1; }
}

/* Content */
.m-content { max-width: 100%; }

.m-head {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  margin-bottom: 24px;
}
.m-head h2 {
  margin: 0;
  font-size: var(--text-h3);
  font-weight: 600;
}
.m-time {
  font-size: var(--text-small);
  color: var(--help-text);
}

/* Cards */
.m-cards {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 12px;
  margin-bottom: 24px;
}
@media (max-width: 720px) { .m-cards { grid-template-columns: repeat(2, 1fr); } }

.m-card {
  background: var(--menu-background);
  border: 1px solid var(--stroke);
  border-radius: var(--radius);
  padding: 16px;
  text-align: center;
}
.m-card-icon {
  margin: 0 auto 8px;
  width: 22px; height: 22px;
  display: flex; align-items: center; justify-content: center;
}
.m-card-icon svg { display: block; }
.m-card-val {
  font-size: var(--text-h4);
  font-weight: 700;
  color: var(--primary-color);
  line-height: 1.4;
}
.m-card-lbl {
  font-size: var(--text-small);
  color: var(--help-text);
  margin-top: 4px;
}

/* Sections */
.m-section { margin-bottom: 24px; }
.m-section h3 {
  margin: 0 0 8px;
  font-size: var(--text-body);
  font-weight: 600;
  color: var(--text-gray);
}

/* Table */
.m-tbl-wrap {
  border-radius: var(--radius);
  overflow: hidden;
  border: 1px solid var(--table-border);
}
.m-tbl {
  width: 100%;
  border-collapse: collapse;
  background: var(--menu-background);
  font-size: var(--text-body);
}
.m-tbl thead { background: var(--table-header); }
.m-tbl th {
  padding: 8px 16px;
  text-align: left;
  font-size: var(--text-small);
  text-transform: uppercase;
  letter-spacing: .03em;
  color: var(--help-text);
  font-weight: 600;
}
.m-tbl td {
  padding: 8px 16px;
  border-bottom: 1px solid var(--table-border);
}
.m-tbl tbody tr:last-child td { border-bottom: none; }
.m-tbl tbody tr:hover { background: var(--table-row-hover); }
.m-tbl .num {
  text-align: right;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.m-tbl .tag {
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--primary-color);
  font-weight: 500;
}

/* Sparkline bar */
.bar {
  height: 3px;
  border-radius: 2px;
  background: var(--primary-color);
  opacity: .25;
  margin-top: 4px;
  min-width: 2px;
}

/* Badges */
.badge {
  display: inline-block;
  padding: 2px 8px;
  border-radius: 10px;
  font-size: var(--text-small);
  font-weight: 600;
}
.badge.ok { color: var(--status-success-text); background: var(--status-success-background); }
.badge.fail { color: var(--error); background: var(--status-warning-background); }

/* Delay */
.ms { font-variant-numeric: tabular-nums; }
.ms.green { color: var(--indicator-online); }
.ms.yellow { color: var(--status-caution-text); }
.ms.red { color: var(--error); }
</style>
