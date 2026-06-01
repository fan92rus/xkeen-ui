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
Chart.defaults.animation.duration = 0;

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
    tension: 0,
    pointRadius: 0,
    pointHitRadius: 8
  }]
}));

const sparkDownOptions = {
  responsive: true,
  maintainAspectRatio: false,
  scales: {
    x: { display: false },
    y: { display: false }
  },
  plugins: {
    tooltip: {
      enabled: true,
      mode: 'index',
      intersect: false,
      backgroundColor: 'rgba(22,28,39,0.95)',
      borderColor: '#2e3d57',
      borderWidth: 1,
      titleFont: { size: 10 },
      bodyFont: { size: 11 },
      padding: 8,
      callbacks: {
        title: () => 'Downlink',
        label: (ctx) => ctx.datasetIndex === 0 ? formatBytes(ctx.raw) + '/интервал' : null
      }
    },
    legend: { display: false }
  },
  interaction: { mode: 'index', intersect: false },
  animation: { duration: 0 }
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
    tension: 0,
    pointRadius: 0,
    pointHitRadius: 8
  }]
}));

const sparkUpOptions = {
  responsive: true,
  maintainAspectRatio: false,
  scales: {
    x: { display: false },
    y: { display: false }
  },
  plugins: {
    tooltip: {
      enabled: true,
      mode: 'index',
      intersect: false,
      backgroundColor: 'rgba(22,28,39,0.95)',
      borderColor: '#2e3d57',
      borderWidth: 1,
      titleFont: { size: 10 },
      bodyFont: { size: 11 },
      padding: 8,
      callbacks: {
        title: () => 'Uplink',
        label: (ctx) => ctx.datasetIndex === 0 ? formatBytes(ctx.raw) + '/интервал' : null
      }
    },
    legend: { display: false }
  },
  interaction: { mode: 'index', intersect: false },
  animation: { duration: 0 }
};

// ---- Chart data: Outbound Bar (top 10) ----
const outboundChartData = computed(() => {
  const top10 = outboundList.value.slice(0, 10).reverse();
  return {
    labels: top10.map(([tag]) => tag),
    datasets: [{
      label: 'Downlink',
      data: top10.map(([, d]) => d?.downlink || 0),
      backgroundColor: 'rgba(0,151,220,0.6)',
      hoverBackgroundColor: 'rgba(0,151,220,0.85)',
      borderRadius: 3,
      barThickness: 16
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
      afterFit: (axis) => {
        axis.width = 120;
      },
      ticks: {
        font: { size: 10, family: "'JetBrains Mono', 'Fira Code', Consolas, monospace" },
        color: '#8899aa',
        autoSkip: false,
        padding: 2
      }
    }
  },
  plugins: {
    tooltip: {
      backgroundColor: 'rgba(22,28,39,0.95)',
      borderColor: '#2e3d57',
      borderWidth: 1,
      padding: 10,
      callbacks: {
        label: (ctx) => 'Downlink: ' + formatBytes(ctx.raw)
      }
    },
    legend: {
      display: true,
      position: 'top',
      align: 'end',
      labels: {
        boxWidth: 10,
        boxHeight: 10,
        padding: 12,
        font: { size: 10 },
        color: '#8899aa',
        usePointStyle: true,
        pointStyle: 'rectRounded'
      }
    }
  },
  animation: { duration: 0 }
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
    legend: {
      display: true,
      position: 'bottom',
      labels: {
        boxWidth: 10,
        boxHeight: 10,
        padding: 8,
        font: { size: 10 },
        color: '#8899aa',
        usePointStyle: true,
        pointStyle: 'circle'
      }
    },
    tooltip: {
      backgroundColor: 'rgba(22,28,39,0.95)',
      borderColor: '#2e3d57',
      borderWidth: 1,
      padding: 8,
      callbacks: {
        label: (ctx) => ctx.label + ': ' + ctx.raw
      }
    }
  },
  animation: { duration: 0 }
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

      <!-- Overview cards - 2x2 grid -->
      <section class="metrics-overview">
        <!-- Row 1: Download + Upload cards -->
        <div class="overview-card overview-card--down">
          <div class="overview-card__top">
            <span class="overview-card__label">
              <span class="legend-dot legend-dot--down"></span> Downlink
            </span>
            <span class="overview-card__value overview-card__value--down">{{ formatBytes(totalDown) }}</span>
          </div>
          <div class="overview-card__chart">
            <Line v-if="historyDown.length >= 2" :data="sparkDownData" :options="sparkDownOptions" />
          </div>
          <div :class="['overview-card__delta', deltaDown > 0 ? 'overview-card__delta--positive' : '']">
            {{ deltaDown > 0 ? '+' : '' }}{{ formatBytes(deltaDown) }}/интервал
          </div>
        </div>

        <div class="overview-card overview-card--up">
          <div class="overview-card__top">
            <span class="overview-card__label">
              <span class="legend-dot legend-dot--up"></span> Uplink
            </span>
            <span class="overview-card__value overview-card__value--up">{{ formatBytes(totalUp) }}</span>
          </div>
          <div class="overview-card__chart">
            <Line v-if="historyUp.length >= 2" :data="sparkUpData" :options="sparkUpOptions" />
          </div>
          <div :class="['overview-card__delta', deltaUp > 0 ? 'overview-card__delta--positive' : '']">
            {{ deltaUp > 0 ? '+' : '' }}{{ formatBytes(deltaUp) }}/интервал
          </div>
        </div>

        <!-- Row 2: Outbound count + Proxy health -->
        <div class="overview-card overview-card--stat">
          <div class="overview-card__stat-icon">&#9650;</div>
          <div class="overview-card__stat-body">
            <div class="overview-card__stat-value">{{ outboundList.length }}</div>
            <div class="overview-card__stat-label">Outbound</div>
          </div>
        </div>

        <div v-if="observatoryList.length" class="overview-card overview-card--stat">
          <div class="overview-card__stat-icon overview-card__stat-icon--health">&#9733;</div>
          <div class="overview-card__stat-body">
            <div class="overview-card__stat-value">{{ healthPercent }}%</div>
            <div class="overview-card__stat-label">Прокси {{ healthRatio }}</div>
          </div>
        </div>
      </section>

      <!-- Bottom sections: 2 columns -->
      <div class="metrics-bottom-grid">
        <!-- Outbound traffic section -->
        <section v-if="outboundList.length" class="metrics-section">
          <div class="metrics-section__header">
            <h3 class="metrics-section__title">Трафик по outbound</h3>
            <button class="sort-toggle" @click="cycleSort" :title="'Sort: ' + sortLabel">
              <span class="sort-toggle__icon">⇅</span>
              <span class="sort-toggle__label">{{ sortLabel }}</span>
            </button>
          </div>

          <div class="metrics-section__bar-chart">
            <Bar v-if="outboundList.length" :data="outboundChartData" :options="outboundBarOptions" />
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
            <div class="observatory-doughnut">
              <Doughnut :data="doughnutData" :options="doughnutOptions" />
              <div class="observatory-doughnut__center">
                <span class="observatory-doughnut__pct">{{ healthPercent }}%</span>
              </div>
            </div>

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
  </div>
</template>

<style scoped>
/* ===== Page ===== */
.metrics-page {
  padding: 12px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  color: var(--primary-text);
  height: 100%;
}

/* ===== Empty state ===== */
.metrics-empty {
  text-align: center;
  padding: 48px 16px;
  color: var(--text-gray);
}
.metrics-empty__icon {
  font-size: 32px;
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
  padding: 48px 16px;
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
  gap: 10px;
}

/* ===== Header ===== */
.metrics-header {
  display: flex;
  align-items: center;
  height: 28px;
  flex-shrink: 0;
}
.metrics-header__title {
  margin: 0;
  font-size: var(--text-body);
  font-weight: 600;
}
.metrics-header__pulse {
  margin-left: 10px;
}
.pulse-dot {
  display: inline-block;
  width: 7px;
  height: 7px;
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

/* ===== Legend dots ===== */
.legend-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 4px;
  vertical-align: middle;
}
.legend-dot--down { background: #3ddc84; }
.legend-dot--up { background: #0097dc; }

/* ===== Overview cards — 2x2 grid ===== */
.metrics-overview {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px;
}

.overview-card {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  padding: 10px 12px 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  transition: border-color 0.15s ease;
}
.overview-card--stat {
  flex-direction: row;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
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
  letter-spacing: 0.04em;
}
.overview-card__value {
  font-size: var(--text-h4);
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.overview-card__value--down { color: #3ddc84; }
.overview-card__value--up { color: var(--primary-color); }

.overview-card__chart {
  width: 100%;
  height: 40px;
  margin-top: 2px;
  position: relative;
}

.overview-card__delta {
  font-size: 10px;
  font-variant-numeric: tabular-nums;
  color: var(--help-text);
}
.overview-card__delta--positive {
  color: #3ddc84;
}

/* Stat card inner */
.overview-card__stat-icon {
  width: 32px;
  height: 32px;
  border-radius: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--status-special-background);
  color: var(--primary-color);
  font-size: 13px;
  flex-shrink: 0;
}
.overview-card__stat-icon--health {
  color: #3ddc84;
}
.overview-card__stat-body {
  min-width: 0;
}
.overview-card__stat-value {
  font-size: var(--text-body);
  font-weight: 700;
  line-height: 1.2;
}
.overview-card__stat-label {
  font-size: 10px;
  color: var(--help-text);
  line-height: 1.2;
}

/* ===== Bottom grid: 2 columns ===== */
.metrics-bottom-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

/* ===== Section ===== */
.metrics-section {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.metrics-section__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px 4px;
}
.metrics-section__title {
  font-size: var(--text-small);
  font-weight: 600;
  margin: 0;
  color: var(--primary-text);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

/* Sort toggle */
.sort-toggle {
  display: flex;
  align-items: center;
  gap: 4px;
  background: none;
  border: 1px solid var(--menu-border);
  border-radius: var(--radius-sm);
  padding: 2px 6px;
  color: var(--help-text);
  font-size: 10px;
  cursor: pointer;
  font-family: var(--font);
  transition: color 0.15s ease, border-color 0.15s ease;
}
.sort-toggle:hover {
  color: var(--primary-color);
  border-color: var(--primary-color);
}
.sort-toggle__icon {
  font-size: 12px;
}

/* Bar chart container */
.metrics-section__bar-chart {
  padding: 4px 10px 8px;
  height: 260px;
  position: relative;
  flex: 1;
  min-height: 0;
}

/* ===== Health badges ===== */
.health-badges {
  display: flex;
  gap: 6px;
}
.health-badge {
  padding: 1px 6px;
  border-radius: 8px;
  font-size: 10px;
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
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 0 12px 10px;
}

.observatory-doughnut {
  position: relative;
  height: 120px;
  max-width: 180px;
  margin: 0 auto;
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
  font-size: var(--text-h4);
  font-weight: 700;
  color: var(--primary-text);
}

/* Observatory card grid */
.observatory-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 6px;
}

/* Observatory card */
.obs-card {
  background: var(--dashboard-background, var(--menu-background));
  border: 1px solid var(--menu-border);
  border-radius: 6px;
  padding: 6px 8px;
  display: flex;
  flex-direction: column;
  gap: 3px;
  transition: border-left-color 0.15s ease;
}
.obs-card--alive { border-left: 2px solid var(--indicator-online); }
.obs-card--dead { border-left: 2px solid var(--error); }

.obs-card__header {
  display: flex;
  align-items: center;
  gap: 6px;
}
.obs-card__dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}
.obs-card__dot--alive {
  background: var(--indicator-online);
  box-shadow: 0 0 4px var(--indicator-online);
}
.obs-card__dot--dead {
  background: var(--error);
}
.obs-card__tag {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--primary-color);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Latency bar */
.obs-card__latency-bar {
  height: 3px;
  border-radius: 1.5px;
  background: var(--progressbar-background);
  overflow: hidden;
}
.obs-card__latency-fill {
  height: 100%;
  border-radius: 1.5px;
  transition: width 0.4s cubic-bezier(0.4, 0, 0.2, 1);
}
.obs-card__latency-fill.green { background: #3ddc84; }
.obs-card__latency-fill.yellow { background: var(--status-caution-text); }
.obs-card__latency-fill.red { background: var(--error); }

/* Latency value */
.obs-card__latency-value {
  font-size: 10px;
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
  font-size: 10px;
  color: var(--help-text);
  align-self: flex-end;
}

/* ===== RESPONSIVE ===== */
@media (max-width: 800px) {
  .metrics-bottom-grid {
    grid-template-columns: 1fr;
  }
  .sort-toggle__label {
    display: none;
  }
}

@media (max-width: 600px) {
  .metrics-page {
    padding: 8px;
  }
  .metrics-overview {
    grid-template-columns: 1fr;
  }
  .overview-card__chart {
    height: 32px;
  }
  .overview-card__value {
    font-size: var(--text-body);
  }
  .observatory-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .metrics-section__bar-chart {
    height: 200px;
  }
}

@media (max-width: 380px) {
  .observatory-grid {
    grid-template-columns: 1fr;
  }
  .overview-card__chart {
    height: 28px;
  }
}
</style>
