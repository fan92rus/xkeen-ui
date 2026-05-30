<script setup>
import { ref, reactive, computed, onMounted } from 'vue';
import { useAppStore } from '../stores/app.js';
import * as api from '../services/subscription.js';

const app = useAppStore();

/* ---- state ---- */
const subs = ref([]);
const proxies = ref([]);
const previewData = ref(null);
const filters = reactive({
    include_markers: [], exclude_markers: [],
    include_countries: [], exclude_countries: [],
    include_regex: '', exclude_regex: '', max_proxies: 0
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
    { v: 'all', l: 'Все', tip: 'Все прокси через первый' },
    { v: 'random', l: 'Случайный', tip: 'Случайный выбор' },
    { v: 'roundrobin', l: 'По очереди', tip: 'Равномерное распределение' },
    { v: 'leastping', l: 'Мин. пинг', tip: 'Требует observatory' },
    { v: 'leastload', l: 'Мин. нагр.', tip: 'Требует observatory' }
];

/* ---- computed ---- */
const allCountries = computed(() => {
    const set = new Set(proxies.value.map(p => p.country).filter(Boolean));
    return [...set].sort();
});

const filteredProxies = computed(() => {
    let list = proxies.value;

    // Exclude markers
    if (filters.exclude_markers.length) {
        const ex = new Set(filters.exclude_markers);
        list = list.filter(p => !ex.has(p.marker));
    }
    // Include markers (empty = all)
    if (filters.include_markers.length) {
        const inc = new Set(filters.include_markers);
        list = list.filter(p => inc.has(p.marker));
    }
    // Exclude countries
    if (filters.exclude_countries.length) {
        const ex = new Set(filters.exclude_countries.map(c => c.toUpperCase()));
        list = list.filter(p => !ex.has((p.country || '').toUpperCase()));
    }
    // Include countries (empty = all)
    if (filters.include_countries.length) {
        const inc = new Set(filters.include_countries.map(c => c.toUpperCase()));
        list = list.filter(p => inc.has((p.country || '').toUpperCase()));
    }
    // Include regex
    if (filters.include_regex) {
        try {
            const re = new RegExp(filters.include_regex, 'i');
            list = list.filter(p => re.test(p.remarks || ''));
        } catch { /* invalid regex, skip */ }
    }
    // Exclude regex
    if (filters.exclude_regex) {
        try {
            const re = new RegExp(filters.exclude_regex, 'i');
            list = list.filter(p => !re.test(p.remarks || ''));
        } catch { /* invalid regex, skip */ }
    }
    // Max proxies
    if (filters.max_proxies > 0 && list.length > filters.max_proxies) {
        list = list.slice(0, filters.max_proxies);
    }
    // Text search
    const q = proxyQ.value.toLowerCase();
    if (q) {
        list = list.filter(p =>
            [p.tag, p.remarks, p.country, p.protocol].some(v => (v || '').toLowerCase().includes(q))
        );
    }
    return list;
});

/* ---- helpers ---- */
function _fmtJson(obj) {
    const raw = JSON.stringify(obj, null, 2);
    return raw.replace(/("(?:[^"\\]|\\.)*")\s*:/g, '<span class="pk">$1</span>:')
              .replace(/:\s*("(?:[^"\\]|\\.)*")/g, ': <span class="ps">$1</span>')
              .replace(/:\s*(\d+\.?\d*)/g, ': <span class="pn">$1</span>')
              .replace(/:\s*(true|false)/g, ': <span class="pb">$1</span>')
              .replace(/:\s*(null)/g, ': <span class="pu">$1</span>');
}
function _extractMarkers(px) {
    const counts = {};
    for (const p of px) { if (p.marker) counts[p.marker] = (counts[p.marker] || 0) + 1; }
    return Object.keys(counts).filter(m => counts[m] >= 2).sort();
}
async function _persist() {
    await Promise.all([api.updateFilters({ ...filters }), api.updateStrategy({ ...strategy })]);
}
function _toast(msg, type) { app.showToast(msg, type); }
function _err(e) { console.error('[sub]', e); _toast(e.message || 'Ошибка', 'error'); }
async function _reload() {
    subs.value = (await api.listSubscriptions()).subscriptions || [];
}
function countByMarker(m) { return proxies.value.filter(p => p.marker === m).length; }
function countByCountry(c) { return proxies.value.filter(p => p.country === c).length; }
function fmtTime(t) {
    if (!t || t === '0001-01-01T00:00:00Z') return '';
    return new Date(t).toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
}

/* ---- subscription CRUD ---- */
async function add() {
    const url = newUrl.value.trim();
    if (!url) return;
    busy.value = true;
    try {
        await api.addSubscription({ name: '', url, interval: 0, enabled: true });
        newUrl.value = '';
        await _reload();
        _toast('Подписка добавлена', 'success');
    } catch (e) { _err(e); } finally { busy.value = false; }
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

/* ---- fetch ---- */
async function fetchOne(id) {
    busy.value = true;
    try {
        const d = await api.fetchSubscription(id);
        console.log('[sub] fetch result:', d.total, d.proxy_count, d.proxies?.length);
        // Set proxies FIRST, then reload metadata
        if (d.proxies?.length) {
            proxies.value = d.proxies;
            markers.value = _extractMarkers(d.proxies);
        }
        await _reload();
        const count = d.total || d.proxy_count || 0;
        _toast(`+${count} прокси`, d.error ? 'error' : 'success');
    } catch (e) {
        _err(e);
        // Even on error, try loading cached proxies
        try {
            const cached = await api.getProxies();
            if (cached.proxies?.length) {
                proxies.value = cached.proxies;
                if (cached.markers) markers.value = cached.markers;
                else markers.value = _extractMarkers(cached.proxies);
            }
        } catch { /* ignore */ }
    } finally { busy.value = false; }
}

async function fetchAll() {
    busy.value = true;
    try {
        let allP = [], n = 0;
        for (const s of subs.value.filter(x => x.enabled)) {
            try {
                const d = await api.fetchSubscription(s.id);
                n += d.total || 0;
                if (d.proxies) allP = allP.concat(d.proxies);
            } catch { /* skip failed */ }
        }
        await _reload();
        if (allP.length) {
            proxies.value = allP;
            markers.value = _extractMarkers(allP);
        }
        _toast(`Обновлено: ${n} прокси (${allP.length} после фильтра)`, 'success');
    } catch (e) { _err(e); } finally { busy.value = false; }
}

async function loadProxies() {
    try {
        const d = await api.getProxies();
        proxies.value = d.proxies || [];
        markers.value = d.markers || _extractMarkers(d.proxies || []);
    } catch (e) {
        console.error('[sub] loadProxies error:', e);
        proxies.value = [];
    }
}

/* ---- markers ---- */
function markerExcl(id) { return filters.exclude_markers.includes(id); }
function toggleMarker(id) {
    const i = filters.exclude_markers.indexOf(id);
    if (i >= 0) filters.exclude_markers.splice(i, 1);
    else filters.exclude_markers.push(id);
}

/* ---- countries ---- */
function countryState(c) {
    if (filters.include_countries.includes(c)) return 'in';
    if (filters.exclude_countries.includes(c)) return 'ex';
    return 'off';
}
function toggleCountry(c) {
    const ii = filters.include_countries.indexOf(c);
    const ei = filters.exclude_countries.indexOf(c);
    if (ii >= 0) { filters.include_countries.splice(ii, 1); }
    else if (ei >= 0) { filters.exclude_countries.splice(ei, 1); filters.include_countries.push(c); }
    else { filters.exclude_countries.push(c); }
}

/* ---- preview & apply ---- */
async function preview() {
    busy.value = true;
    try {
        await _persist();
        previewData.value = await api.previewSubscriptions();
        showPreview.value = true;
    } catch (e) { _err(e); } finally { busy.value = false; }
}
async function applySubs() {
    if (!confirm('Применить и перезапустить Xkeen?')) return;
    busy.value = true;
    try {
        await _persist();
        const d = await api.applySubscriptions();
        if (d.error) _toast(d.error, 'error');
        else { _toast('Применено. Xkeen перезапускается.', 'success'); showPreview.value = false; }
    } catch (e) { _err(e); } finally { busy.value = false; }
}

/* ---- init ---- */
onMounted(async () => {
    try {
        const [d, f, s] = await Promise.all([api.listSubscriptions(), api.getFilters(), api.getStrategy()]);
        subs.value = d.subscriptions || [];
        if (f) Object.assign(filters, f);
        if (s) Object.assign(strategy, s);
        // Auto-load cached proxies on mount
        await loadProxies();
    } catch (e) { console.error('[sub] init error:', e); }
});
</script>

<template>
<div class="sub-wrapper">
  <!-- Toolbar -->
  <div class="sub-toolbar">
    <input type="url" v-model="newUrl" @keydown.enter="add()"
           placeholder="URL подписки… Enter → добавить" :disabled="busy">
    <button @click="add()" :disabled="busy || !newUrl.trim()" class="btn btn-primary btn-sm">＋</button>
    <div class="sub-sep"></div>
    <button @click="fetchAll()" :disabled="busy || !subs.length" class="btn btn-sm" title="Обновить все подписки">↻ Обновить</button>
    <div class="sub-sep"></div>
    <button @click="preview()" :disabled="busy || !proxies.length" class="btn btn-sm">👁 Предпросмотр</button>
    <button @click="applySubs()" :disabled="busy || !proxies.length" class="btn btn-primary btn-sm">✓ Применить</button>
    <div class="sub-sep"></div>
    <!-- Strategy inline -->
    <span class="strat-label">Роутинг:</span>
    <div class="strat-pills">
      <button v-for="s in STRATS" :key="s.v" class="spill" :class="{ active: strategy.type === s.v }"
              @click="strategy.type = s.v" :title="s.tip">{{ s.l }}</button>
    </div>
  </div>

  <!-- Body: two columns -->
  <div class="sub-body">
    <!-- LEFT: subscriptions + filters -->
    <div class="sub-left">
      <!-- Subscription cards -->
      <div v-for="s in subs" :key="s.id" class="sub-card" :class="{ editing: editId === s.id }">
        <div v-if="editId !== s.id" class="sub-row">
          <span class="dot" :class="s.enabled ? 'on' : 'off'"></span>
          <span class="name">{{ s.name || 'Без названия' }}</span>
          <span class="badge" v-if="s.proxy_count">{{ s.proxy_count }}</span>
          <span class="meta" v-if="s.last_fetch && s.last_fetch !== '0001-01-01T00:00:00Z'">{{ fmtTime(s.last_fetch) }}</span>
          <span class="err" v-if="s.last_error" :title="s.last_error">⚠</span>
          <span class="acts">
            <button @click="fetchOne(s.id)" :disabled="busy" title="Обновить">↻</button>
            <button @click="editStart(s)" title="Редактировать">✎</button>
            <button @click="remove(s.id)" class="danger" title="Удалить">✕</button>
          </span>
        </div>
        <div v-else class="sub-edit">
          <input type="url" v-model="edit.url" class="sub-input" placeholder="URL">
          <div class="sub-edit-row">
            <input type="text" v-model="edit.name" placeholder="Название" class="sub-input">
            <input type="number" v-model.number="edit.interval" min="0" class="sub-input xs" title="Интервал (мин)" placeholder="мин">
            <label><input type="checkbox" v-model="edit.enabled"> Вкл</label>
            <button @click="editSave()" class="btn btn-primary btn-sm">✓</button>
            <button @click="editCancel()" class="btn btn-sm">✕</button>
          </div>
        </div>
      </div>

      <!-- Empty state for no subscriptions -->
      <div v-if="subs.length === 0" class="sub-empty">
        <p>📋 Нет подписок</p>
        <p class="sub-empty-hint">Вставьте URL подписки в поле выше</p>
      </div>

      <!-- Filters (always visible when proxies exist) -->
      <div class="sub-filters" v-if="proxies.length">
        <div class="sub-divider"></div>

        <div class="px-stats">
          <span>Всего: <strong>{{ proxies.length }}</strong></span>
          <span>В выборке: <strong>{{ filteredProxies.length }}</strong></span>
          <span v-if="proxies.length - filteredProxies.length" class="px-stat-excl">
            Исключено: <strong>{{ proxies.length - filteredProxies.length }}</strong>
          </span>
        </div>

        <!-- Markers -->
        <div v-if="markers.length">
          <div class="sub-row-label">Маркеры</div>
          <div class="marker-pills">
            <button v-for="m in markers" :key="m" class="mpill" :class="{ excl: markerExcl(m) }"
                    @click="toggleMarker(m)" :title="markerExcl(m) ? 'Вернуть' : 'Исключить'">
              {{ m }} <span class="mpill-cnt">{{ countByMarker(m) }}</span>
            </button>
          </div>
        </div>

        <!-- Countries -->
        <div v-if="allCountries.length">
          <div class="sub-row-label">Страны</div>
          <div class="country-cloud">
            <button v-for="c in allCountries" :key="c" class="cc"
                    :class="'cc-' + countryState(c)" @click="toggleCountry(c)">
              {{ c }} {{ countByCountry(c) }}
            </button>
          </div>
        </div>

        <!-- Regex + max -->
        <div class="sub-row-compact">
          <input type="text" v-model="filters.include_regex" placeholder="Regex +" class="sub-input sm">
          <input type="text" v-model="filters.exclude_regex" placeholder="Regex −" class="sub-input sm">
          <input type="number" v-model.number="filters.max_proxies" min="0" class="sub-input xs"
                 title="Макс прокси (0 = без лимита)" placeholder="Лимит">
        </div>
      </div>
    </div>

    <!-- RIGHT: proxy list -->
    <div class="sub-right">
      <template v-if="proxies.length">
        <div class="px-header">
          <input type="text" v-model="proxyQ" placeholder="🔍 Поиск…" class="sub-input">
          <span class="px-count">{{ filteredProxies.length }} / {{ proxies.length }}</span>
        </div>
        <div class="px-list">
          <div v-for="p in filteredProxies" :key="p.tag" class="px-row">
            <span class="px-country" :title="p.remarks">{{ p.country || '?' }}</span>
            <span class="px-marker" v-if="p.marker">{{ p.marker }}</span>
            <span class="px-remarks">{{ p.remarks || p.tag }}</span>
            <span class="px-tag mono">{{ p.tag }}</span>
          </div>
        </div>
      </template>
      <div v-else class="sub-right-empty">
        <p v-if="subs.length === 0">Добавьте подписку и обновите её</p>
        <p v-else>Нажмите ↻ на подписке для загрузки прокси</p>
      </div>
    </div>
  </div>

  <!-- Preview modal -->
  <div v-if="showPreview" class="modal-overlay" @click.self="showPreview = false">
    <div class="modal-box">
      <div class="modal-header">
        <h3>Предпросмотр</h3>
        <button @click="showPreview = false" class="btn btn-sm">✕</button>
      </div>
      <div class="modal-body" v-if="previewData">
        <div class="preview-summary">
          <span>Прокси: <strong>{{ previewData.proxy_count }}</strong></span>
          <span>Стратегия: <strong>{{ previewData.strategy }}</strong></span>
        </div>
        <details>
          <summary>outbounds.json</summary>
          <pre class="preview-json" v-html="_fmtJson(previewData.outbounds)"></pre>
        </details>
        <details>
          <summary>routing.json</summary>
          <pre class="preview-json" v-html="_fmtJson(previewData.routing)"></pre>
        </details>
        <details v-if="previewData.observatory">
          <summary>observatory.json</summary>
          <pre class="preview-json" v-html="_fmtJson(previewData.observatory)"></pre>
        </details>
      </div>
      <div class="modal-footer">
        <button @click="applySubs()" :disabled="busy" class="btn btn-primary">✓ Применить</button>
        <button @click="showPreview = false" class="btn">Закрыть</button>
      </div>
    </div>
  </div>
</div>
</template>
