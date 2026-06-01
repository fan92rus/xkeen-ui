<script>
import { h, computed } from 'vue';

const CHART_W = 280;
const CHART_H = 48;
const CHART_PAD = 2;

export const MiniChart = {
	props: {
		points: { type: Array, default: () => [] },
	},
	setup(props) {
		const path = computed(() => {
			if (props.points.length < 2) return null;

			const values = props.points.map(p => p.dl);
			const max = Math.max(...values, 1);

			const w = CHART_W - CHART_PAD * 2;
			const h = CHART_H - CHART_PAD * 2;

			const coords = values.map((v, i) => {
				const x = CHART_PAD + (i / (values.length - 1)) * w;
				const y = CHART_PAD + h - (v / max) * h;
				return [x, y];
			});

			let d = `M${coords[0][0]},${coords[0][1]}`;
			for (let i = 1; i < coords.length; i++) {
				d += ` L${coords[i][0]},${coords[i][1]}`;
			}

			const last = coords[coords.length - 1];
			const first = coords[0];
			const areaD = d + ` L${last[0]},${CHART_H - CHART_PAD} L${first[0]},${CHART_H - CHART_PAD} Z`;

			return { line: d, area: areaD };
		});

		return () => {
			if (props.points.length < 2) {
				return h('div', { class: 'chart-placeholder' }, '—');
			}
			const p = path.value;
			return h('svg', {
				viewBox: `0 0 ${CHART_W} ${CHART_H}`,
				class: 'mini-chart',
				preserveAspectRatio: 'none',
			}, [
				h('path', { d: p.area, class: 'chart-area', fill: 'currentColor' }),
				h('path', { d: p.line, class: 'chart-line', fill: 'none', stroke: 'currentColor', 'stroke-width': 1.5 }),
			]);
		};
	},
};
</script>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch, shallowReactive } from 'vue';
import { MetricsWS } from '../services/metrics.js';

const props = defineProps({ active: Boolean });

// ── State ──
const wsStatus = ref('disconnected');
const history = shallowReactive([]);
const latestSnap = ref(null);
const wsError = ref('');
const showInactive = ref(false);

let ws = null;

// ── Computed: traffic deltas (bytes/sec) ──
const series = computed(() => {
	if (history.length < 2) return { inbound: {}, outbound: {} };

	const inbound = {};
	const outbound = {};

	for (let i = 1; i < history.length; i++) {
		const prev = history[i - 1];
		const cur = history[i];
		const dt = cur.ts - prev.ts;
		if (dt <= 0) continue;

		if (cur.inbound && prev.inbound) {
			for (const tag of Object.keys(cur.inbound)) {
				if (!inbound[tag]) inbound[tag] = [];
				const pDL = prev.inbound[tag]?.downlink ?? 0;
				const pUL = prev.inbound[tag]?.uplink ?? 0;
				const cDL = cur.inbound[tag]?.downlink ?? 0;
				const cUL = cur.inbound[tag]?.uplink ?? 0;
				if (cDL >= pDL) {
					inbound[tag].push({ ts: cur.ts, dl: (cDL - pDL) / dt, ul: (cUL - pUL) / dt });
				}
			}
		}

		if (cur.outbound && prev.outbound) {
			for (const tag of Object.keys(cur.outbound)) {
				if (!outbound[tag]) outbound[tag] = [];
				const pDL = prev.outbound[tag]?.downlink ?? 0;
				const pUL = prev.outbound[tag]?.uplink ?? 0;
				const cDL = cur.outbound[tag]?.downlink ?? 0;
				const cUL = cur.outbound[tag]?.uplink ?? 0;
				if (cDL >= pDL) {
					outbound[tag].push({ ts: cur.ts, dl: (cDL - pDL) / dt, ul: (cUL - pUL) / dt });
				}
			}
		}
	}

	return { inbound, outbound };
});

// ── Computed: latest rates per tag ──
const latestRates = computed(() => {
	if (!latestSnap.value || history.length < 2) return null;
	const cur = latestSnap.value;
	const prev = history[history.length - 2];
	if (!prev) return null;
	const dt = cur.ts - prev.ts;
	if (dt <= 0) return null;

	const rates = { inbound: {}, outbound: {} };
	if (cur.inbound && prev.inbound) {
		for (const tag of Object.keys(cur.inbound)) {
			const dl = ((cur.inbound[tag]?.downlink ?? 0) - (prev.inbound[tag]?.downlink ?? 0)) / dt;
			const ul = ((cur.inbound[tag]?.uplink ?? 0) - (prev.inbound[tag]?.uplink ?? 0)) / dt;
			rates.inbound[tag] = { dl: Math.max(0, dl), ul: Math.max(0, ul) };
		}
	}
	if (cur.outbound && prev.outbound) {
		for (const tag of Object.keys(cur.outbound)) {
			const dl = ((cur.outbound[tag]?.downlink ?? 0) - (prev.outbound[tag]?.downlink ?? 0)) / dt;
			const ul = ((cur.outbound[tag]?.uplink ?? 0) - (prev.outbound[tag]?.uplink ?? 0)) / dt;
			rates.outbound[tag] = { dl: Math.max(0, dl), ul: Math.max(0, ul) };
		}
	}
	return rates;
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

// ── Computed: total rates ──
const totalRates = computed(() => {
	if (!latestRates.value) return { dl: 0, ul: 0 };
	let dl = 0, ul = 0;
	for (const r of Object.values(latestRates.value.outbound || {})) {
		dl += r.dl;
		ul += r.ul;
	}
	return { dl, ul };
});

// ── Helpers ──
function fmtRate(bytesPerSec) {
	if (!bytesPerSec || bytesPerSec <= 0) return '0 B/s';
	const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
	let i = 0, v = bytesPerSec;
	while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
	return v.toFixed(i === 0 ? 0 : 1) + ' ' + units[i];
}

function fmtDelay(ms) {
	if (!ms || ms <= 0) return '—';
	if (ms < 1000) return Math.round(ms) + ' ms';
	return (ms / 1000).toFixed(1) + ' s';
}

function fmtTime(ts) {
	return new Date(ts * 1000).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

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

watch(() => props.active, (v) => { v ? connect() : disconnect(); });
onMounted(() => { if (props.active) connect(); });
onUnmounted(disconnect);
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
			<div v-if="latestRates" class="metrics-total">
				<span class="total-dl">↓ {{ fmtRate(totalRates.dl) }}</span>
				<span class="total-ul">↑ {{ fmtRate(totalRates.ul) }}</span>
			</div>
			<div class="metrics-controls">
				<label class="toggle-label">
					<input type="checkbox" v-model="showInactive">
					Неактивные
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

		<!-- Charts + observatory -->
		<template v-else>
			<div class="metrics-charts">
				<div v-for="(tags, group) in series" :key="group" class="chart-group">
					<h3 class="chart-group-title">{{ group === 'inbound' ? 'Входящий трафик' : 'Исходящий трафик' }}</h3>
					<div v-for="(points, tag) in tags" :key="tag" class="chart-card">
						<div class="chart-card-header">
							<span class="chart-tag">{{ tag }}</span>
							<span v-if="latestRates?.[group]?.[tag]" class="chart-rate">
								↓ {{ fmtRate(latestRates[group][tag].dl) }}
								↑ {{ fmtRate(latestRates[group][tag].ul) }}
							</span>
							<span v-else class="chart-rate chart-rate-idle">нет данных</span>
						</div>
						<MiniChart :points="points" class="chart-svg" />
					</div>
					<div v-if="Object.keys(tags).length === 0" class="chart-empty">Нет данных</div>
				</div>
			</div>

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
							<td class="obs-tag">{{ entry.tag }}</td>
							<td><span class="obs-alive" :class="{ alive: entry.alive }">{{ entry.alive ? '✓' : '✗' }}</span></td>
							<td class="obs-delay">{{ fmtDelay(entry.delay) }}</td>
							<td class="obs-time">{{ entry.lastSeen ? fmtTime(entry.lastSeen) : '—' }}</td>
						</tr>
					</tbody>
				</table>
			</div>
		</template>
	</div>
</template>

<style scoped>
.metrics-wrapper {
	display: flex;
	flex-direction: column;
	gap: 16px;
	padding: 16px;
	height: 100%;
	overflow-y: auto;
}
.metrics-header {
	display: flex;
	align-items: center;
	gap: 16px;
	flex-wrap: wrap;
}
.metrics-status { display: flex; align-items: center; gap: 8px; }
.status-indicator {
	width: 8px; height: 8px; border-radius: 50%;
	background: var(--text-muted, #888);
}
.status-indicator.connected { background: #27ae60; }
.status-indicator.connecting { background: #f39c12; animation: pulse 1s infinite; }
.status-indicator.disconnected { background: #e74c3c; }
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
.status-text { font-size: 13px; color: var(--text-muted, #888); }
.status-error { font-size: 12px; color: #e74c3c; }

.metrics-total {
	display: flex; gap: 12px;
	font-size: 14px; font-weight: 600;
	font-variant-numeric: tabular-nums;
}
.total-dl { color: #3498db; }
.total-ul { color: #e67e22; }
.metrics-controls { margin-left: auto; }
.toggle-label {
	display: flex; align-items: center; gap: 6px;
	font-size: 12px; color: var(--text-muted, #888); cursor: pointer;
}

.metrics-unavailable {
	display: flex; flex-direction: column; align-items: center;
	justify-content: center; padding: 48px 16px;
	text-align: center; color: var(--text-muted, #888);
}
.unavail-icon { font-size: 48px; margin-bottom: 16px; }
.unavail-hint { font-size: 12px; margin-top: 8px; }

.metrics-charts {
	display: grid; grid-template-columns: 1fr 1fr; gap: 16px;
}
.chart-group { display: flex; flex-direction: column; gap: 8px; }
.chart-group-title {
	font-size: 13px; font-weight: 600; text-transform: uppercase;
	letter-spacing: 0.5px; color: var(--text-muted, #888); margin: 0;
}
.chart-card {
	background: var(--bg-secondary, #1e1e2e);
	border: 1px solid var(--border, #333);
	border-radius: 8px; padding: 10px 12px;
	display: flex; flex-direction: column; gap: 6px;
}
.chart-card-header { display: flex; align-items: center; justify-content: space-between; }
.chart-tag { font-size: 13px; font-weight: 500; color: var(--text, #ccc); }
.chart-rate { font-size: 11px; color: var(--text-muted, #888); font-variant-numeric: tabular-nums; }
.chart-rate-idle { font-style: italic; opacity: 0.5; }
.chart-empty { text-align: center; padding: 20px; color: var(--text-muted, #888); font-size: 13px; }

.chart-svg { width: 100%; }
.mini-chart { width: 100%; height: 48px; color: #3498db; }
.mini-chart .chart-area { opacity: 0.15; }
.mini-chart .chart-line { opacity: 0.8; }
.chart-placeholder {
	height: 48px; display: flex; align-items: center; justify-content: center;
	color: var(--text-muted, #888); font-size: 12px;
}

.observatory-section { margin-top: 8px; }
.section-title {
	font-size: 13px; font-weight: 600; text-transform: uppercase;
	letter-spacing: 0.5px; color: var(--text-muted, #888); margin: 0 0 8px;
}
.obs-table { width: 100%; border-collapse: collapse; font-size: 13px; }
.obs-table th {
	text-align: left; padding: 6px 10px; font-weight: 500;
	color: var(--text-muted, #888); border-bottom: 1px solid var(--border, #333);
}
.obs-table td { padding: 6px 10px; border-bottom: 1px solid var(--border, #333); }
.obs-dead { opacity: 0.5; }
.obs-alive { font-weight: 700; color: #e74c3c; }
.obs-alive.alive { color: #27ae60; }
.obs-tag { font-family: monospace; font-size: 12px; }
.obs-delay { font-variant-numeric: tabular-nums; }
.obs-time { color: var(--text-muted, #888); font-size: 12px; }

@media (max-width: 700px) {
	.metrics-charts { grid-template-columns: 1fr; }
}
</style>
