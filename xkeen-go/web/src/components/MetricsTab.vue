<script setup>
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue';
import { getProxyNames } from '../services/metrics.js';
import { computeTagRates, computeChartData, totalOutboundRates } from '../utils/metrics-rates.js';
import { fmtBytes, fmtRate, fmtRateShort, fmtDelay, fmtTime, fmtTimeShort, fmtDuration, percentile } from '../utils/metrics-format.js';
import { useMetricsWS } from '../composables/useMetricsWS.js';
import { useMetricsChart } from '../composables/useMetricsChart.js';

const props = defineProps({ active: Boolean });

// ── WS composable ──
const { wsStatus, wsError, history, latestSnap, connect, disconnect } = useMetricsWS();

// ── Chart composable ──
const { chartCanvas, CHART_H, initCharts, destroyCharts, updateCharts } = useMetricsChart();

// ── Local state ──
const showInactive = ref(false);
const proxyNames = ref({}); // tag → remarks

// ── Computed: chart data (outbound totals = real proxy traffic) ──
const chartData = computed(() => computeChartData(history));

// ── Computed: per-tag rates ──
const tagRates = computed(() => {
	if (!latestSnap.value || history.length < 2) return { inbound: [], outbound: [] };
	const prev = history[history.length - 2];
	return computeTagRates(latestSnap.value, prev);
});

const totalRates = computed(() => totalOutboundRates(tagRates.value));

const observatory = computed(() => {
	if (!latestSnap.value?.observatory) return [];
	return Object.entries(latestSnap.value.observatory).map(([tag, data]) => ({
		tag, alive: data.alive ?? false, delay: data.delay ?? 0, lastSeen: data.last_seen_time ?? 0,
	})).sort((a, b) => a.tag.localeCompare(b.tag));
});

// ── Computed: session uptime ──
const sessionUptime = computed(() => {
	if (history.length < 2) return '';
	const first = history[0].ts;
	const last = history[history.length - 1].ts;
	return fmtDuration(last - first);
});

// ── Computed: speed stats (peak, p95, p50) ──
const speedStats = computed(() => {
	const data = chartData.value;
	if (!data || data.dl.length === 0) return null;
	return {
		peak: { dl: Math.max(...data.dl), ul: Math.max(...data.ul) },
		p95: { dl: percentile(data.dl, 95), ul: percentile(data.ul, 95) },
		p50: { dl: percentile(data.dl, 50), ul: percentile(data.ul, 50) },
	};
});

// ── Computed: cumulative volumes ──
const PROXY_COLORS = ['#3498db', '#2ecc71', '#9b59b6', '#e74c3c', '#f1c40f', '#1abc9c', '#e67e22'];
const tagVolumes = computed(() => {
	if (history.length < 2) return { inbound: [], outbound: [] };
	const first = history[0], last = history[history.length - 1];
	const result = { inbound: [], outbound: [] };
	for (const dir of ['inbound', 'outbound']) {
		if (!first[dir] || !last[dir]) continue;
		for (const tag of Object.keys(last[dir])) {
			const curDL = last[dir][tag]?.downlink ?? 0;
			const prevDL = first[dir][tag]?.downlink ?? 0;
			const curUL = last[dir][tag]?.uplink ?? 0;
			const prevUL = first[dir][tag]?.uplink ?? 0;
			result[dir].push({ tag, dl: Math.max(0, curDL - prevDL), ul: Math.max(0, curUL - prevUL) });
		}
	}
	return result;
});
const proxyShare = computed(() => {
	const entries = tagVolumes.value.outbound;
	if (!entries.length) return [];
	const totalDL = entries.reduce((s, r) => s + r.dl, 0);
	if (totalDL === 0) return entries.map((r, i) => ({ ...r, pct: 0, color: PROXY_COLORS[i % PROXY_COLORS.length] }));
	return entries.map((r, i) => ({
		...r,
		pct: (r.dl / totalDL) * 100,
		color: PROXY_COLORS[i % PROXY_COLORS.length],
	}));
});

// ── Computed: observatory timeline ──
const obsTimeline = computed(() => {
	if (history.length < 2) return { tags: [], range: { start: 0, end: 0 } };
	const first = history[0].ts;
	const last = history[history.length - 1].ts;
	const range = last - first;
	if (range <= 0) return { tags: [], range: { start: first, end: last } };
	const tagMap = new Map();
	for (const snap of history) {
		if (!snap.observatory) continue;
		for (const [tag, data] of Object.entries(snap.observatory)) {
			if (!tagMap.has(tag)) tagMap.set(tag, []);
			tagMap.get(tag).push({ ts: snap.ts, alive: data.alive ?? false });
		}
	}
	const tags = [];
	for (const [tag, points] of tagMap) {
		const segments = [];
		let segStart = points[0]?.ts ?? first;
		let segAlive = points[0]?.alive ?? false;
		for (let i = 1; i < points.length; i++) {
			if (points[i].alive !== segAlive) {
				segments.push({ start: segStart, end: points[i].ts, alive: segAlive });
				segStart = points[i].ts;
				segAlive = points[i].alive;
			}
		}
		segments.push({ start: segStart, end: last, alive: segAlive });
		tags.push({
			tag,
			segments: segments.map(s => ({
				left: ((s.start - first) / range) * 100,
				width: ((s.end - s.start) / range) * 100,
				alive: s.alive,
			})),
		});
	}
	tags.sort((a, b) => a.tag.localeCompare(b.tag));
	return { tags, range: { start: first, end: last } };
});

// ── Helpers ──
function displayName(tag) {
	const name = proxyNames.value[tag];
	return name ? tag + ' · ' + name : tag;
}

// ── Chart update watcher ──
watch(chartData, () => { nextTick(() => updateCharts(chartData.value)); });

// ── Lifecycle ──
watch(() => props.active, async (v) => {
	if (v) { connect(); await nextTick(); initCharts(); updateCharts(chartData.value); }
	else { destroyCharts(); disconnect(); }
});
onMounted(async () => {
	if (props.active) {
		connect();
		await nextTick();
		initCharts();
		updateCharts(chartData.value);
	}
	try {
		const data = await getProxyNames();
		if (data && typeof data === 'object') proxyNames.value = data;
	} catch { /* non-critical */ }
});
onUnmounted(() => { destroyCharts(); disconnect(); });
</script>

<template>
	<div class="metrics-wrapper">
		<!-- Header -->
		<div class="metrics-header">
			<div class="metrics-status">
				<span class="status-indicator" :class="wsStatus"></span>
				<span class="status-text">
					{{ wsStatus === 'connected' ? 'Подключено' : wsStatus === 'connecting' ? 'Подключение…' : 'Отключено' }}
				</span>
				<span v-if="wsError" class="status-error">{{ wsError }}</span>
			</div>
			<div v-if="latestSnap" class="metrics-total">
				<span class="total-dl">↓ {{ fmtRate(totalRates.dl) }}</span>
				<span class="total-ul">↑ {{ fmtRate(totalRates.ul) }}</span>
			</div>
			<span v-if="sessionUptime" class="session-uptime">⏱ {{ sessionUptime }}</span>
			<div class="metrics-controls">
				<label class="toggle-label">
					<input type="checkbox" v-model="showInactive"> Неактивные
				</label>
			</div>
		</div>

		<!-- Unavailable -->
		<div v-if="latestSnap && !latestSnap.available" class="metrics-unavailable">
			<span class="unavail-icon">⚠</span>
			<p>Метрики Xray недоступны</p>
			<p class="unavail-hint">Убедитесь что Xray запущен и порт метрик настроен в Настройках</p>
		</div>
		<div v-else-if="!latestSnap && history.length === 0" class="metrics-unavailable">
			<span class="unavail-icon">📊</span>
			<p>Ожидание данных…</p>
			<p class="unavail-hint" v-if="wsStatus !== 'connected'">WebSocket не подключён</p>
		</div>

		<!-- Content -->
		<template v-else>
			<!-- Chart -->
			<div class="chart-container">
				<canvas ref="chartCanvas" :height="CHART_H"></canvas>
			</div>

			<!-- Speed stats -->
			<div v-if="speedStats" class="stats-row">
				<div class="stat-card">
					<div class="stat-label">Пик</div>
					<div class="stat-values">
						<span class="stat-dl">↓ {{ fmtRate(speedStats.peak.dl) }}</span>
						<span class="stat-ul">↑ {{ fmtRate(speedStats.peak.ul) }}</span>
					</div>
				</div>
				<div class="stat-card">
					<div class="stat-label">P95</div>
					<div class="stat-values">
						<span class="stat-dl">↓ {{ fmtRate(speedStats.p95.dl) }}</span>
						<span class="stat-ul">↑ {{ fmtRate(speedStats.p95.ul) }}</span>
					</div>
				</div>
				<div class="stat-card">
					<div class="stat-label">P50</div>
					<div class="stat-values">
						<span class="stat-dl">↓ {{ fmtRate(speedStats.p50.dl) }}</span>
						<span class="stat-ul">↑ {{ fmtRate(speedStats.p50.ul) }}</span>
					</div>
				</div>
			</div>

			<!-- Rates + Observatory side by side -->
			<div class="bottom-section">
				<!-- Rates tables -->
				<div class="rates-column">
					<h3 class="rates-title">Входящий</h3>
					<table class="rates-table">
						<thead><tr><th>Тег</th><th>↓ DL</th><th>↑ UL</th><th class="vol-sep">↓ DL</th><th>↑ UL</th></tr></thead>
						<tbody>
							<tr v-for="(r, i) in tagRates.inbound" :key="r.tag">
								<td class="tag-cell">{{ displayName(r.tag) }}</td>
								<td class="rate-cell dl">{{ fmtRate(r.dl) }}</td>
								<td class="rate-cell ul">{{ fmtRate(r.ul) }}</td>
								<td class="vol-cell vol-sep">{{ fmtBytes(tagVolumes.inbound[i]?.dl ?? 0) }}</td>
								<td class="vol-cell">{{ fmtBytes(tagVolumes.inbound[i]?.ul ?? 0) }}</td>
							</tr>
							<tr v-if="!tagRates.inbound.length"><td colspan="5" class="empty-cell">—</td></tr>
						</tbody>
					</table>
					<h3 class="rates-title" style="margin-top:10px">Исходящий</h3>
					<table class="rates-table">
						<thead><tr><th>Тег</th><th>↓ DL</th><th>↑ UL</th><th class="vol-sep">↓ DL</th><th>↑ UL</th></tr></thead>
						<tbody>
							<tr v-for="(r, i) in tagRates.outbound" :key="r.tag">
								<td class="tag-cell">{{ displayName(r.tag) }}</td>
								<td class="rate-cell dl">{{ fmtRate(r.dl) }}</td>
								<td class="rate-cell ul">{{ fmtRate(r.ul) }}</td>
								<td class="vol-cell vol-sep">{{ fmtBytes(tagVolumes.outbound[i]?.dl ?? 0) }}</td>
								<td class="vol-cell">{{ fmtBytes(tagVolumes.outbound[i]?.ul ?? 0) }}</td>
							</tr>
							<tr v-if="!tagRates.outbound.length"><td colspan="5" class="empty-cell">—</td></tr>
						</tbody>
					</table>
					<h3 class="rates-title" style="margin-top:10px">Доля трафика</h3>
					<div class="share-bars">
						<div v-for="s in proxyShare" :key="s.tag" class="share-row">
							<span class="share-tag">{{ displayName(s.tag) }}</span>
							<div class="share-track">
								<div class="share-fill" :style="{ width: s.pct + '%', background: s.color }"></div>
							</div>
							<span class="share-pct">{{ s.pct.toFixed(1) }}%</span>
						</div>
					</div>
				</div>
				<!-- Observatory -->
				<div v-if="observatory.length > 0" class="obs-section">
					<h3 class="section-title">Observatory</h3>
					<table class="obs-table">
						<thead><tr><th>Тег</th><th>Статус</th><th>Задержка</th><th>Проверка</th></tr></thead>
						<tbody>
							<tr v-for="e in observatory" :key="e.tag"
								v-show="showInactive || e.alive"
								:class="{ 'obs-dead': !e.alive }">
								<td class="tag-cell">{{ displayName(e.tag) }}</td>
								<td><span class="obs-alive" :class="{ alive: e.alive }">{{ e.alive ? '✓' : '✗' }}</span></td>
								<td class="rate-cell">{{ fmtDelay(e.delay) }}</td>
								<td class="time-cell">{{ e.lastSeen ? fmtTime(e.lastSeen) : '—' }}</td>
							</tr>
						</tbody>
					</table>
					<template v-if="obsTimeline.tags.length > 0">
						<h3 class="section-title" style="margin-top:12px">Timeline</h3>
						<div class="obs-timeline-header">
							<span>{{ fmtTimeShort(obsTimeline.range.start) }}</span>
							<span>{{ fmtTimeShort(obsTimeline.range.end) }}</span>
						</div>
						<div class="obs-timeline">
							<div v-for="t in obsTimeline.tags" :key="t.tag" class="timeline-row">
								<span class="timeline-tag">{{ t.tag }}</span>
								<div class="timeline-track">
									<div v-for="(seg, i) in t.segments" :key="i"
										class="timeline-seg"
										:class="{ alive: seg.alive, dead: !seg.alive }"
										:style="{ left: seg.left + '%', width: Math.max(seg.width, 0.5) + '%' }">
									</div>
								</div>
							</div>
						</div>
					</template>
				</div>
			</div>
		</template>
	</div>
</template>

<style scoped>
.metrics-wrapper {
	display: flex; flex-direction: column; gap: 14px;
	padding: 14px; height: 100%; overflow-y: auto;
}

/* Header */
.metrics-header { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
.metrics-status { display: flex; align-items: center; gap: 8px; }
.status-indicator { width: 8px; height: 8px; border-radius: 50%; background: var(--indicator-offline); }
.status-indicator.connected { background: var(--indicator-online); }
.status-indicator.connecting { background: #f39c12; animation: pulse 1s infinite; }
.status-indicator.disconnected { background: var(--error); }
@keyframes pulse { 0%,100% { opacity:1 } 50% { opacity:.4 } }
.status-text { font-size: 13px; color: var(--text-gray); }
.status-error { font-size: 12px; color: var(--error); }

.metrics-total { display: flex; gap: 14px; font-size: 15px; font-weight: 600; font-variant-numeric: tabular-nums; }
.total-dl { color: #3498db; min-width: 120px; }
.total-ul { color: #e67e22; min-width: 120px; }
.metrics-controls { margin-left: auto; }
.toggle-label { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-gray); cursor: pointer; user-select: none; }

/* Unavailable */
.metrics-unavailable { display: flex; flex-direction: column; align-items: center; justify-content: center; padding: 48px 16px; text-align: center; color: var(--text-gray); }
.unavail-icon { font-size: 48px; margin-bottom: 16px; }
.unavail-hint { font-size: 12px; margin-top: 8px; color: var(--help-text); }

/* Chart */
.chart-container {
	background: var(--menu-background);
	border: 1px solid var(--menu-border);
	border-radius: 8px;
	padding: 10px 12px 2px;
	position: relative;
	height: 280px;
}

/* Bottom: rates + observatory */
.bottom-section { display: grid; grid-template-columns: 660px 1fr; gap: 14px; }
.rates-column { background: var(--menu-background); border: 1px solid var(--menu-border); border-radius: 8px; padding: 10px 12px; min-width: 0; }
.rates-title { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .5px; color: var(--text-gray); margin: 0 0 5px; }
.obs-section { background: var(--menu-background); border: 1px solid var(--menu-border); border-radius: 8px; padding: 10px 12px; overflow-x: auto; }
.section-title { font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .5px; color: var(--text-gray); margin: 0 0 5px; }

/* Tables */
.rates-table, .obs-table { width: 100%; border-collapse: collapse; font-size: 12px; }
.rates-table th, .obs-table th { text-align: left; padding: 3px 8px; font-weight: 500; color: var(--help-text); border-bottom: 1px solid var(--menu-border); }
.rates-table th:nth-child(1), .rates-table td:nth-child(1) { width: 280px; }
.rates-table th:nth-child(2), .rates-table td:nth-child(2) { width: 98px; }
.rates-table th:nth-child(3), .rates-table td:nth-child(3) { width: 98px; }
.rates-table th:nth-child(4), .rates-table td:nth-child(4) { width: 72px; }
.rates-table th:nth-child(5), .rates-table td:nth-child(5) { width: 72px; }
.rates-table td, .obs-table td { padding: 3px 8px; border-bottom: 1px solid var(--menu-border); }
.tag-cell { font-family: monospace; font-size: 11px; color: var(--primary-text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 280px; }
.rate-cell { font-variant-numeric: tabular-nums; color: var(--primary-text); }
.rate-cell.dl { color: #3498db; }
.rate-cell.ul { color: #e67e22; }
.vol-sep { border-left: 1px solid var(--menu-border); }
.vol-cell { font-variant-numeric: tabular-nums; color: var(--text-gray); font-size: 11px; }
.empty-cell { text-align: center; color: var(--help-text); font-style: italic; padding: 8px !important; }
.time-cell { color: var(--text-gray); font-size: 11px; }
.obs-dead { opacity: .4; }
.obs-alive { font-weight: 700; color: var(--error); }
.obs-alive.alive { color: var(--indicator-online); }


/* Session uptime */
.session-uptime {
	font-size: 13px;
	color: var(--text-gray);
	font-variant-numeric: tabular-nums;
}

/* Speed stats row */
.stats-row {
	display: flex;
	gap: 12px;
}
.stat-card {
	flex: 1;
	background: var(--menu-background);
	border: 1px solid var(--menu-border);
	border-radius: 8px;
	padding: 8px 12px;
	display: flex;
	flex-direction: column;
	gap: 4px;
}
.stat-label {
	font-size: 11px;
	font-weight: 600;
	text-transform: uppercase;
	letter-spacing: .5px;
	color: var(--help-text);
}
.stat-values {
	display: flex;
	gap: 12px;
	font-size: 13px;
	font-weight: 600;
	font-variant-numeric: tabular-nums;
}
.stat-dl { color: #3498db; min-width: 100px; }
.stat-ul { color: #e67e22; min-width: 100px; }

/* Proxy share bars */
.share-bars {
	display: flex;
	flex-direction: column;
	gap: 4px;
}
.share-row {
	display: flex;
	align-items: center;
	gap: 8px;
}
.share-tag {
	font-family: monospace;
	font-size: 10px;
	color: var(--primary-text);
	width: 200px;
	flex-shrink: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.share-track {
	flex: 1;
	height: 8px;
	background: var(--menu-border);
	border-radius: 4px;
	overflow: hidden;
	min-width: 40px;
}
.share-fill {
	height: 100%;
	border-radius: 4px;
	transition: width 0.3s ease;
	min-width: 2px;
}
.share-pct {
	font-size: 11px;
	font-variant-numeric: tabular-nums;
	color: var(--text-gray);
	width: 44px;
	text-align: right;
	flex-shrink: 0;
}

/* Observatory timeline */
.obs-timeline-header {
	display: flex;
	justify-content: space-between;
	font-size: 10px;
	color: var(--help-text);
	margin-bottom: 4px;
	padding: 0 2px;
}
.obs-timeline {
	display: flex;
	flex-direction: column;
	gap: 4px;
}
.timeline-row {
	display: flex;
	align-items: center;
	gap: 8px;
}
.timeline-tag {
	font-family: monospace;
	font-size: 10px;
	color: var(--primary-text);
	width: 80px;
	flex-shrink: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.timeline-track {
	flex: 1;
	height: 10px;
	background: #1a2332;
	border-radius: 5px;
	position: relative;
	overflow: hidden;
	min-width: 60px;
}
.timeline-seg {
	position: absolute;
	top: 0;
	height: 100%;
	border-radius: 2px;
}
.timeline-seg.alive { background: #2ecc71; }
.timeline-seg.dead { background: #e74c3c; }

@media (max-width: 800px) {
	.bottom-section { grid-template-columns: 1fr; }
}
</style>
