<script setup>
import { ref, computed, onMounted, onUnmounted, watch, shallowReactive, nextTick } from 'vue';
import { MetricsWS } from '../services/metrics.js';
import uPlot from 'uplot';

const props = defineProps({ active: Boolean });

// ── State ──
const wsStatus = ref('disconnected');
const history = shallowReactive([]);
const latestSnap = ref(null);
const wsError = ref('');
const showInactive = ref(false);
const inboundChartEl = ref(null);
const outboundChartEl = ref(null);

let ws = null;
let inboundPlot = null;
let outboundPlot = null;

// ── Colors ──
const DL_COLOR = '#3498db';
const UL_COLOR = '#e67e22';
const DL_FILL = 'rgba(52,152,219,0.15)';
const UL_FILL = 'rgba(230,126,34,0.15)';

// ── Computed: chart data (delta rates per tag, summed) ──
const chartData = computed(() => {
	if (history.length < 2) return null;

	const timestamps = [];
	const inboundDL = [];
	const inboundUL = [];
	const outboundDL = [];
	const outboundUL = [];

	for (let i = 1; i < history.length; i++) {
		const prev = history[i - 1];
		const cur = history[i];
		const dt = cur.ts - prev.ts;
		if (dt <= 0) continue;

		timestamps.push(cur.ts);

		// Sum all inbound tags
		let iDL = 0, iUL = 0;
		if (cur.inbound && prev.inbound) {
			for (const tag of Object.keys(cur.inbound)) {
				const cDL = cur.inbound[tag]?.downlink ?? 0;
				const pDL = prev.inbound[tag]?.downlink ?? 0;
				const cUL = cur.inbound[tag]?.uplink ?? 0;
				const pUL = prev.inbound[tag]?.uplink ?? 0;
				if (cDL >= pDL) {
					iDL += (cDL - pDL) / dt;
					iUL += (cUL - pUL) / dt;
				}
			}
		}
		inboundDL.push(iDL);
		inboundUL.push(iUL);

		// Sum all outbound tags
		let oDL = 0, oUL = 0;
		if (cur.outbound && prev.outbound) {
			for (const tag of Object.keys(cur.outbound)) {
				const cDL = cur.outbound[tag]?.downlink ?? 0;
				const pDL = prev.outbound[tag]?.downlink ?? 0;
				const cUL = cur.outbound[tag]?.uplink ?? 0;
				const pUL = prev.outbound[tag]?.uplink ?? 0;
				if (cDL >= pDL) {
					oDL += (cDL - pDL) / dt;
					oUL += (cUL - pUL) / dt;
				}
			}
		}
		outboundDL.push(oDL);
		outboundUL.push(oUL);
	}

	return {
		ts: timestamps,
		inbound: { dl: inboundDL, ul: inboundUL },
		outbound: { dl: outboundDL, ul: outboundUL },
	};
});

// ── Computed: per-tag breakdown for rates display ──
const tagRates = computed(() => {
	if (!latestSnap.value || history.length < 2) return { inbound: [], outbound: [] };
	const cur = latestSnap.value;
	const prev = history[history.length - 2];
	if (!prev) return { inbound: [], outbound: [] };
	const dt = cur.ts - prev.ts;
	if (dt <= 0) return { inbound: [], outbound: [] };

	const result = { inbound: [], outbound: [] };
	if (cur.inbound && prev.inbound) {
		for (const tag of Object.keys(cur.inbound)) {
			const dl = Math.max(0, ((cur.inbound[tag]?.downlink ?? 0) - (prev.inbound[tag]?.downlink ?? 0)) / dt);
			const ul = Math.max(0, ((cur.inbound[tag]?.uplink ?? 0) - (prev.inbound[tag]?.uplink ?? 0)) / dt);
			result.inbound.push({ tag, dl, ul });
		}
	}
	if (cur.outbound && prev.outbound) {
		for (const tag of Object.keys(cur.outbound)) {
			const dl = Math.max(0, ((cur.outbound[tag]?.downlink ?? 0) - (prev.outbound[tag]?.downlink ?? 0)) / dt);
			const ul = Math.max(0, ((cur.outbound[tag]?.uplink ?? 0) - (prev.outbound[tag]?.uplink ?? 0)) / dt);
			result.outbound.push({ tag, dl, ul });
		}
	}
	return result;
});

// ── Computed: total rates ──
const totalRates = computed(() => {
	const tags = tagRates.value;
	let dl = 0, ul = 0;
	for (const r of tags.outbound) { dl += r.dl; ul += r.ul; }
	return { dl, ul };
});

// ── Computed: observatory ──
const observatory = computed(() => {
	if (!latestSnap.value?.observatory) return [];
	const obs = latestSnap.value.observatory;
	return Object.entries(obs).map(([tag, data]) => ({
		tag,
		alive: data.alive ?? false,
		delay: data.delay ?? 0,
		lastSeen: data.last_seen_time ?? 0,
	})).sort((a, b) => a.tag.localeCompare(b.tag));
});

// ── Helpers ──
function fmtRate(bytesPerSec) {
	if (!bytesPerSec || bytesPerSec <= 0) return '0 B/s';
	const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
	let i = 0, v = bytesPerSec;
	while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
	return v.toFixed(i === 0 ? 0 : 1) + ' ' + units[i];
}

function fmtRateShort(bytesPerSec) {
	if (!bytesPerSec || bytesPerSec <= 0) return '0';
	const units = ['B', 'K', 'M', 'G'];
	let i = 0, v = bytesPerSec;
	while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
	return v.toFixed(i === 0 ? 0 : 1) + units[i];
}

function fmtDelay(ms) {
	if (!ms || ms <= 0) return '—';
	if (ms < 1000) return Math.round(ms) + ' ms';
	return (ms / 1000).toFixed(1) + ' s';
}

function fmtTime(ts) {
	return new Date(ts * 1000).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

// ── uPlot setup ──
const CHART_W = 400;
const CHART_H = 200;

function makeOpts(title) {
	return {
		title,
		width: CHART_W,
		height: CHART_H,
		cursor: {
			drag: { x: true, y: true },
			points: {
				size: 6,
				width: 1.5,
			},
		},
		legend: {
			show: true,
			live: true,
		},
		scales: {
			x: { time: true },
			y: {
				auto: true,
				orient: 'left',
			},
		},
		axes: [
			{
				stroke: '#4d545f',
				grid: { stroke: '#2e3d57', width: 1 },
				ticks: { stroke: '#4d545f', width: 1 },
				values: (u, vals) => vals.map(v => {
					const d = new Date(v * 1000);
					return d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
				}),
				font: '11px monospace',
				labelFont: '11px monospace',
				gap: 4,
			},
			{
				stroke: '#4d545f',
				grid: { stroke: '#2e3d57', width: 1 },
				ticks: { stroke: '#4d545f', width: 1 },
				values: (u, vals) => vals.map(v => fmtRateShort(v)),
				font: '11px monospace',
				labelFont: '11px monospace',
				gap: 4,
				size: 50,
			},
		],
		series: [
			{},
			{
				label: '↓ Download',
				stroke: DL_COLOR,
				fill: DL_FILL,
				width: 2,
				points: { show: true },
				value: (u, v) => v == null ? '—' : fmtRate(v),
			},
			{
				label: '↑ Upload',
				stroke: UL_COLOR,
				fill: UL_FILL,
				width: 2,
				points: { show: true },
				value: (u, v) => v == null ? '—' : fmtRate(v),
			},
		],
		hooks: {},
	};
}

function initPlots() {
	destroyPlots();
	if (inboundChartEl.value) {
		const emptyData = [new Float64Array(0), new Float64Array(0), new Float64Array(0)];
		inboundPlot = new uPlot(makeOpts('Входящий'), emptyData, inboundChartEl.value);
	}
	if (outboundChartEl.value) {
		const emptyData = [new Float64Array(0), new Float64Array(0), new Float64Array(0)];
		outboundPlot = new uPlot(makeOpts('Исходящий'), emptyData, outboundChartEl.value);
	}
}

function destroyPlots() {
	if (inboundPlot) { inboundPlot.destroy(); inboundPlot = null; }
	if (outboundPlot) { outboundPlot.destroy(); outboundPlot = null; }
}

function updatePlots() {
	const data = chartData.value;
	if (!data || !data.ts.length) return;

	const ts = Float64Array.from(data.ts);

	if (inboundPlot) {
		inboundPlot.setData([
			ts,
			Float64Array.from(data.inbound.dl),
			Float64Array.from(data.inbound.ul),
		], false);
		inboundPlot.redraw();
	}

	if (outboundPlot) {
		outboundPlot.setData([
			ts,
			Float64Array.from(data.outbound.dl),
			Float64Array.from(data.outbound.ul),
		], false);
		outboundPlot.redraw();
	}
}

// Update plots when history changes
watch(chartData, () => { nextTick(updatePlots); });

// ── WS lifecycle ──
function connect() {
	if (ws) return;
	wsStatus.value = 'connecting';

	ws = new MetricsWS({
		onData: (msg) => {
			if (msg.type === 'history') {
				history.splice(0, history.length, ...msg.history);
			} else if (msg.type === 'snapshot') {
				latestSnap.value = msg.snap;
				history.push(msg.snap);
				if (history.length > 300) history.splice(0, history.length - 300);
			}
		},
		onError: (err) => { wsError.value = String(err); },
		onStatus: (status) => {
			wsStatus.value = status;
			if (status === 'connected') wsError.value = '';
		},
	});

	const cached = ws.getCachedHistory();
	if (cached.length > 0) {
		history.splice(0, history.length, ...cached);
	}

	ws.connect();
}

function disconnect() {
	if (ws) { ws.disconnect(); ws = null; }
	wsStatus.value = 'disconnected';
}

watch(() => props.active, async (v) => {
	if (v) {
		connect();
		await nextTick();
		initPlots();
		updatePlots();
	} else {
		destroyPlots();
		disconnect();
	}
});

onMounted(async () => {
	if (props.active) {
		connect();
		await nextTick();
		initPlots();
		updatePlots();
	}
});
onUnmounted(() => { destroyPlots(); disconnect(); });
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
			<div class="metrics-controls">
				<label class="toggle-label">
					<input type="checkbox" v-model="showInactive">
					Показать неактивные
				</label>
			</div>
		</div>

		<!-- Unavailable -->
		<div v-if="latestSnap && !latestSnap.available" class="metrics-unavailable">
			<span class="unavail-icon">⚠</span>
			<p>Метрики Xray недоступны</p>
			<p class="unavail-hint">Убедитесь что Xray запущен и порт метрик настроен в Настройках</p>
		</div>

		<!-- No data -->
		<div v-else-if="!latestSnap && history.length === 0" class="metrics-unavailable">
			<span class="unavail-icon">📊</span>
			<p>Ожидание данных…</p>
			<p class="unavail-hint" v-if="wsStatus !== 'connected'">WebSocket не подключён</p>
		</div>

		<!-- Content -->
		<template v-else>
			<!-- Charts -->
			<div class="charts-row">
				<div class="chart-container">
					<div ref="inboundChartEl" class="chart-el"></div>
				</div>
				<div class="chart-container">
					<div ref="outboundChartEl" class="chart-el"></div>
				</div>
			</div>

			<!-- Per-tag rates -->
			<div class="rates-section">
				<div class="rates-column">
					<h3 class="rates-title">Входящий трафик</h3>
					<table class="rates-table">
						<thead>
							<tr>
								<th>Тег</th>
								<th>↓ Download</th>
								<th>↑ Upload</th>
							</tr>
						</thead>
						<tbody>
							<tr v-for="r in tagRates.inbound" :key="r.tag">
								<td class="tag-cell">{{ r.tag }}</td>
								<td class="rate-cell dl">{{ fmtRate(r.dl) }}</td>
								<td class="rate-cell ul">{{ fmtRate(r.ul) }}</td>
							</tr>
							<tr v-if="tagRates.inbound.length === 0">
								<td colspan="3" class="empty-cell">Нет данных</td>
							</tr>
						</tbody>
					</table>
				</div>
				<div class="rates-column">
					<h3 class="rates-title">Исходящий трафик</h3>
					<table class="rates-table">
						<thead>
							<tr>
								<th>Тег</th>
								<th>↓ Download</th>
								<th>↑ Upload</th>
							</tr>
						</thead>
						<tbody>
							<tr v-for="r in tagRates.outbound" :key="r.tag">
								<td class="tag-cell">{{ r.tag }}</td>
								<td class="rate-cell dl">{{ fmtRate(r.dl) }}</td>
								<td class="rate-cell ul">{{ fmtRate(r.ul) }}</td>
							</tr>
							<tr v-if="tagRates.outbound.length === 0">
								<td colspan="3" class="empty-cell">Нет данных</td>
							</tr>
						</tbody>
					</table>
				</div>
			</div>

			<!-- Observatory -->
			<div v-if="observatory.length > 0" class="observatory-section">
				<h3 class="section-title">Observatory</h3>
				<table class="obs-table">
					<thead>
						<tr>
							<th>Тег</th>
							<th>Статус</th>
							<th>Задержка</th>
							<th>Последняя проверка</th>
						</tr>
					</thead>
					<tbody>
						<tr v-for="entry in observatory" :key="entry.tag"
							v-show="showInactive || entry.alive"
							:class="{ 'obs-dead': !entry.alive }">
							<td class="tag-cell">{{ entry.tag }}</td>
							<td>
								<span class="obs-alive" :class="{ alive: entry.alive }">
									{{ entry.alive ? '✓' : '✗' }}
								</span>
							</td>
							<td class="rate-cell">{{ fmtDelay(entry.delay) }}</td>
							<td class="time-cell">{{ entry.lastSeen ? fmtTime(entry.lastSeen) : '—' }}</td>
						</tr>
					</tbody>
				</table>
			</div>
		</template>
	</div>
</template>

<style>
/* uPlot overrides — not scoped so they affect the library's injected styles */
.uplot {
	font-family: inherit !important;
}
.u-title {
	color: var(--primary-text) !important;
	font-size: 13px !important;
	font-weight: 600 !important;
	text-transform: uppercase;
	letter-spacing: 0.5px;
	margin-bottom: 4px !important;
}
.u-legend {
	display: flex !important;
	gap: 16px;
	margin-top: 4px;
}
.u-label {
	color: var(--text-gray) !important;
	font-size: 12px !important;
}
.u-val {
	color: var(--primary-text) !important;
	font-size: 12px !important;
	font-variant-numeric: tabular-nums;
}
.u-cursor-x,
.u-cursor-y {
	border-color: #4d545f !important;
}
.u-cursor-pt {
	border-width: 1.5px !important;
}
.u-select {
	background: rgba(0, 151, 220, 0.08) !important;
}
</style>

<style scoped>
.metrics-wrapper {
	display: flex;
	flex-direction: column;
	gap: 16px;
	padding: 16px;
	height: 100%;
	overflow-y: auto;
}

/* Header */
.metrics-header {
	display: flex;
	align-items: center;
	gap: 16px;
	flex-wrap: wrap;
}
.metrics-status { display: flex; align-items: center; gap: 8px; }
.status-indicator {
	width: 8px; height: 8px; border-radius: 50%;
	background: var(--indicator-offline);
}
.status-indicator.connected { background: var(--indicator-online); }
.status-indicator.connecting { background: #f39c12; animation: pulse 1s infinite; }
.status-indicator.disconnected { background: var(--error); }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
.status-text { font-size: 13px; color: var(--text-gray); }
.status-error { font-size: 12px; color: var(--error); }

.metrics-total {
	display: flex; gap: 14px;
	font-size: 15px; font-weight: 600;
	font-variant-numeric: tabular-nums;
}
.total-dl { color: #3498db; }
.total-ul { color: #e67e22; }
.metrics-controls { margin-left: auto; }
.toggle-label {
	display: flex; align-items: center; gap: 6px;
	font-size: 12px; color: var(--text-gray); cursor: pointer;
	user-select: none;
}

/* Unavailable */
.metrics-unavailable {
	display: flex; flex-direction: column; align-items: center;
	justify-content: center; padding: 48px 16px;
	text-align: center; color: var(--text-gray);
}
.unavail-icon { font-size: 48px; margin-bottom: 16px; }
.unavail-hint { font-size: 12px; margin-top: 8px; color: var(--help-text); }

/* Charts */
.charts-row {
	display: flex;
	gap: 16px;
	flex-wrap: wrap;
}
.chart-container {
	background: var(--menu-background);
	border: 1px solid var(--menu-border);
	border-radius: 8px;
	padding: 12px;
}
.chart-el {
	width: 400px;
	height: auto;
}

/* Rates section */
.rates-section {
	display: grid;
	grid-template-columns: 1fr 1fr;
	gap: 16px;
}
.rates-column {
	background: var(--menu-background);
	border: 1px solid var(--menu-border);
	border-radius: 8px;
	padding: 12px;
}
.rates-title {
	font-size: 12px; font-weight: 600; text-transform: uppercase;
	letter-spacing: 0.5px; color: var(--text-gray); margin: 0 0 8px;
}
.rates-table {
	width: 100%; border-collapse: collapse; font-size: 12px;
}
.rates-table th {
	text-align: left; padding: 4px 8px; font-weight: 500;
	color: var(--help-text); border-bottom: 1px solid var(--menu-border);
}
.rates-table td {
	padding: 4px 8px; border-bottom: 1px solid var(--menu-border);
}
.tag-cell { font-family: monospace; font-size: 11px; color: var(--primary-text); }
.rate-cell { font-variant-numeric: tabular-nums; color: var(--primary-text); }
.rate-cell.dl { color: #3498db; }
.rate-cell.ul { color: #e67e22; }
.empty-cell { text-align: center; color: var(--help-text); font-style: italic; padding: 12px 8px !important; }
.time-cell { color: var(--text-gray); font-size: 11px; }

/* Observatory */
.observatory-section {
	background: var(--menu-background);
	border: 1px solid var(--menu-border);
	border-radius: 8px;
	padding: 12px;
}
.section-title {
	font-size: 12px; font-weight: 600; text-transform: uppercase;
	letter-spacing: 0.5px; color: var(--text-gray); margin: 0 0 8px;
}
.obs-table {
	width: 100%; border-collapse: collapse; font-size: 12px;
}
.obs-table th {
	text-align: left; padding: 4px 8px; font-weight: 500;
	color: var(--help-text); border-bottom: 1px solid var(--menu-border);
}
.obs-table td {
	padding: 4px 8px; border-bottom: 1px solid var(--menu-border);
}
.obs-dead { opacity: 0.45; }
.obs-alive { font-weight: 700; color: var(--error); }
.obs-alive.alive { color: var(--indicator-online); }

@media (max-width: 900px) {
	.charts-row { flex-direction: column; }
	.rates-section { grid-template-columns: 1fr; }
	.chart-el { width: 100%; }
	.chart-container { width: 100%; }
}
</style>
