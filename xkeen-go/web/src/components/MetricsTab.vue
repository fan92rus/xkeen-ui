<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { getMetricsStats, getMetricsObservatory } from '../services/metrics.js';

// ---- Constants ----
const HISTORY_SIZE = 60;
const BAR_COLORS = [
  '#0097dc', '#7dce70', '#ffbb57', '#de3d3d',
  '#3d5073', '#25c478', '#6c7c85', '#005bb3'
];

// ---- Core state ----
const stats = ref(null);
const observatory = ref(null);
const available = ref(false);
const loading = ref(true);
const lastUpdate = ref(null);
let pollTimer = null;

// ---- Traffic history ring buffers ----
const historyDown = ref([]);
const historyUp = ref([]);
let prevDown = 0;
let prevUp = 0;

// ---- Sort state for outbound table ----
const sortKey = ref('downlink'); // 'downlink' | 'uplink' | 'total'

// ---- Animated counter refs ----
const displayDown = ref(0);
const displayUp = ref(0);
const animFrameDown = ref(null);
const animFrameUp = ref(null);

// ---- Card refresh pulse ----
const refreshingDown = ref(false);
const refreshingUp = ref(false);

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

function easeOutCubic(t) {
  return 1 - Math.pow(1 - t, 3);
}

// ---- Animated counter ----
function animateValue(from, to, setter, frameHolder) {
  if (frameHolder.value) cancelAnimationFrame(frameHolder.value);
  const delta = Math.abs(to - from);
  if (delta < to * 0.05 && to > 0) {
    setter(to);
    return;
  }
  const duration = 600;
  const start = performance.now();
  function tick(now) {
    const elapsed = now - start;
    const t = Math.min(elapsed / duration, 1);
    const val = from + (to - from) * easeOutCubic(t);
    setter(val);
    if (t < 1 && !document.hidden) {
      frameHolder.value = requestAnimationFrame(tick);
    } else {
      setter(to);
      frameHolder.value = null;
    }
  }
  frameHolder.value = requestAnimationFrame(tick);
}

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

// ---- Computed: outbound list ----
const outboundList = computed(() => {
  if (!stats.value?.outbound) return [];
  const entries = Object.entries(stats.value.outbound);
  return entries.sort((a, b) => {
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

// ---- Computed: outbound bar segments ----
const trafficBarSegments = computed(() => {
  const list = outboundList.value;
  if (!list.length) return [];
  const total = list.reduce((s, [, d]) => s + (d?.downlink || 0), 0);
  if (total === 0) return [];
  return list.map(([tag, d], i) => ({
    tag,
    width: ((d?.downlink || 0) / total * 100).toFixed(2),
    color: BAR_COLORS[i % BAR_COLORS.length],
    size: formatBytesShort(d?.downlink || 0)
  }));
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

// ---- Sparkline path builders ----
function buildSparklinePath(history) {
  const pts = history.value;
  if (pts.length < 2) return null;
  const W = 120, H = 40, PAD = 2;
  let maxVal = 0;
  for (let i = 0; i < pts.length; i++) if (pts[i] > maxVal) maxVal = pts[i];
  if (maxVal === 0) maxVal = 1;

  const xs = [], ys = [];
  for (let i = 0; i < pts.length; i++) {
    xs.push(i * (W / (pts.length - 1)));
    ys.push(H - PAD - (pts[i] / maxVal) * (H - PAD * 2));
  }

  // Build smooth bezier curve
  let line = `M${xs[0].toFixed(1)},${ys[0].toFixed(1)}`;
  for (let i = 1; i < pts.length; i++) {
    const cpx1 = xs[i - 1] + (xs[i] - xs[i - 1]) / 3;
    const cpx2 = xs[i] - (xs[i] - xs[i - 1]) / 3;
    line += ` C${cpx1.toFixed(1)},${ys[i - 1].toFixed(1)} ${cpx2.toFixed(1)},${ys[i].toFixed(1)} ${xs[i].toFixed(1)},${ys[i].toFixed(1)}`;
  }

  const area = line + ` L${xs[xs.length - 1].toFixed(1)},${H} L${xs[0].toFixed(1)},${H} Z`;
  return { line, area };
}

const sparkDown = computed(() => buildSparklinePath(historyDown));
const sparkUp = computed(() => buildSparklinePath(historyUp));

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
  const pct = ((d?.downlink || 0) / total * 100);
  return pct >= 10 ? pct.toFixed(1) + '%' : pct.toFixed(1) + '%';
}

// ---- Poll ----
async function poll() {
  try {
    const [s, o] = await Promise.all([getMetricsStats(), getMetricsObservatory()]);
    if (s?.available) {
      const oldDown = totalDown.value;
      const oldUp = totalUp.value;

      stats.value = s;
      observatory.value = o?.available ? o.results : null;
      available.value = true;
      lastUpdate.value = new Date();

      // Update history buffers
      const newDown = totalDown.value;
      const newUp = totalUp.value;
      const dDown = newDown - prevDown;
      const dUp = newUp - prevUp;
      historyDown.value = historyDown.value.concat([Math.max(0, dDown)]).slice(-HISTORY_SIZE);
      historyUp.value = historyUp.value.concat([Math.max(0, dUp)]).slice(-HISTORY_SIZE);
      prevDown = newDown;
      prevUp = newUp;

      // Animate counters
      animateValue(displayDown.value, newDown, v => { displayDown.value = v; }, animFrameDown);
      animateValue(displayUp.value, newUp, v => { displayUp.value = v; }, animFrameUp);

      // Refresh pulse
      refreshingDown.value = true;
      refreshingUp.value = true;
      setTimeout(() => { refreshingDown.value = false; refreshingUp.value = false; }, 300);
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
  pollTimer = setInterval(poll, 3000);
});
onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer);
  if (animFrameDown.value) cancelAnimationFrame(animFrameDown.value);
  if (animFrameUp.value) cancelAnimationFrame(animFrameUp.value);
});
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
      <p>&#x041C;&#x0435;&#x0442;&#x0440;&#x0438;&#x043A;&#x0438; &#x043D;&#x0435;&#x0434;&#x043E;&#x0441;&#x0442;&#x0443;&#x043F;&#x043D;&#x044B;</p>
      <span>&#x0412;&#x043A;&#x043B;&#x044E;&#x0447;&#x0438;&#x0442;&#x0435; <code>metrics_port</code> &#x0432; &#x043D;&#x0430;&#x0441;&#x0442;&#x0440;&#x043E;&#x0439;&#x043A;&#x0430;&#x0445; &#x0438; &#x043F;&#x0435;&#x0440;&#x0435;&#x0437;&#x0430;&#x043F;&#x0443;&#x0441;&#x0442;&#x0438;&#x0442;&#x0435; Xray</span>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="metrics-loading">
      <span></span><span></span><span></span>
    </div>

    <!-- Dashboard -->
    <div v-if="available" class="metrics-dashboard">

      <!-- Sticky header -->
      <div class="metrics-header">
        <h2>&#x041C;&#x0435;&#x0442;&#x0440;&#x0438;&#x043A;&#x0438; Xray</h2>
        <div class="metrics-pulse">
          <svg class="pulse-dot" width="12" height="12" viewBox="0 0 12 12">
            <circle cx="6" cy="6" r="3" fill="var(--indicator-online)"/>
          </svg>
        </div>
        <span v-if="lastUpdate" class="metrics-updated">{{ lastUpdate.toLocaleTimeString() }}</span>
      </div>

      <!-- ===== TRAFFIC OVERVIEW SPARKLINE CARDS ===== -->
      <section class="metrics-overview">
        <!-- Download sparkline -->
        <div :class="['sparkline-card', 'card-down', { refreshing: refreshingDown }]">
          <div class="sparkline-card-header">
            <span class="sparkline-label">Downlink</span>
            <span class="sparkline-value">{{ formatBytes(displayDown) }}</span>
          </div>
          <div class="sparkline-chart">
            <svg v-if="sparkDown" viewBox="0 0 120 40" preserveAspectRatio="none" width="100%" height="100%">
              <defs>
                <linearGradient id="areaGradDown" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stop-color="var(--indicator-online)" stop-opacity="0.25"/>
                  <stop offset="100%" stop-color="var(--indicator-online)" stop-opacity="0.02"/>
                </linearGradient>
              </defs>
              <path :d="sparkDown.area" fill="url(#areaGradDown)"/>
              <path :d="sparkDown.line" fill="none" stroke="var(--indicator-online)" stroke-opacity="0.7" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
          </div>
          <div :class="['sparkline-delta', deltaDown > 0 ? 'positive' : '']">
            {{ deltaDown > 0 ? '+' : '' }}{{ formatBytes(deltaDown) }}
          </div>
        </div>

        <!-- Upload sparkline -->
        <div :class="['sparkline-card', 'card-up', { refreshing: refreshingUp }]">
          <div class="sparkline-card-header">
            <span class="sparkline-label">Uplink</span>
            <span class="sparkline-value">{{ formatBytes(displayUp) }}</span>
          </div>
          <div class="sparkline-chart">
            <svg v-if="sparkUp" viewBox="0 0 120 40" preserveAspectRatio="none" width="100%" height="100%">
              <defs>
                <linearGradient id="areaGradUp" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stop-color="var(--primary-color)" stop-opacity="0.25"/>
                  <stop offset="100%" stop-color="var(--primary-color)" stop-opacity="0.02"/>
                </linearGradient>
              </defs>
              <path :d="sparkUp.area" fill="url(#areaGradUp)"/>
              <path :d="sparkUp.line" fill="none" stroke="var(--primary-color)" stroke-opacity="0.7" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
            </svg>
          </div>
          <div :class="['sparkline-delta', deltaUp > 0 ? 'positive' : '']">
            {{ deltaUp > 0 ? '+' : '' }}{{ formatBytes(deltaUp) }}
          </div>
        </div>

        <!-- Outbound count mini-stat -->
        <div class="mini-stat-card">
          <div class="mini-stat-icon">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 2L2 7l10 5 10-5-10-5z"/>
              <path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/>
            </svg>
          </div>
          <div class="mini-stat-info">
            <div class="mini-stat-value">{{ outboundList.length }}</div>
            <div class="mini-stat-label">Outbound</div>
          </div>
        </div>

        <!-- Proxy health mini-stat -->
        <div v-if="observatoryList.length" class="mini-stat-card">
          <div class="mini-stat-icon">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
              <circle cx="12" cy="12" r="3"/>
            </svg>
          </div>
          <div class="mini-stat-info">
            <div class="mini-stat-value">{{ healthRatio }}</div>
            <div class="mini-stat-label">&#x041F;&#x0440;&#x043E;&#x043A;&#x0441;&#x0438;</div>
          </div>
        </div>
      </section>

      <!-- ===== OUTBOUND TRAFFIC DISTRIBUTION ===== -->
      <section v-if="outboundList.length" class="metrics-outbound">
        <div class="section-header-row">
          <h3>&#x0422;&#x0440;&#x0430;&#x0444;&#x0438;&#x043A; &#x043F;&#x043E; outbound</h3>
          <button class="sort-toggle" @click="cycleSort" :title="'Sort: ' + sortLabel">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none"
                 stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M11 5h10"/><path d="M11 9h7"/><path d="M11 13h4"/>
              <path d="M3 17l3 3 3-3"/><path d="M6 18V4"/>
            </svg>
            <span class="sort-label">{{ sortLabel }}</span>
          </button>
        </div>

        <!-- Stacked bar overview -->
        <div v-if="trafficBarSegments.length" class="traffic-bars">
          <div v-for="(seg, i) in trafficBarSegments" :key="i"
               class="traffic-bar-segment"
               :style="{ width: seg.width + '%', background: seg.color }"
               :title="seg.tag + ': ' + seg.size">
          </div>
        </div>

        <!-- Table -->
        <div class="metrics-table-wrap">
          <table>
            <thead>
              <tr>
                <th>Tag</th>
                <th class="r">Share</th>
                <th class="dl-col-header">Downlink</th>
                <th class="r">Uplink</th>
                <th class="r">&#x0412;&#x0441;&#x0435;&#x0433;&#x043E;</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="[tag, d] in outboundList" :key="tag" class="outbound-row">
                <td class="tag">{{ tag }}</td>
                <td class="share-col">
                  <span class="share-badge">{{ outboundShare(d) }}</span>
                </td>
                <td class="dl-col">
                  <span class="dl-value">{{ formatBytes(d?.downlink || 0) }}</span>
                  <div class="progress-track">
                    <div class="progress-fill"
                         :style="{ width: ((d?.downlink || 0) / maxDown * 100) + '%' }"></div>
                  </div>
                </td>
                <td class="r">{{ formatBytes(d?.uplink || 0) }}</td>
                <td class="r">{{ formatBytes((d?.downlink || 0) + (d?.uplink || 0)) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- ===== OBSERVATORY HEALTH PANEL ===== -->
      <section v-if="observatoryList.length" class="metrics-observatory">
        <div class="section-header-row">
          <h3>Observatory</h3>
          <div class="health-summary">
            <span class="health-count alive">{{ aliveCount }} alive</span>
            <span v-if="deadCount" class="health-count dead">{{ deadCount }} dead</span>
          </div>
        </div>

        <!-- Card grid -->
        <div class="observatory-grid">
          <div v-for="[tag, d] in observatoryList" :key="tag"
               :class="['obs-card', d?.alive ? 'alive' : 'dead']">
            <div class="obs-header">
              <span :class="['obs-status-dot', d?.alive ? 'alive' : 'dead']"></span>
              <span class="obs-tag">{{ tag }}</span>
            </div>
            <div class="obs-latency-bar">
              <div v-if="d?.alive && d?.delay != null"
                   :class="['obs-latency-fill', delayColorClass(d.delay)]"
                   :style="{ width: Math.min(d.delay / 1000 * 100, 100) + '%' }"></div>
            </div>
            <div class="obs-latency-value">
              <span v-if="d?.delay != null && d?.alive" :class="['delay', delayColorClass(d.delay)]">{{ d.delay }} ms</span>
              <span v-else class="no-delay">&mdash;</span>
            </div>
            <div class="obs-last-check">{{ timeAgo(d?.last_try_time) }}</div>
          </div>
        </div>

        <!-- Detailed table -->
        <div class="observatory-table-wrap">
          <table>
            <thead>
              <tr>
                <th>&#x0422;&#x0435;&#x0433;</th>
                <th>&#x0421;&#x0442;&#x0430;&#x0442;&#x0443;&#x0441;</th>
                <th class="r">&#x0417;&#x0430;&#x0434;&#x0435;&#x0440;&#x0436;&#x043A;&#x0430;</th>
                <th class="r">&#x041F;&#x0440;&#x043E;&#x0432;&#x0435;&#x0440;&#x043A;&#x0430;</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="[tag, d] in observatoryList" :key="tag">
                <td class="tag">{{ tag }}</td>
                <td>
                  <span :class="['badge', d?.alive ? 'alive' : 'dead']">{{ d?.alive ? 'Alive' : 'Dead' }}</span>
                </td>
                <td class="r">
                  <span v-if="d?.delay != null" :class="['delay', delayColorClass(d.delay)]">{{ d.delay }} ms</span>
                  <span v-else>&mdash;</span>
                </td>
                <td class="r light">{{ timeAgo(d?.last_try_time) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <!-- Debug info -->
      <div v-if="stats?.debug" class="metrics-debug">{{ stats.debug }}</div>
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
  font-family: var(--font);
  color: var(--primary-text);
}

/* ===== Empty state ===== */
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

/* ===== Loading ===== */
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
  justify-content: space-between;
  height: 32px;
  flex-shrink: 0;
  margin-bottom: 0;
}
.metrics-header h2 {
  margin: 0;
  font-size: var(--text-h4);
  font-weight: 600;
  flex-shrink: 0;
}
.metrics-pulse {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  margin-left: 12px;
}
.pulse-dot circle {
  animation: livePulse 2s ease-in-out infinite;
}
@keyframes livePulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}
@media (prefers-reduced-motion: reduce) {
  .pulse-dot circle { animation: none; }
}
.metrics-updated {
  margin-left: auto;
  font-size: var(--text-small);
  color: var(--help-text);
  white-space: nowrap;
}

/* ===== Overview sparkline cards ===== */
.metrics-overview {
  display: grid;
  grid-template-columns: 1fr 1fr auto auto;
  gap: 12px;
}

/* Sparkline card */
.sparkline-card {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  padding: 14px 16px 10px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  position: relative;
  overflow: hidden;
  min-width: 0;
  transition: box-shadow 0.3s ease;
}
.sparkline-card.refreshing {
  animation: cardPulse 0.3s ease;
}
@keyframes cardPulse {
  0%   { box-shadow: var(--box-shadow-3); }
  50%  { box-shadow: 0 0 14px 0 rgba(0,151,220,0.12); }
  100% { box-shadow: var(--box-shadow-3); }
}

.sparkline-card-header {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
}
.sparkline-label {
  font-size: var(--text-small);
  color: var(--help-text);
  text-transform: uppercase;
  letter-spacing: .05em;
}
.sparkline-value {
  font-size: var(--text-h3);
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.card-down .sparkline-value { color: var(--indicator-online); }
.card-up .sparkline-value { color: var(--primary-color); }

.sparkline-chart {
  width: 100%;
  height: 40px;
  margin-top: 4px;
}
.sparkline-chart svg {
  display: block;
}

.sparkline-delta {
  font-size: var(--text-small);
  font-variant-numeric: tabular-nums;
  color: var(--help-text);
}
.sparkline-delta.positive {
  color: var(--indicator-online);
}

/* Mini stat cards */
.mini-stat-card {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  padding: 14px 16px;
  display: flex;
  align-items: center;
  gap: 12px;
}
.mini-stat-icon {
  width: 36px;
  height: 36px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--status-special-background);
  color: var(--primary-color);
  flex-shrink: 0;
}
.mini-stat-info {
  min-width: 0;
}
.mini-stat-value {
  font-size: var(--text-h4);
  font-weight: 700;
  line-height: 1.3;
}
.mini-stat-label {
  font-size: var(--text-small);
  color: var(--help-text);
  line-height: 1.3;
}

/* ===== Outbound section ===== */
.metrics-outbound {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  overflow: hidden;
}
.section-header-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px 8px;
}
.section-header-row h3 {
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
  transition: color 0.15s, border-color 0.15s;
}
.sort-toggle:hover {
  color: var(--primary-color);
  border-color: var(--primary-color);
}
.sort-toggle svg {
  opacity: 0.7;
}

/* Traffic distribution bar */
.traffic-bars {
  display: flex;
  height: 6px;
  border-radius: 3px;
  overflow: hidden;
  margin: 0 16px 12px;
  background: var(--progressbar-background);
}
.traffic-bar-segment {
  height: 100%;
  transition: width 0.6s cubic-bezier(0.4, 0, 0.2, 1);
  min-width: 1px;
}

/* Outbound table */
.metrics-table-wrap {
  overflow-x: auto;
}
.metrics-outbound table,
.metrics-observatory .observatory-table-wrap table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--text-body);
}
.metrics-outbound thead,
.metrics-observatory .observatory-table-wrap thead {
  background: var(--table-header);
}
.metrics-outbound th,
.metrics-observatory .observatory-table-wrap th {
  padding: 8px 16px;
  text-align: left;
  font-size: var(--text-small);
  text-transform: uppercase;
  letter-spacing: .04em;
  color: var(--help-text);
  font-weight: 600;
  border-bottom: 1px solid var(--table-border);
}
.metrics-outbound td,
.metrics-observatory .observatory-table-wrap td {
  padding: 8px 16px;
  border-bottom: 1px solid var(--table-border);
}
.metrics-outbound tbody tr:last-child td,
.metrics-observatory .observatory-table-wrap tbody tr:last-child td {
  border-bottom: none;
}
.metrics-outbound tbody tr:hover,
.metrics-observatory .observatory-table-wrap tbody tr:hover {
  background: var(--table-row-hover);
}

/* Shared table styles */
.tag {
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--primary-color);
  font-weight: 500;
}
.r {
  text-align: right;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.light { color: var(--help-text); }

/* Share badge */
.share-col { text-align: center; }
.share-badge {
  font-size: var(--text-small);
  color: var(--help-text);
  font-variant-numeric: tabular-nums;
}

/* Downlink column with progress bar */
.dl-col-header { min-width: 140px; }
.dl-col {
  position: relative;
  min-width: 100px;
}
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

/* ===== Observatory section ===== */
.metrics-observatory {
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  overflow: hidden;
}

/* Health summary badges */
.health-summary {
  display: flex;
  gap: 8px;
}
.health-count {
  padding: 2px 8px;
  border-radius: 10px;
  font-size: var(--text-small);
  font-weight: 600;
}
.health-count.alive {
  color: var(--status-success-text);
  background: var(--status-success-background);
}
.health-count.dead {
  color: var(--error);
  background: var(--status-warning-background);
}

/* Observatory card grid */
.observatory-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 8px;
  padding: 0 16px 16px;
}

/* Observatory card */
.obs-card {
  background: var(--bg-panel, var(--menu-background));
  border: 1px solid var(--menu-border);
  border-radius: var(--radius);
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  transition: border-left-color 0.3s ease, box-shadow 0.3s ease;
}
.obs-card.alive {
  border-left: 3px solid var(--indicator-online);
}
.obs-card.dead {
  border-left: 3px solid var(--error);
}

/* Obs card header */
.obs-header {
  display: flex;
  align-items: center;
  gap: 8px;
}
.obs-status-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}
.obs-status-dot.alive {
  background: var(--indicator-online);
  box-shadow: 0 0 6px var(--indicator-online);
}
.obs-status-dot.dead {
  background: var(--error);
}
.obs-tag {
  font-family: var(--font-mono);
  font-size: var(--text-small);
  color: var(--primary-color);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* Latency bar */
.obs-latency-bar {
  height: 4px;
  border-radius: 2px;
  background: var(--progressbar-background);
  overflow: hidden;
}
.obs-latency-fill {
  height: 100%;
  border-radius: 2px;
  transition: width 0.6s cubic-bezier(0.4, 0, 0.2, 1);
}
.obs-latency-fill.green { background: var(--indicator-online); }
.obs-latency-fill.yellow { background: var(--status-caution-text); }
.obs-latency-fill.red { background: var(--error); }

/* Latency value */
.obs-latency-value {
  font-size: var(--text-small);
  font-variant-numeric: tabular-nums;
  font-weight: 600;
  align-self: flex-end;
}
.no-delay {
  color: var(--help-text);
}
.obs-last-check {
  font-size: var(--text-small);
  color: var(--help-text);
  align-self: flex-end;
}

/* Observatory detail table */
.observatory-table-wrap {
  overflow-x: auto;
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

/* Delay colors */
.delay { font-variant-numeric: tabular-nums; }
.delay.green  { color: var(--indicator-online); }
.delay.yellow { color: var(--status-caution-text); }
.delay.red    { color: var(--error); }

/* Debug */
.metrics-debug {
  padding: 8px 12px;
  background: var(--menu-background);
  border: 1px solid var(--menu-border);
  border-radius: var(--radius-sm);
  font-size: var(--text-small);
  color: var(--help-text);
  font-family: var(--font-mono);
}

/* ===== RESPONSIVE ===== */

/* Tablet */
@media (max-width: 900px) {
  .metrics-overview {
    grid-template-columns: 1fr 1fr;
  }
  .mini-stat-card {
    /* make them span full row as a pair */
  }
  .sort-toggle .sort-label {
    display: none;
  }
  .observatory-grid {
    grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  }
}

/* Mobile */
@media (max-width: 600px) {
  .metrics-page {
    padding: 12px;
  }
  .metrics-overview {
    grid-template-columns: 1fr;
  }
  .sparkline-card {
    padding: 12px 14px 8px;
  }
  .sparkline-chart {
    height: 32px;
  }
  .sparkline-value {
    font-size: var(--text-h4);
  }
  .observatory-grid {
    grid-template-columns: repeat(2, 1fr);
  }
  .traffic-bars {
    margin: 0 12px 10px;
  }
  .share-col {
    display: none;
  }
}

/* Very small phones */
@media (max-width: 380px) {
  .observatory-grid {
    grid-template-columns: 1fr;
  }
  .sparkline-chart {
    height: 28px;
  }
  .sparkline-value {
    font-size: var(--text-small);
  }
  /* Hide uplink column in table on very small screens */
  .metrics-outbound table th:nth-child(4),
  .metrics-outbound table td:nth-child(4) {
    display: none;
  }
}
</style>
