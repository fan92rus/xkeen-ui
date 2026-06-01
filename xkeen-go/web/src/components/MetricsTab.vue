<script setup>
import { ref, computed, onMounted, onUnmounted, shallowRef } from 'vue';
import { Chart, registerables } from 'chart.js';
import { Line, Bar, Doughnut } from 'vue-chartjs';
import { getMetricsStats, getMetricsObservatory } from '../services/metrics.js';

Chart.register(...registerables);

// ---- Constants ----
const HISTORY_SIZE = 60;
const POLL_INTERVAL = 3000;

// ---- Core state ----
const stats = ref(null);
const observatory = ref(null);
const available = ref(false);
const loading = ref(true);
const lastUpdate = ref(null);
let pollTimer = null;

// ---- Traffic history ring buffers (delta bytes per interval) ----
const historyDown = ref([]);
const historyUp = ref([]);
let prevDown = 0;
let prevUp = 0;

// ---- Sort state ----
const sortKey = ref('downlink');

// ---- Format helpers ----
function formatBytes(bytes) {
  if (bytes == null || bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(1024));
  return (Math.abs(bytes) / Math.pow(1024, i)).toFixed(1) + ' ' + units[i];
}

function formatBytesShort(bytes) {
  if (bytes == null || bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(1024));
  const val = Math.abs(bytes) / Math.pow(1024, i);
  return (val >= 100 ? val.toFixed(0) : val.toFixed(1)) + ' ' + units[i];
}

function timeAgo(unix) {
  if (!unix) return '—';
  const diff = Math.floor(Date.now() / 1000 - unix);
  if (diff < 0) return 'сейчас';
  if (diff < 60) return diff + ' сек назад';
  if (diff < 3600) return Math.floor(diff / 60) + ' мин назад';
  return Math.floor(diff / 3600) + ' ч назад';
}

function delayColorClass(ms) {
  if (ms == null) return '';
  if (ms < 200) return 'green';
  if (ms < 500) return 'yellow';
  return 'red';
}

// ---- Chart.js global defaults for dark theme ----
Chart.defaults.color = '#8899aa';
Chart.defaults.borderColor = 'rgba(255,255,255,0.04)';
Chart.defaults.font.family = 'Roboto, -apple-system, BlinkMacSystemFont, Segoe UI, system-ui, sans-serif';
Chart.defaults.font.size = 11;
Chart.defaults.plugins.legend.display = false;
Chart.defaults.animation.duration = 400;

// ---- Computed: totals ----
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

// ---- Computed: delta since last poll ----
const deltaDown = computed(() => {
  const h = historyDown.value;
  return h.length >= 2 ? h[h.length - 1] - h[h.length - 2] : 0;
});

const deltaUp = computed(() => {
  const h = historyUp.value;
  return h.length >= 2 ? h[h.length - 2] - h[h.length - 1] : 0;
});

// ---- Computed: outbound list sorted ----
const outboundList = computed(() => {
  if (!stats.value?.outbound) return [];
  return Object.entries(stats.value.outbound).sort((a, b) => {
    const av = sortKey.value === 'total'
      ? (a[1]?.downlink || 0) + (a[1]?.uplink || 0)
      : a[1]?.[sortKey.value] || 0;
    const bv = sortKey.value === 'total'
      ? (b[1]?.downlink || 0) + (b[1]?.uplink || 0)
      : b[1]?.[sortKey.value] || 0;
    return bv - av;
  });
});

const maxDown = computed(() => {
  if (!stats.value?.outbound) return 1;
  let m = 0;
  for (const v of Object.values(stats.value.outbound)) if (v?.downlink > m) m = v.downlink;
  return m || 1;
});

// ---- Computed: observatory ----
const observatoryList = computed(() => {
  if (!observatory.value) return [];
  return Object.entries(observatory.value);
});

const aliveCount = computed(() => observatoryList.value.filter(([, d]) => d?.alive).length);
const deadCount = computed(() => observatoryList.value.length - aliveCount.value);
const healthRatio = computed(() => {
  const total = observatoryList.value.length;
  if (!total) return '';
  return aliveCount.value + '/' + total;
});
const healthPercent = computed(() => {
  const total = observatoryList.value.length;
  if (!total) return 0;
  return Math.round((aliveCount.value / total) * 100);
});

// ---- Chart data: Download sparkline ----
const sparkDownData = computed(() => ({
  labels: historyDown.value.map((_, i) => i),
  datasets: [{
    data: [...historyDown.value],
    borderColor: '#3ddc84',
    backgroundColor: 'rgba(61,220,132,0.08)',
    borderWidth: 1.5,
    fill: true,
    tension: 0.4,
    pointRadius: 0,
    pointHitRadius: 0
  }]
}));

const sparkDownOptions = {
  responsive: true,
  maintainAspectRatio: false,
  scales: {
    x: { display: false },
    y: { display: false }
  },
  plugins: { tooltip: { enabled: false }, legend: { display: false } },
  elements: { line: { borderCapStyle: 'round' } },
  animation: { duration: 300 }
};

// ---- Chart data: Upload sparkline ----
const sparkUpData = computed(() => ({
  labels: historyUp.value.map((_, i) => i),
  datasets: [{
    data: [...historyUp.value],
    borderColor: '#0097dc',
    backgroundColor: 'rgba(0,151,220,0.08)',
    borderWidth: 1.5,
    fill: true,
    tension: 0.4,
    pointRadius: 0,
    pointHitRadius: 0
  }]
}));

const sparkUpOptions = sparkDownOptions;

// ---- Chart data: Outbound Bar (top 10) ----
const outboundChartData = computed(() => {
  const top10 = outboundList.value.slice(0, 10).reverse();
  return {
    labels: top10.map(([tag]) => tag),
    datasets: [{
      data: top10.map(([, d]) => d?.downlink || 0),
      backgroundColor: top10.map((_, i) => {
        const colors = [
          'rgba(0,151,220,0.7)', 'rgba(61,220,132,0.7)', 'rgba(255,187,87,0.7)',
          'rgba(222,61,61,0.7)', 'rgba(61,80,115,0.7)', 'rgba(37,196,120,0.7)',
          'rgba(108,124,133,0.7)', 'rgba(0,91,179,0.7)', 'rgba(160,120,220,0.7)',
          'rgba(220,160,60,0.7)'
        ];
        return colors[i % colors.length];
      }),
      borderRadius: 3,
      barThickness: 18
    }]
  };
});

const outboundBarOptions = {
  responsive: true,
  maintainAspectRatio: false,
  indexAxis: 'y',
  scales: {
    x: {
      display: true,
      grid: { display: false },
      ticks: {
        callback: (v) => formatBytesShort(v),
        maxTicksLimit: 4,
        font: { size: 10 }
      }
    },
    y: {
      grid: { display: false },
      ticks: {
        font: { size: 11, family: "'JetBrains Mono', 'Fira Code', Consolas, monospace" },
        color: '#8899aa'
      }
    }
  },
  plugins: {
    tooltip: {
      callbacks: {
        label: (ctx) => formatBytes(ctx.raw)
      }
    },
    legend: { display: false }
  },
  animation: { duration: 400 }
};

// ---- Chart data: Observatory doughnut ----
const doughnutData = computed(() => ({
  labels: ['Alive', 'Dead'],
  datasets: [{
    data: [aliveCount.value, deadCount.value],
    backgroundColor: ['#3ddc84', 'rgba(222,61,61,0.7)'],
    borderColor: ['rgba(61,220,132,0.3)', 'rgba(222,61,61,0.3)'],
    borderWidth: 1,
    hoverOffset: 4
  }]
}));

const doughnutOptions = {
  responsive: true,
  maintainAspectRatio: false,
  cutout: '65%',
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (ctx) => ctx.label + ': ' + ctx.raw
      }
    }
  },
  animation: { duration: 400 }
};

// ---- Sort toggle ----
function cycleSort() {
  const keys = ['downlink', 'uplink', 'total'];
  const idx = keys.indexOf(sortKey.value);
  sortKey.value = keys[(idx + 1) % keys.length];
}

const sortLabel = computed(() => {
  const map = { downlink: 'Downlink', uplink: 'Uplink', total: 'Всего' };
  return map[sortKey.value] || 'Downlink';
});

// ---- Outbound share ----
function outboundShare(d) {
  const total = totalDown.value;
  if (!total) return '0.0%';
  return ((d?.downlink || 0) / total * 100).toFixed(1) + '%';
}

// ---- Poll ----
async function poll() {
  try {
    const [s, o] = await Promise.all([getMetricsStats(), getMetricsObservatory()]);
    if (s?.available) {
      stats.value = s;
      observatory.value = o?.available ? o.results : null;
      available.value = true;
      lastUpdate.value = new Date();

      const newDown = totalDown.value;
      const newUp = totalUp.value;
      const dDown = newDown - prevDown;
      const dUp = newUp - prevUp;
      historyDown.value = historyDown.value.concat([Math.max(0, dDown)]).slice(-HISTORY_SIZE);
      historyUp.value = historyUp.value.concat([Math.max(0, dUp)]).slice(-HISTORY_SIZE);
      prevDown = newDown;
      prevUp = newUp;
    } else {
      available.value = false;
    }
  } catch {
    // keep last state
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  poll();
  pollTimer = setInterval(poll, POLL_INTERVAL);
});

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer);
});
</script>

<template>
  <div class="metrics-page">
    <!-- Unavailable -->
    <div v-if="!loading && !available" class="metrics-empty">
      <div class="metrics-empty__icon">&#9888;</div>
      <p class="metrics-empty__title">Метрики недоступны</p>
      <span class="metrics-empty__hint">Включите <code>metrics_port</code> в настройках и перезапустите Xray</span>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="metrics-loading">
      <span class="metrics-loading__dot"></span>
      <span class="metrics-loading__dot"></span>
      <span class="metrics-loading__dot"></span>
    </div>

    <!-- Dashboard -->
    <div v-if="available" class="metrics-dashboard">

      <!-- Header -->
      <div class="metrics-header">
        <h2 class="metrics-header__title">Метрики Xray</h2>
        <div class="metrics-header__pulse">
          <span class="pulse-dot"></span>
        </div>
        <span v-if="lastUpdate" class="metrics-header__clock">{{ lastUpdate.toLocaleTimeString() }}</span>
      </div>

      <!-- Overview cards -->
      <section class="metrics-overview">
        <!-- Download card -->
        <div class="overview-card overview-card--down">
          <div class="overview-card__top">
            <span class="overview-card__label">Downlink</span>
            <span class="overview-card__value overview-card__value--down">{{ formatBytes(totalDown) }}</span>
          </div>
          <div class="overview-card__chart">
            <Line v-if="historyDown.length >= 2" :data="sparkDownData" :options="sparkDownOptions" />
          </div>
          <div :class="['overview-card__delta', deltaDown > 0 ? 'overview-card__delta--positive' : '']">
            {{ deltaDown > 0 ? '+' : '' }}{{ formatBytes(deltaDown) }}
          </div>
        </div>

        <!-- Upload card -->
        <div class="overview-card overview-card--up">
          <div class="overview-card__top">
            <span class="overview-card__label">Uplink</span>
            <span class="overview-card__value overview-card__value--up">{{ formatBytes(totalUp) }}</span>
          </div>
          <div class="overview-card__chart">
            <Line v-if="historyUp.length >= 2" :data="sparkUpData" :options="sparkUpOptions" />
          </div>
          <div :class="['overview-card__delta', deltaUp > 0 ? 'overview-card__delta--positive' : '']">
            {{ deltaUp > 0 ? '+' : '' }}{{ formatBytes(deltaUp) }}
          </div>
        </div>

        <!-- Outbound count card -->
        <div class="overview-card overview-card--stat">
          <div class="overview-card__stat-icon">&#9650;</div>
          <div class="overview-card__stat-body">
            <div class="overview-card__stat-value">{{ outboundList.length }}</div>
            <div class="overview-card__stat-label">Outbound</div>
          </div>
        </div>

        <!-- Proxy health card -->
        <div v-if="observatoryList.length" class="overview-card overview-card--stat">
          <div class="overview-card__stat-icon overview-card__stat-icon--health">&#9733;</div>
          <div class="overview-card__stat-body">
            <div class="overview-card__stat-value">{{ healthPercent }}%</div>
            <div class="overview-card__stat-label">Прокси {{ healthRatio }}</div>
          </div>
        </div>
      </section>

      <!-- Outbound traffic section -->
      <section v-if="outboundList.length" class="metrics-section">
        <div class="metrics-section__header">
          <h3 class="metrics-section__title">Трафик по outbound</h3>
          <button class="sort-toggle" @click="cycleSort" :title="'Sort: ' + sortLabel">
            <span class="sort-toggle__icon">⇅</span>
            <span class="sort-toggle__label">{{ sortLabel }}</span>
          </button>
        </div>

        <!-- Horizontal bar chart -->
        <div class="metrics-section__bar-chart">
          <Bar v-if="outboundList.length" :data="outboundChartData" :options="outboundBarOptions" />
        </div>

        <!-- Table -->
        <div class="metrics-table-wrap">
          <table>
            <thead>
              <tr>
                <th>Tag</th>
                <th class="cell--center">Share</th>
                <th class="cell--min140">Downlink</th>
                <th class="cell--right">Uplink</th>
                <th class="cell--right">Всего</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="[tag, d] in outboundList" :key="tag">
                <td class="cell--tag">{{ tag }}</td>
                <td class="cell--center">
                  <span class="share-badge">{{ outboundShare(d) }}</span>
                </td>
                <td>
                  <span class="dl-value">{{ formatBytes(d?.downlink || 0) }}</span>
                  <div class="progress-track">
                    <div class="progress-fill"
                         :style="{ width: ((d?.downlink || 0) / maxDown * 100) + '%' }"></div>
                  </div>
                </td>
                <td class="cell--right">{{ formatBytes(d?.uplink || 0) }}</td>
                <td class="cell--right">{{ formatBytes((d?.downlink || 0) + (d?.uplink || 0)) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- Observatory section -->
      <section v-if="observatoryList.length" class="metrics-section">
        <div class="metrics-section__header">
          <h3 class="metrics-section__title">Observatory</h3>
          <div class="health-badges">
            <span class="health-badge health-badge--alive">{{ aliveCount }} alive</span>
            <span v-if="deadCount" class="health-badge health-badge--dead">{{ deadCount }} dead</span>
          </div>
        </div>

        <div class="observatory-layout">
          <!-- Doughnut chart -->
          <div class="observatory-doughnut">
            <Doughnut :data="doughnutData" :options="doughnutOptions" />
            <div class="observatory-doughnut__center">
              <span class="observatory-doughnut__pct">{{ healthPercent }}%</span>
            </div>
          </div>

          <!-- Proxy cards grid -->
          <div class="observatory-grid">
            <div v-for="[tag, d] in observatoryList" :key="tag"
                 :class="['obs-card', d?.alive ? 'obs-card--alive' : 'obs-card--dead']">
              <div class="obs-card__header">
                <span :class="['obs-card__dot', d?.alive ? 'obs-card__dot--alive' : 'obs-card__dot--dead']"></span>
                <span class="obs-card__tag">{{ tag }}</span>
              </div>
              <div class="obs-card__latency-bar">
                <div v-if="d?.alive && d?.delay != null"
                     :class="['obs-card__latency-fill', delayColorClass(d.delay)]"
                     :style="{ width: Math.min(d.delay / 1000 * 100, 100) + '%' }"></div>
              </div>
              <div class="obs-card__latency-value">
                <span v-if="d?.delay != null && d?.alive"
                      :class="['delay-value', delayColorClass(d.delay)]">{{ d.delay }} ms</span>
                <span v-else class="delay-value delay-value--none">&mdash;</span>
              </div>
              <div class="obs-card__time">{{ timeAgo(d?.last_try_time) }}</div>
            </div>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
/* ===== Page ===== */
.metrics-page {
  padding: 16px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  color: var(--primary-text);
}

/* ===== Empty state ===== */
.metrics-empty {
  text-align: center;
  padding: 64px 16px;
  color: var(--text-gray);
}
.metrics-empty__icon {
  font-size: 36px;
  color: var(--error);
  opacity: 0.6;
  margin-bottom: 12px;
}
.metrics-empty__title {
  font-size: var(--text-h4);
  font-weight: 500;
  color: var(--primary-text);
  margin: 0 0 6px;
}
.metrics-empty__hint {
  font-size: var(--text-small);
  color: var(--help-text);
}
.metrics-empty__hint code {
  background: var(--menu-background);
  border: 1px solid var(--stroke);
  border-radius: var(--radius-sm);
  padding: 1px 5px;
  font-family: var(--font-mono);
  font-size: var(--text-small);
}

/* ===== Loading ===== */
.metrics-loading {
  display: flex;
  justify-content: center;
  gap: 6px;
  padding: 64px 16px;
}
.metrics-loading__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--primary-color);
  animation: dotPulse 1.4s ease-in-out infinite both;
}
.metrics-loading__dot:nth-child(2) { animation-delay: 0.16s; }
.metrics-loading__dot:nth-child(3) { animation-delay: 0.32s; }
@keyframes dotPulse {
  0%, 80%, 100% { transform: scale(0.35); opacity: 0.25; }
  40% { transform: scale(1); opacity: 1; }
}

/* ===== Dashboard ===== */
.metrics-dashboard {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* ===== Header ===== */
.metrics-header {
  display: flex;
  align-items: center;
  height: 32px;
  flex-shrink: 0;
}
.metrics-header__title {
  margin: 0;
  font-size: var(--text-h4);
  font-weight: 600;
}
.metrics-header__pulse {
  margin-left: 12px;
}
.pulse-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--indicator-online);
  animation: livePulse 2s ease-in-out infinite;
}
@keyframes livePulse {
  0%, 100% { opacity: 1; box-shadow: 0 0 4px var(--indicator-online); }
  50% { opacity: 0.4; box-shadow: 0 0 8px var(--indicator-online); }
}
@media (prefers-reduced-motion: reduce) {
  .pulse-dot { animation: none; }
}
.metrics-header__clock {
  margin-left: auto;
  font-size: var(--text-small);
  color: var(--help-text);
  white-space: nowrap;
}

/* ===== Overview cards ===== */
.metrics-overview {
  display: grid;
  grid-template-columns: 1fr 1fr auto auto;
  gap: 12px;
}

.overview-card {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  padding: 14px 16px 10px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  transition: box-shadow 0.15s ease;
}
.overview-card--stat {
  flex-direction: row;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
}

.overview-card__top {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
}
.overview-card__label {
  font-size: var(--text-small);
  color: var(--help-text);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}
.overview-card__value {
  font-size: var(--text-h3);
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.overview-card__value--down { color: #3ddc84; }
.overview-card__value--up { color: var(--primary-color); }

.overview-card__chart {
  width: 100%;
  height: 44px;
  margin-top: 4px;
  position: relative;
}

.overview-card__delta {
  font-size: var(--text-small);
  font-variant-numeric: tabular-nums;
  color: var(--help-text);
}
.overview-card__delta--positive {
  color: #3ddc84;
}

/* Stat card inner */
.overview-card__stat-icon {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--status-special-background);
  color: var(--primary-color);
  font-size: 14px;
  flex-shrink: 0;
}
.overview-card__stat-icon--health {
  color: #3ddc84;
}
.overview-card__stat-body {
  min-width: 0;
}
.overview-card__stat-value {
  font-size: var(--text-h4);
  font-weight: 700;
  line-height: 1.3;
}
.overview-card__stat-label {
  font-size: var(--text-small);
  color: var(--help-text);
  line-height: 1.3;
}

/* ===== Section ===== */
.metrics-section {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  overflow: hidden;
}
.metrics-section__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px 8px;
}
.metrics-section__title {
  font-size: var(--text-body);
  font-weight: 600;
  margin: 0;
  color: var(--primary-text);
}

/* Sort toggle */
.sort-toggle {
  display: flex;
  align-items: center;
  gap: 5px;
  background: none;
  border: 1px solid var(--menu-border);
  border-radius: var(--radius-sm);
  padding: 3px 8px;
  color: var(--help-text);
  font-size: var(--text-small);
  cursor: pointer;
  font-family: var(--font);
  transition: color 0.15s ease, border-color 0.15s ease;
}
.sort-toggle:hover {
  color: var(--primary-color);
  border-color: var(--primary-color);
}
.sort-toggle__icon {
  font-size: 14px;
}

/* Bar chart container */
.metrics-section__bar-chart {
  padding: 4px 16px 12px;
  height: 220px;
  position: relative;
}

/* ===== Table ===== */
.metrics-table-wrap {
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
  letter-spacing: 0.04em;
  color: var(--help-text);
  font-weight: 600;
  border-bottom: 1px solid var(--table-border);
}
.metrics-section td {
  padding: 8px 16px;
  border-bottom: 1px solid var(--table-border);
}
.metrics-section tbody tr:last-child td {
  border-bottom: none;
}
.metrics-section tbody tr:hover {
  background: var(--table-row-hover);
}

/* Table cell helpers */
.cell--tag {
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--primary-color);
  font-weight: 500;
}
.cell--right {
  text-align: right;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.cell--center {
  text-align: center;
}
.cell--min140 {
  min-width: 140px;
}

/* Share badge */
.share-badge {
  font-size: var(--text-small);
  color: var(--help-text);
  font-variant-numeric: tabular-nums;
}

/* Downlink column with inline bar */
.dl-value {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  display: block;
}
.progress-track {
  height: 4px;
  border-radius: 2px;
  background: var(--progressbar-background);
  margin-top: 3px;
  min-width: 60px;
  overflow: hidden;
}
.progress-fill {
  height: 100%;
  border-radius: 2px;
  background: linear-gradient(90deg, var(--primary-color), var(--primary-color-hover));
  transition: width 0.6s cubic-bezier(0.4, 0, 0.2, 1);
}

/* ===== Health badges ===== */
.health-badges {
  display: flex;
  gap: 8px;
}
.health-badge {
  padding: 2px 8px;
  border-radius: 10px;
  font-size: var(--text-small);
  font-weight: 600;
}
.health-badge--alive {
  color: var(--status-success-text);
  background: var(--status-success-background);
}
.health-badge--dead {
  color: var(--error);
  background: var(--status-warning-background);
}

/* ===== Observatory layout ===== */
.observatory-layout {
  display: grid;
  grid-template-columns: 160px 1fr;
  gap: 16px;
  padding: 0 16px 16px;
}

.observatory-doughnut {
  position: relative;
  height: 140px;
}
.observatory-doughnut__center {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  text-align: center;
  pointer-events: none;
}
.observatory-doughnut__pct {
  font-size: var(--text-h3);
  font-weight: 700;
  color: var(--primary-text);
}

/* Observatory card grid */
.observatory-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 8px;
}

/* Observatory card */
.obs-card {
  background: var(--dashboard-background, var(--menu-background));
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  transition: border-left-color 0.15s ease;
}
.obs-card--alive { border-left: 3px solid var(--indicator-online); }
.obs-card--dead { border-left: 3px solid var(--error); }

.obs-card__header {
  display: flex;
  align-items: center;
  gap: 8px;
}
.obs-card__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.obs-card__dot--alive {
  background: var(--indicator-online);
  box-shadow: 0 0 6px var(--indicator-online);
}
.obs-card__dot--dead {
  background: var(--error);
}
.obs-card__tag {
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--primary-color);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Latency bar */
.obs-card__latency-bar {
  height: 4px;
  border-radius: 2px;
  background: var(--progressbar-background);
  overflow: hidden;
}
.obs-card__latency-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.6s cubic-bezier(0.4, 0, 0.2, 1);
}
.obs-card__latency-fill.green { background: #3ddc84; }
.obs-card__latency-fill.yellow { background: var(--status-caution-text); }
.obs-card__latency-fill.red { background: var(--error); }

/* Latency value */
.obs-card__latency-value {
  font-size: var(--text-small);
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  align-self: flex-end;
}
.delay-value { font-variant-numeric: tabular-nums; }
.delay-value.green { color: #3ddc84; }
.delay-value.yellow { color: var(--status-caution-text); }
.delay-value.red { color: var(--error); }
.delay-value--none { color: var(--help-text); }

.obs-card__time {
  font-size: var(--text-small);
  color: var(--help-text);
  align-self: flex-end;
}

/* ===== RESPONSIVE ===== */
@media (max-width: 900px) {
  .metrics-overview {
    grid-template-columns: 1fr 1fr;
  }
  .sort-toggle__label {
    display: none;
  }
  .observatory-layout {
    grid-template-columns: 1fr;
  }
  .observatory-doughnut {
    height: 120px;
    max-width: 200px;
    margin: 0 auto;
  }
  .observatory-grid {
    grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  }
}

@media (max-width: 600px) {
  .metrics-page {
    padding: 12px;
  }
  .metrics-overview {
    grid-template-columns: 1fr;
  }
  .overview-card__chart {
    height: 36px;
  }
  .overview-card__value {
    font-size: var(--text-h4);
  }
  .observatory-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .metrics-section__bar-chart {
    height: 180px;
  }
  .cell--center {
    display: none;
  }
}

@media (max-width: 380px) {
  .observatory-grid {
    grid-template-columns: 1fr;
  }
  .overview-card__chart {
    height: 28px;
  }
  .metrics-section table th:nth-child(4),
  .metrics-section table td:nth-child(4) {
    display: none;
  }
}
</style>
