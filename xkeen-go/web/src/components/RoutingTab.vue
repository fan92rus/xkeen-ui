<template>
	<div class="routing-container">
		<!-- Header -->
		<div class="rt-header">
			<div class="rt-header-left">
				<label class="rt-strategy">
					<span class="rt-strategy-label">DNS:</span>
					<select v-model="localRouting.domainStrategy" class="rt-select" @change="markDirty">
						<option value="AsIs">AsIs</option>
						<option value="IPIfNonMatch">IPIfNonMatch</option>
						<option value="IPOnDemand">IPOnDemand</option>
					</select>
				</label>
				<span class="rt-rule-count">{{ rules.length }} {{ pluralize(rules.length) }}</span>
			</div>
			<div class="rt-header-right">
				<button class="btn btn-sm" @click="showTemplates = !showTemplates">📦 Шаблоны</button>
				<button class="btn btn-sm btn-primary" @click="addRule">+ Правило</button>
				<button v-if="dirty" class="btn btn-sm btn-success" @click="save">💾 Сохранить</button>
			</div>
		</div>

		<!-- Info banner -->
		<div class="rt-info">
			💡 Правила применяются <strong>сверху вниз</strong>. Первое совпадение выигрывает. Перетаскивайте за <span class="drag-hint">⋮⋮</span> для смены приоритета.
		</div>

		<!-- Templates panel -->
		<div v-if="showTemplates" class="rt-templates">
			<div class="rt-template" @click="applyTemplate('ru-direct')">
				<span class="rt-tpl-icon">🇷🇺</span>
				<span class="rt-tpl-name">RU Direct</span>
				<span class="rt-tpl-desc">Российские домены → напрямую</span>
			</div>
			<div class="rt-template" @click="applyTemplate('block-ads')">
				<span class="rt-tpl-icon">🚫</span>
				<span class="rt-tpl-name">Block Ads</span>
				<span class="rt-tpl-desc">Реклама → блок</span>
			</div>
			<div class="rt-template" @click="applyTemplate('streaming')">
				<span class="rt-tpl-icon">📺</span>
				<span class="rt-tpl-name">Streaming</span>
				<span class="rt-tpl-desc">Netflix, YouTube → прокси</span>
			</div>
		</div>

		<!-- Rule cards -->
		<div class="rt-rules">
			<div
				v-for="(rule, idx) in rules"
				:key="rule.id"
				class="rt-card"
				:class="{
					dragging: dragIdx === idx,
					'drag-over': dragOverIdx === idx,
					expanded: expandedId === rule.id,
				}"
				:draggable="expandedId !== rule.id"
				@dragstart="onDragStart($event, idx)"
				@dragover.prevent="onDragOver(idx)"
				@dragleave="onDragLeave"
				@drop.prevent="onDrop(idx)"
				@dragend="onDragEnd"
			>
				<!-- Card header (collapsed view) -->
				<div class="rt-card-header" @click="toggleExpand(rule.id)">
					<span class="rt-drag-handle" @click.stop title="Перетащите для смены приоритета">⋮⋮</span>
					<span class="rt-card-num">{{ idx + 1 }}</span>
					<span class="rt-card-icon">{{ ruleIcon(rule) }}</span>
					<span class="rt-card-name">{{ rule.name }}</span>
					<span class="rt-card-summary" v-if="!expandedId">
						<span v-if="rule.domains.length" class="rt-badge rt-badge-domain">D:{{ rule.domains.length }}</span>
						<span v-if="rule.ips.length" class="rt-badge rt-badge-ip">IP:{{ rule.ips.length }}</span>
						<span v-if="rule.port" class="rt-badge rt-badge-port">:{{ rule.port }}</span>
						<span v-if="rule.networks.length && rule.networks.length < 2" class="rt-badge">{{ rule.networks.join(',') }}</span>
					</span>
					<span class="rt-card-action" :class="actionClass(rule.action)">
						{{ actionLabel(rule.action) }}
					</span>
					<span class="rt-card-actions" @click.stop>
						<button class="rt-icon-btn" @click="toggleExpand(rule.id)" :title="expandedId === rule.id ? 'Свернуть' : 'Изменить'">{{ expandedId === rule.id ? '▲' : '✏️' }}</button>
						<button class="rt-icon-btn rt-icon-danger" @click="deleteRule(idx)" title="Удалить">🗑️</button>
					</span>
				</div>

				<!-- Expanded editor -->
				<div v-if="expandedId === rule.id" class="rt-card-body">
					<!-- Name -->
					<div class="rt-field">
						<label class="rt-field-label">Название</label>
						<input v-model="rule.name" class="rt-input" placeholder="Например: RU Direct" @input="markDirty">
					</div>

					<!-- Domains -->
					<div class="rt-field">
						<label class="rt-field-label">Домены и сайты</label>
						<div class="rt-tag-list">
							<span v-for="(d, di) in rule.domains" :key="di" class="rt-tag" :class="'rt-tag-' + d.type">
								<span class="rt-tag-icon">{{ entryIcon(d) }}</span>
								<span class="rt-tag-text">{{ entryLabel(d) }}</span>
								<button class="rt-tag-remove" @click="rule.domains.splice(di, 1); markDirty()">✕</button>
							</span>
						</div>
						<div class="rt-tag-input-wrap">
							<input
								v-model="domainInput[idx]"
								class="rt-input rt-tag-input"
								placeholder="домен.com или geosite:google или regexp:..."
								@keydown.enter.prevent="addDomain(idx)"
								@input="showDomainSuggest(idx, $event.target.value)"
							>
							<div v-if="domainSuggestions[idx]?.length" class="rt-suggest">
								<div
									v-for="s in domainSuggestions[idx]"
									:key="s.value + (s.db || '')"
									class="rt-suggest-item"
									@click="addDomainEntry(idx, s); domainInput[idx] = ''; domainSuggestions[idx] = []"
								>
									<span>{{ s.flag || '📁' }} {{ s.db ? `ext:${s.db}:${s.value}` : `geosite:${s.value}` }}</span>
									<span class="rt-suggest-label">{{ s.label }}</span>
								</div>
							</div>
						</div>
					</div>

					<!-- IPs -->
					<div class="rt-field">
						<label class="rt-field-label">IP-адреса и подсети</label>
						<div class="rt-tag-list">
							<span v-for="(ip, ii) in rule.ips" :key="ii" class="rt-tag" :class="'rt-tag-' + ip.type">
								<span class="rt-tag-icon">{{ entryIcon(ip) }}</span>
								<span class="rt-tag-text">{{ entryLabel(ip) }}</span>
								<button class="rt-tag-remove" @click="rule.ips.splice(ii, 1); markDirty()">✕</button>
							</span>
						</div>
						<div class="rt-tag-input-wrap">
							<input
								v-model="ipInput[idx]"
								class="rt-input rt-tag-input"
								placeholder="1.2.3.0/24 или geoip:ru"
								@keydown.enter.prevent="addIp(idx)"
								@input="showIpSuggest(idx, $event.target.value)"
							>
							<div v-if="ipSuggestions[idx]?.length" class="rt-suggest">
								<div
									v-for="s in ipSuggestions[idx]"
									:key="s.value + (s.db || '')"
									class="rt-suggest-item"
									@click="addIpEntry(idx, s); ipInput[idx] = ''; ipSuggestions[idx] = []"
								>
									<span>{{ s.flag || '🌍' }} {{ s.db ? `ext:${s.db}:${s.value}` : `geoip:${s.value}` }}</span>
									<span class="rt-suggest-label">{{ s.label }}</span>
								</div>
							</div>
						</div>
					</div>

					<!-- Two-column: protocol/port + action -->
					<div class="rt-row-2col">
						<div class="rt-field">
							<label class="rt-field-label">Протокол</label>
							<div class="rt-checkboxes">
								<label class="rt-check"><input type="checkbox" :checked="rule.networks.includes('tcp')" @change="toggleNetwork(rule, 'tcp')"> TCP</label>
								<label class="rt-check"><input type="checkbox" :checked="rule.networks.includes('udp')" @change="toggleNetwork(rule, 'udp')"> UDP</label>
							</div>
						</div>
						<div class="rt-field">
							<label class="rt-field-label">Порт</label>
							<input v-model="rule.port" class="rt-input" placeholder="* или 443 или 19200-19400" @input="markDirty">
						</div>
					</div>

					<!-- Action selector -->
					<div class="rt-field">
						<label class="rt-field-label">Действие</label>
						<div class="rt-actions">
							<button
								class="rt-action-btn"
								:class="{ active: rule.action.tag === 'direct' && rule.action.kind === 'outbound' }"
								@click="rule.action = { kind: 'outbound', tag: 'direct' }; markDirty()"
							>⚪ Direct</button>
							<button
								class="rt-action-btn"
								:class="{ active: rule.action.kind === 'balancer' }"
								@click="rule.action = { kind: 'balancer', tag: 'default-balancer' }; markDirty()"
							>🟢 Balancer</button>
							<button
								class="rt-action-btn"
								:class="{ active: rule.action.tag === 'warp' }"
								@click="rule.action = { kind: 'outbound', tag: 'warp' }; markDirty()"
							>🔵 Warp</button>
							<button
								class="rt-action-btn"
								:class="{ active: rule.action.tag === 'block' }"
								@click="rule.action = { kind: 'outbound', tag: 'block' }; markDirty()"
							>🔴 Block</button>
						</div>
					</div>

					<div class="rt-card-footer">
						<button class="btn btn-sm" @click="expandedId = null">Готово</button>
					</div>
				</div>
			</div>
		</div>

		<!-- Loading / error -->
		<div v-if="loading" class="rt-loading">Загрузка...</div>
		<div v-if="error" class="rt-error">⚠️ {{ error }}</div>
	</div>
</template>

<script setup>
import { ref, reactive, onMounted, computed } from 'vue';
import {
	getRouting, saveRouting, normalizeRule, parseEntry,
	entryLabel, entryIcon, COMMON_GEOSITE, COMMON_GEOIP,
	serializeRule,
} from '../services/routing-rules.js';

const loading = ref(true);
const error = ref('');
const dirty = ref(false);
const expandedId = ref(null);
const showTemplates = ref(false);

const localRouting = reactive({ domainStrategy: 'AsIs' });
const rawRules = ref([]);
const rawBalancers = ref([]);

const rules = computed(() => rawRules.value);

// Drag-and-drop state
const dragIdx = ref(null);
const dragOverIdx = ref(null);

// Tag input state
const domainInput = reactive({});
const domainSuggestions = reactive({});
const ipInput = reactive({});
const ipSuggestions = reactive({});

onMounted(async () => {
	try {
		const data = await getRouting();
		const routing = data.routing || data;
		localRouting.domainStrategy = routing.domainStrategy || 'AsIs';
		rawBalancers.value = routing.balancers || [];
		rawRules.value = (routing.rules || []).map((r, i) => normalizeRule(r, i));
	} catch (e) {
		error.value = e.message || 'Failed to load routing config';
	} finally {
		loading.value = false;
	}
});

function markDirty() { dirty.value = true; }

function pluralize(n) {
	const mod10 = n % 10, mod100 = n % 100;
	if (mod10 === 1 && mod100 !== 11) return 'правило';
	if (mod10 >= 2 && mod10 <= 4 && (mod100 < 10 || mod100 >= 20)) return 'правила';
	return 'правил';
}

// ── Expand/collapse ──
function toggleExpand(id) {
	expandedId.value = expandedId.value === id ? null : id;
}

// ── Drag and drop ──
function onDragStart(e, idx) {
	dragIdx.value = idx;
	e.dataTransfer.effectAllowed = 'move';
	e.dataTransfer.setData('text/plain', String(idx));
}
function onDragOver(idx) {
	dragOverIdx.value = idx;
}
function onDragLeave() {
	dragOverIdx.value = null;
}
function onDrop(targetIdx) {
	const srcIdx = dragIdx.value;
	if (srcIdx === null || srcIdx === targetIdx) return;
	const moved = rawRules.value.splice(srcIdx, 1)[0];
	rawRules.value.splice(targetIdx, 0, moved);
	markDirty();
	onDragEnd();
}
function onDragEnd() {
	dragIdx.value = null;
	dragOverIdx.value = null;
}

// ── Rule operations ──
function addRule() {
	const newRule = normalizeRule({
		type: 'field',
		domain: [],
		outboundTag: 'direct',
	}, Date.now());
	newRule.name = 'Новое правило';
	rawRules.value.splice(rawRules.value.length - 1, 0, newRule); // before catch-all
	expandedId.value = newRule.id;
	markDirty();
}

function deleteRule(idx) {
	rawRules.value.splice(idx, 1);
	markDirty();
}

// ── Domain/IP tag input ──
function addDomain(idx) {
	const val = (domainInput[idx] || '').trim();
	if (!val) return;
	rawRules.value[idx].domains.push(parseEntry(val));
	domainInput[idx] = '';
	domainSuggestions[idx] = [];
	markDirty();
}

function addDomainEntry(idx, suggestion) {
	const raw = suggestion.db
		? `ext:${suggestion.db}:${suggestion.value}`
		: `geosite:${suggestion.value}`;
	rawRules.value[idx].domains.push(parseEntry(raw));
	markDirty();
}

function showDomainSuggest(idx, val) {
	if (!val || val.length < 2) { domainSuggestions[idx] = []; return; }
	const q = val.replace(/^geosite:|^ext:.*:/, '').toLowerCase();
	domainSuggestions[idx] = COMMON_GEOSITE
		.filter(s => s.value.toLowerCase().includes(q) || s.label.toLowerCase().includes(q))
		.slice(0, 8);
}

function addIp(idx) {
	const val = (ipInput[idx] || '').trim();
	if (!val) return;
	rawRules.value[idx].ips.push(parseEntry(val));
	ipInput[idx] = '';
	ipSuggestions[idx] = [];
	markDirty();
}

function addIpEntry(idx, suggestion) {
	const raw = suggestion.db
		? `ext:${suggestion.db}:${suggestion.value}`
		: `geoip:${suggestion.value}`;
	rawRules.value[idx].ips.push(parseEntry(raw));
	markDirty();
}

function showIpSuggest(idx, val) {
	if (!val || val.length < 2) { ipSuggestions[idx] = []; return; }
	const q = val.replace(/^geoip:|^ext:.*:/, '').toLowerCase();
	ipSuggestions[idx] = COMMON_GEOIP
		.filter(s => s.value.toLowerCase().includes(q) || s.label.toLowerCase().includes(q))
		.slice(0, 8);
}

function toggleNetwork(rule, net) {
	const i = rule.networks.indexOf(net);
	if (i >= 0) rule.networks.splice(i, 1);
	else rule.networks.push(net);
	markDirty();
}

// ── Helpers ──
function ruleIcon(rule) {
	if (rule.domains.length) return entryIcon(rule.domains[0]);
	if (rule.ips.length) return entryIcon(rule.ips[0]);
	return '📭';
}

function actionClass(action) {
	if (action.tag === 'direct') return 'rt-act-direct';
	if (action.kind === 'balancer') return 'rt-act-balancer';
	if (action.tag === 'warp') return 'rt-act-warp';
	if (action.tag === 'block') return 'rt-act-block';
	return 'rt-act-other';
}

function actionLabel(action) {
	if (action.tag === 'direct') return '⚪ DIRECT';
	if (action.kind === 'balancer') return '🟢 BALANCER';
	if (action.tag === 'warp') return '🔵 WARP';
	if (action.tag === 'block') return '🔴 BLOCK';
	return action.tag.toUpperCase();
}

// ── Templates ──
function applyTemplate(name) {
	const templates = {
		'ru-direct': {
			name: '🇷🇺 RU Direct',
			domains: [
				parseEntry('regexp:^([\\w\\-\\.]+\\.)ru$'),
				parseEntry('ext:geosite_v2fly.dat:category-ru'),
			],
			ips: [],
			networks: [],
			port: '',
			action: { kind: 'outbound', tag: 'direct' },
		},
		'block-ads': {
			name: '🚫 Block Ads',
			domains: [parseEntry('ext:geosite_v2fly.dat:category-ads')],
			ips: [],
			networks: [],
			port: '',
			action: { kind: 'outbound', tag: 'block' },
		},
		'streaming': {
			name: '📺 Streaming',
			domains: [
				parseEntry('geosite:netflix'),
				parseEntry('geosite:youtube'),
			],
			ips: [],
			networks: [],
			port: '',
			action: { kind: 'balancer', tag: 'default-balancer' },
		},
	};
	const tpl = templates[name];
	if (!tpl) return;
	const newRule = normalizeRule({
		type: 'field',
		domain: tpl.domains.map(d => d.raw),
		outboundTag: tpl.action.kind === 'outbound' ? tpl.action.tag : undefined,
		balancerTag: tpl.action.kind === 'balancer' ? tpl.action.tag : undefined,
	}, Date.now());
	newRule.name = tpl.name;
	rawRules.value.splice(rawRules.value.length - 1, 0, newRule);
	expandedId.value = newRule.id;
	showTemplates.value = false;
	markDirty();
}

// ── Save ──
async function save() {
	loading.value = true;
	error.value = '';
	try {
		const rulesJson = rawRules.value.map(r => serializeRule(r));

		await saveRouting({
			domainStrategy: localRouting.domainStrategy,
			balancers: rawBalancers.value,
			rules: rulesJson,
		});
		dirty.value = false;
	} catch (e) {
		error.value = e.message || 'Failed to save';
	} finally {
		loading.value = false;
	}
}
</script>

<style scoped>
.routing-container {
	max-width: 900px;
	margin: 0 auto;
	padding: 12px;
}

.rt-header {
	display: flex;
	justify-content: space-between;
	align-items: center;
	margin-bottom: 8px;
	flex-wrap: wrap;
	gap: 8px;
}
.rt-header-left, .rt-header-right {
	display: flex;
	align-items: center;
	gap: 8px;
}
.rt-strategy { display: flex; align-items: center; gap: 6px; font-size: 13px; color: var(--text-muted); }
.rt-select {
	background: var(--input-bg, #1a1a2e);
	color: var(--text);
	border: 1px solid var(--border, #333);
	border-radius: 4px;
	padding: 2px 6px;
	font-size: 13px;
}
.rt-rule-count { font-size: 12px; color: var(--text-muted); }

.rt-info {
	font-size: 12px;
	color: var(--help-text, #888);
	background: var(--card-bg, rgba(255,255,255,0.04));
	border-radius: 6px;
	padding: 8px 12px;
	margin-bottom: 12px;
}
.drag-hint {
	cursor: grab;
	font-weight: bold;
	letter-spacing: -2px;
}

/* Templates */
.rt-templates {
	display: flex;
	gap: 8px;
	margin-bottom: 12px;
	flex-wrap: wrap;
}
.rt-template {
	display: flex;
	align-items: center;
	gap: 6px;
	padding: 8px 12px;
	background: var(--card-bg, rgba(255,255,255,0.06));
	border: 1px solid var(--border, #333);
	border-radius: 6px;
	cursor: pointer;
	transition: background 0.15s;
	font-size: 13px;
}
.rt-template:hover { background: var(--card-hover, rgba(255,255,255,0.12)); }
.rt-tpl-icon { font-size: 18px; }
.rt-tpl-name { font-weight: 600; }
.rt-tpl-desc { color: var(--text-muted); font-size: 11px; }

/* Rule cards */
.rt-rules { display: flex; flex-direction: column; gap: 6px; }
.rt-card {
	background: var(--card-bg, rgba(255,255,255,0.04));
	border: 1px solid var(--border, #2a2a3e);
	border-radius: 8px;
	transition: border-color 0.15s, opacity 0.15s;
}
.rt-card.dragging { opacity: 0.4; }
.rt-card.drag-over { border-color: var(--accent, #4a9eff); border-style: dashed; }
.rt-card.expanded { border-color: var(--accent, #4a9eff); }

.rt-card-header {
	display: flex;
	align-items: center;
	gap: 8px;
	padding: 8px 12px;
	cursor: pointer;
	user-select: none;
}
.rt-drag-handle {
	cursor: grab;
	color: var(--text-muted, #555);
	font-size: 14px;
	line-height: 1;
	letter-spacing: -3px;
	padding-right: 4px;
}
.rt-drag-handle:active { cursor: grabbing; }
.rt-card-num {
	font-size: 11px;
	color: var(--text-muted);
	background: var(--badge-bg, rgba(255,255,255,0.1));
	border-radius: 50%;
	width: 20px;
	height: 20px;
	display: flex;
	align-items: center;
	justify-content: center;
	flex-shrink: 0;
}
.rt-card-icon { font-size: 16px; flex-shrink: 0; }
.rt-card-name {
	flex: 1;
	font-size: 13px;
	font-weight: 500;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.rt-card-summary { display: flex; gap: 4px; flex-shrink: 0; }
.rt-badge {
	font-size: 10px;
	padding: 1px 5px;
	border-radius: 3px;
	background: var(--badge-bg, rgba(255,255,255,0.1));
	color: var(--text-muted);
}
.rt-badge-domain { color: var(--accent, #4a9eff); }
.rt-badge-ip { color: var(--warning, #f5a623); }

/* Action badge */
.rt-card-action {
	font-size: 11px;
	font-weight: 600;
	padding: 2px 8px;
	border-radius: 4px;
	white-space: nowrap;
	flex-shrink: 0;
}
.rt-act-direct { background: rgba(255,255,255,0.08); color: #aaa; }
.rt-act-balancer { background: rgba(74,158,255,0.15); color: #4a9eff; }
.rt-act-warp { background: rgba(100,100,255,0.15); color: #6464ff; }
.rt-act-block { background: rgba(231,76,60,0.15); color: #e74c3c; }
.rt-act-other { background: rgba(255,193,7,0.15); color: #ffc107; }

.rt-card-actions { display: flex; gap: 4px; flex-shrink: 0; }
.rt-icon-btn {
	background: none;
	border: none;
	color: var(--text-muted);
	cursor: pointer;
	font-size: 14px;
	padding: 2px 4px;
	border-radius: 4px;
	transition: background 0.15s;
}
.rt-icon-btn:hover { background: rgba(255,255,255,0.1); }
.rt-icon-danger:hover { color: #e74c3c; }

/* Card body (expanded) */
.rt-card-body {
	padding: 12px;
	border-top: 1px solid var(--border, #2a2a3e);
	display: flex;
	flex-direction: column;
	gap: 12px;
}
.rt-field { display: flex; flex-direction: column; gap: 4px; }
.rt-field-label {
	font-size: 11px;
	color: var(--text-muted);
	text-transform: uppercase;
	letter-spacing: 0.5px;
}
.rt-input {
	background: var(--input-bg, #1a1a2e);
	color: var(--text);
	border: 1px solid var(--border, #333);
	border-radius: 4px;
	padding: 6px 10px;
	font-size: 13px;
	font-family: inherit;
}
.rt-input:focus { outline: none; border-color: var(--accent, #4a9eff); }

/* Tag list */
.rt-tag-list {
	display: flex;
	flex-wrap: wrap;
	gap: 4px;
	min-height: 4px;
}
.rt-tag {
	display: inline-flex;
	align-items: center;
	gap: 4px;
	padding: 2px 8px;
	border-radius: 4px;
	font-size: 12px;
	background: var(--badge-bg, rgba(255,255,255,0.06));
	border: 1px solid var(--border, #333);
}
.rt-tag-icon { font-size: 11px; }
.rt-tag-remove {
	background: none;
	border: none;
	color: var(--text-muted);
	cursor: pointer;
	font-size: 11px;
	padding: 0;
	line-height: 1;
}
.rt-tag-remove:hover { color: #e74c3c; }
.rt-tag-geosite, .rt-tag-ext { border-color: rgba(74,158,255,0.3); }
.rt-tag-geoip { border-color: rgba(46,204,113,0.3); }
.rt-tag-regexp { border-color: rgba(155,89,182,0.3); font-family: monospace; }
.rt-tag-cidr { border-color: rgba(243,156,18,0.3); font-family: monospace; }

/* Tag input with suggestions */
.rt-tag-input-wrap { position: relative; }
.rt-tag-input { width: 100%; }
.rt-suggest {
	position: absolute;
	top: 100%;
	left: 0;
	right: 0;
	background: var(--input-bg, #1a1a2e);
	border: 1px solid var(--border, #333);
	border-radius: 4px;
	max-height: 200px;
	overflow-y: auto;
	z-index: 10;
	box-shadow: 0 4px 12px rgba(0,0,0,0.4);
}
.rt-suggest-item {
	padding: 6px 10px;
	cursor: pointer;
	font-size: 12px;
	display: flex;
	justify-content: space-between;
	align-items: center;
	gap: 8px;
}
.rt-suggest-item:hover { background: var(--card-hover, rgba(255,255,255,0.08)); }
.rt-suggest-label { color: var(--text-muted); font-size: 11px; }

/* Two-column layout */
.rt-row-2col {
	display: grid;
	grid-template-columns: 1fr 1fr;
	gap: 12px;
}
.rt-checkboxes { display: flex; gap: 12px; }
.rt-check {
	display: flex;
	align-items: center;
	gap: 4px;
	font-size: 13px;
	cursor: pointer;
}

/* Action buttons */
.rt-actions { display: flex; gap: 6px; flex-wrap: wrap; }
.rt-action-btn {
	padding: 6px 14px;
	border-radius: 6px;
	border: 1px solid var(--border, #333);
	background: var(--input-bg, #1a1a2e);
	color: var(--text-muted);
	cursor: pointer;
	font-size: 13px;
	transition: all 0.15s;
}
.rt-action-btn:hover { border-color: var(--accent, #4a9eff); }
.rt-action-btn.active {
	border-color: var(--accent, #4a9eff);
	background: rgba(74,158,255,0.15);
	color: var(--accent, #4a9eff);
	font-weight: 600;
}

.rt-card-footer {
	display: flex;
	justify-content: flex-end;
	margin-top: 4px;
}

.rt-loading, .rt-error {
	text-align: center;
	padding: 20px;
	color: var(--text-muted);
}
.rt-error { color: #e74c3c; }

.btn-sm { font-size: 12px; padding: 4px 10px; }
.btn-success { background: #27ae60; color: white; border: none; }

@media (max-width: 640px) {
	.rt-row-2col { grid-template-columns: 1fr; }
	.rt-card-summary { display: none; }
	.rt-header { flex-direction: column; align-items: stretch; }
}
</style>
