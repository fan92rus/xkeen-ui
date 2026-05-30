<script setup>
import { ref, reactive, computed, onMounted } from 'vue';
import { useAppStore } from '../stores/app.js';
import * as api from '../services/subscription.js';

const app = useAppStore();

const subs = ref([]);
const proxies = ref([]);
const previewData = ref(null);
const filters = reactive({
    include_markers: [], exclude_markers: [],
    include_countries: [], exclude_countries: [],
    include_regex: '', exclude_regex: '', max_proxies: 50
});
const strategy = reactive({ type: 'all' });
const busy = ref(false);
const editId = ref(null);
const edit = reactive({ name: '', url: '', interval: 0, enabled: true });
const newUrl = ref('');
const proxyQ = ref('');
const showPreview = ref(false);
const markers = ref([]);

const STRATS = [
    { v: 'all', l: 'Первый' }, { v: 'random', l: 'Случайный' },
    { v: 'roundrobin', l: 'По очереди' }, { v: 'leastping', l: 'Мин. пинг' },
    { v: 'leastload', l: 'Мин. нагрузка' }
];

const allCountries = computed(() => {
    const set = new Set(proxies.value.map(p => p.country).filter(Boolean));
    return [...set].sort();
});

const filteredProxies = computed(() => {
    const q = proxyQ.value.toLowerCase();
    return proxies.value.filter(p =>
        !q || [p.tag, p.remarks, p.country, p.protocol].some(v => (v || '').toLowerCase().includes(q))
    );
});

function _extractMarkers(px) {
    const counts = {};
    for (const p of px) { if (p.marker) counts[p.marker] = (counts[p.marker] || 0) + 1; }
    return Object.keys(counts).filter(m => counts[m] >= 2).sort();
}

async function _reload() {
    subs.value = (await api.listSubscriptions()).subscriptions || [];
}

async function _persist() {
    await Promise.all([api.updateFilters({ ...filters }), api.updateStrategy({ ...strategy })]);
}

function _toast(msg, type) { app.showToast(msg, type); }
function _err(e) { _toast(e.message || 'Ошибка', 'error'); }

// -- Subscriptions --
async function add() {
    const url = newUrl.value.trim();
    if (!url) return;
    busy.value = true;
    try { await api.addSubscription({ name: '', url, interval: 0, enabled: true }); newUrl.value = ''; await _reload(); }
    catch (e) { _err(e); } finally { busy.value = false; }
}

function editStart(s) { editId.value = s.id; Object.assign(edit, s); }
function editCancel() { editId.value = null; }

async function editSave() {
    try {
        await api.updateSubscription(editId.value, { name: edit.name, url: edit.url, interval: edit.interval, enabled: edit.enabled });
        editId.value = null;
        await _reload();
    } catch (e) { _err(e); }
}

async function remove(id) {
    if (!confirm('Удалить подписку?')) return;
    try { await api.deleteSubscription(id); await _reload(); } catch (e) { _err(e); }
}

async function fetchOne(id) {
    busy.value = true;
    try {
        const d = await api.fetchSubscription(id);
        await _reload();
        if (d.proxies?.length) { proxies.value = d.proxies; markers.value = _extractMarkers(d.proxies); }
        _toast(d.error ? d.error : `+${d.total || d.proxy_count || 0} прокси`, d.error ? 'error' : 'success');
    } catch (e) { _err(e); } finally { busy.value = false; }
}

async function fetchAll() {
    busy.value = true;
    try {
        let allP = [], n = 0;
        for (const s of subs.value.filter(x => x.enabled)) {
            try { const d = await api.fetchSubscription(s.id); n += d.total || 0; if (d.proxies) allP = allP.concat(d.proxies); }
            catch { /* skip */ }
        }
        await _reload();
        proxies.value = allP; markers.value = _extractMarkers(allP);
        _toast(`Обновлено ${n} прокси (${allP.length} после фильтра)`, 'success');
    } catch (e) { _err(e); } finally { busy.value = false; }
}

async function loadProxies() {
    try {
        const d = await api.getProxies();
        proxies.value = d.proxies || [];
        if (d.markers) markers.value = d.markers;
    } catch { proxies.value = []; }
}

// -- Markers --
function markerExcl(id) { return filters.exclude_markers.includes(id); }
function toggleMarker(id) {
    const i = filters.exclude_markers.indexOf(id);
    if (i >= 0) filters.exclude_markers.splice(i, 1);
    else filters.exclude_markers.push(id);
}
function countByMarker(m) { return proxies.value.filter(p => p.marker === m).length; }

// -- Countries --
function toggleCountry(c) {
    const ii = filters.include_countries.indexOf(c);
    const ei = filters.exclude_countries.indexOf(c);
    if (ii >= 0) filters.include_countries.splice(ii, 1);
    else if (ei >= 0) filters.exclude_countries.splice(ei, 1);
    else filters.exclude_countries.push(c);
}
function countByCountry(c) { return proxies.value.filter(p => p.country === c).length; }

// -- Preview & Apply --
async function preview() {
    busy.value = true;
    try { await _persist(); previewData.value = await api.previewSubscriptions(); showPreview.value = true; }
    catch (e) { _err(e); } finally { busy.value = false; }
}

async function apply() {
    if (!confirm('Применить и перезапустить Xkeen?')) return;
    busy.value = true;
    try {
        await _persist();
        const d = await api.applySubscriptions();
        if (d.error) _toast(d.error, 'error');
        else { _toast('Применено, Xkeen перезапускается', 'success'); showPreview.value = false; }
    } catch (e) { _err(e); } finally { busy.value = false; }
}

function fmtTime(t) {
    if (!t || t === '0001-01-01T00:00:00Z') return '';
    return new Date(t).toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
}
function stratLabel(v) { return STRATS.find(s => s.v === (v || strategy.type))?.l || v || ''; }

onMounted(async () => {
    try {
        const [d, f, s] = await Promise.all([api.listSubscriptions(), api.getFilters(), api.getStrategy()]);
        subs.value = d.subscriptions || [];
        if (f) Object.assign(filters, f);
        if (s) Object.assign(strategy, s);
    } catch (e) { console.error('[sub] init error:', e); }
});
</script>

<template>
  <!-- Toolbar -->
  <div class="sub-toolbar">
    <input type="url" v-model="newUrl" @keydown.enter="add()"
           placeholder="URL подписки... Enter чтобы добавить" :disabled="busy">
    <button @click="add()" :disabled="busy || !newUrl.trim()" class="btn btn-primary btn-sm">+</button>
    <button @click="fetchAll()" :disabled="busy || !subs.length" class="btn btn-sm" title="Обновить все">🔄</button>
    <button @click="loadProxies()" :disabled="busy || !subs.length" class="btn btn-sm" title="Загрузить прокси">📋</button>
    <div class="sub-toolbar-spacer"></div>
    <button @click="preview()" :disabled="busy || !proxies.length" class="btn btn-sm">👁 Предпросмотр</button>
    <button @click="apply()" :disabled="busy || !proxies.length" class="btn btn-primary btn-sm">✅ Применить</button>
  </div>

  <div class="sub-scroll sub-2col">
    <!-- LEFT -->
    <div class="sub-left">
      <div v-for="s in subs" :key="s.id" class="sub-card" :class="{ editing: editId === s.id }">
        <div v-if="editId !== s.id" class="sub-row">
          <span class="dot" :class="s.enabled ? 'on' : 'off'"></span>
          <span class="name">{{ s.name || 'Без названия' }}</span>
          <span class="badge" v-show="s.proxy_count">{{ (s.proxy_count || 0) }} шт</span>
          <span class="meta" v-show="s.interval > 0">{{ s.interval }} мин</span>
          <span class="meta" v-show="s.last_fetch">{{ fmtTime(s.last_fetch) }}</span>
          <span class="err" v-show="s.last_error">{{ s.last_error }}</span>
          <span class="acts">
            <button @click="fetchOne(s.id)" :disabled="busy" title="Обновить">🔄</button>
            <button @click="editStart(s)" title="Редактировать">✏️</button>
            <button @click="remove(s.id)" class="danger" title="Удалить">🗑</button>
          </span>
        </div>
        <div v-else class="sub-edit">
          <input type="url" v-model="edit.url" class="sub-input">
          <div class="sub-edit-row">
            <input type="text" v-model="edit.name" placeholder="Название" class="sub-input">
            <input type="number" v-model.number="edit.interval" min="0" class="sub-input xs" placeholder="мин">
            <label><input type="checkbox" v-model="edit.enabled"> Вкл</label>
            <button @click="editSave()" class="btn btn-primary btn-sm">✓</button>
            <button @click="editCancel()" class="btn btn-sm">✕</button>
          </div>
        </div>
      </div>
      <div v-show="subs.length === 0" class="sub-empty">Вставьте URL подписки и нажмите Enter</div>

      <!-- Filters -->
      <div class="sub-settings" v-show="subs.length && proxies.length">
        <div class="px-stats">
          <span class="px-stat-total">Всего: <strong>{{ proxies.length }}</strong></span>
          <span class="px-stat-filtered">В выборке: <strong>{{ filteredProxies.length }}</strong></span>
          <span class="px-stat-excl" v-show="proxies.length - filteredProxies.length > 0">
            Исключено: <strong>{{ proxies.length - filteredProxies.length }}</strong>
          </span>
        </div>

        <div class="sub-row-label" v-show="markers.length">Маркеры</div>
        <div class="marker-pills" v-show="markers.length">
          <button v-for="m in markers" :key="m" class="mpill" :class="{ excl: markerExcl(m) }"
                  @click="toggleMarker(m)" :title="markerExcl(m) ? 'Вернуть' : 'Исключить'">
            <span>{{ m }}</span>
            <span class="mpill-cnt">{{ countByMarker(m) }}</span>
          </button>
        </div>

        <div class="sub-row-label" v-show="allCountries.length">Страны</div>
        <div class="country-section" v-show="allCountries.length">
          <div class="country-cloud">
            <button v-for="c in allCountries" :key="c" class="cc"
                    :class="{ 'cc-in': filters.include_countries.includes(c), 'cc-ex': filters.exclude_countries.includes(c), 'cc-off': !filters.include_countries.includes(c) && !filters.exclude_countries.includes(c) }"
                    @click="toggleCountry(c)">
              {{ c }} {{ countByCountry(c) }}
            </button>
          </div>
        </div>

        <div class="sub-row-compact">
          <input type="text" v-model="filters.include_regex" placeholder="Regex +" class="sub-input sm">
          <input type="text" v-model="filters.exclude_regex" placeholder="Regex −" class="sub-input sm">
          <input type="number" v-model.number="filters.max_proxies" min="0" class="sub-input xs" title="Макс прокси (0 = без лимита)" placeholder="Лимит">
        </div>

        <div class="sub-row-label">Роутинг</div>
        <div class="strat-pills">
          <button v-for="s in STRATS" :key="s.v" class="spill" :class="{ active: strategy.type === s.v }"
                  @click="strategy.type = s.v">{{ s.l }}</button>
        </div>
      </div>
    </div>

    <!-- RIGHT -->
    <div class="sub-right" v-show="proxies.length">
      <div class="px-header">
        <input type="text" v-model="proxyQ" placeholder="🔍 Поиск..." class="sub-input">
        <span class="px-count">{{ filteredProxies.length }} / {{ proxies.length }}</span>
      </div>
      <div class="px-list">
        <div v-for="p in filteredProxies" :key="p.tag" class="px-row">
          <span class="px-country" :title="p.remarks">{{ p.country || '?' }}</span>
          <span class="px-marker" v-show="p.marker">{{ p.marker }}</span>
          <span class="px-remarks">{{ p.remarks || p.tag }}</span>
          <span class="px-tag mono">{{ p.tag }}</span>
        </div>
      </div>
    </div>
  </div>

  <!-- Preview modal -->
  <div class="modal-overlay" v-show="showPreview" @click.self="showPreview = false">
    <div class="modal modal-large">
      <div class="modal-header">
        <h3>Предпросмотр</h3>
        <button class="modal-close" @click="showPreview = false">&times;</button>
      </div>
      <div class="modal-body">
        <div class="preview-summary">
          <span>Прокси: <strong>{{ previewData?.proxy_count || 0 }}</strong></span>
          <span>Стратегия: <strong>{{ stratLabel(previewData?.strategy) }}</strong></span>
        </div>
        <details v-show="previewData?.outbounds" open>
          <summary>04_outbounds.json</summary>
          <pre class="preview-json">{{ previewData?.outbounds }}</pre>
        </details>
        <details v-show="previewData?.routing">
          <summary>05_routing.json</summary>
          <pre class="preview-json">{{ previewData?.routing }}</pre>
        </details>
        <details v-show="previewData?.observatory">
          <summary>07_observatory.json</summary>
          <pre class="preview-json">{{ previewData?.observatory }}</pre>
        </details>
      </div>
      <div class="modal-footer">
        <button class="btn" @click="showPreview = false">Закрыть</button>
        <button class="btn btn-primary" @click="apply()" :disabled="busy">✅ Применить</button>
      </div>
    </div>
  </div>
</template>
