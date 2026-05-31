<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue';
import { useAppStore } from '../stores/app.js';
import * as api from '../services/subscription.js';
import { fmtTime } from '../utils/format.js';

const app = useAppStore();

/* ---- state ---- */
const subs = ref([]);
const proxies = ref([]);
const previewData = ref(null);
const profiles = ref([]);
const busy = ref(false);
const editId = ref(null);
const edit = reactive({ name: '', url: '', interval: 0, enabled: true });
const newUrl = ref('');
const proxyQ = ref('');
const showPreview = ref(false);
const showProfileModal = ref(false);
const markers = ref([]);

const STRATS = [
    { v: 'all', l: 'Все', tip: 'Все прокси через первый' },
    { v: 'random', l: 'Случайный', tip: 'Случайный выбор' },
    { v: 'roundrobin', l: 'По очереди', tip: 'Равномерное распределение' },
    { v: 'leastping', l: 'Мин. пинг', tip: 'Требует observatory' },
    { v: 'leastload', l: 'Мин. нагр.', tip: 'Требует observatory' }
];

/* ---- default profile computed ---- */
const dp = computed(() => profiles.value.find(p => p.is_default) || null);
const filters = computed(() => dp.value?.filter || {
    include_markers: [], exclude_markers: [],
    include_countries: [], exclude_countries: [],
    include_regex: '', exclude_regex: '', max_proxies: 0
});
const strategy = computed(() => dp.value?.strategy || { type: 'all', replace_balancer_tag: false });

/* ---- profile editor state ---- */
const pe = reactive({
    id: '', name: '', enabled: true, is_default: false,
    filter: { exclude_markers: [], include_markers: [], exclude_countries: [], include_countries: [], include_regex: '', exclude_regex: '', max_proxies: 0 },
    strategy: { type: 'random' }
});
// String representations for filter arrays
const peStr = reactive({
    exclude_markers: '', include_markers: '',
    exclude_countries: '', include_countries: ''
});

/* ---- computed ---- */
const allCountries = computed(() => {
    const set = new Set(proxies.value.map(p => p.country).filter(Boolean));
    return [...set].sort();
});

const filteredProxies = computed(() => {
    let list = proxies.value;
    const f = filters.value;
    if (!f) return list;

    if (f.exclude_markers?.length) {
        const ex = new Set(f.exclude_markers);
        list = list.filter(p => !ex.has(p.marker));
    }
    if (f.include_markers?.length) {
        const inc = new Set(f.include_markers);
        list = list.filter(p => inc.has(p.marker));
    }
    if (f.exclude_countries?.length) {
        const ex = new Set(f.exclude_countries.map(c => c.toUpperCase()));
        list = list.filter(p => !ex.has((p.country || '').toUpperCase()));
    }
    if (f.include_countries?.length) {
        const inc = new Set(f.include_countries.map(c => c.toUpperCase()));
        list = list.filter(p => inc.has((p.country || '').toUpperCase()));
    }
    if (f.include_regex) {
        try {
            const re = new RegExp(f.include_regex, 'i');
            list = list.filter(p => re.test(p.remarks || ''));
        } catch { /* invalid regex */ }
    }
    if (f.exclude_regex) {
        try {
            const re = new RegExp(f.exclude_regex, 'i');
            list = list.filter(p => !re.test(p.remarks || ''));
        } catch { /* invalid regex */ }
    }
    if (f.max_proxies > 0 && list.length > f.max_proxies) {
        list = list.slice(0, f.max_proxies);
    }
    const q = proxyQ.value.toLowerCase();
    if (q) {
        list = list.filter(p =>
            [p.tag, p.remarks, p.country, p.protocol].some(v => (v || '').toLowerCase().includes(q))
        );
    }
    return list;
});

/* ---- helpers ---- */
function _esc(s) { return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'); }
function _fmtJson(obj) {
    const raw = _esc(JSON.stringify(obj, null, 2));
    return raw.replace(/(&quot;(?:[^&]|&(?!quot;))*?&quot;)\s*:/g, '<span class="pk">$1</span>:')
              .replace(/:\s*(&quot;(?:[^&]|&(?!quot;))*?&quot;)/g, ': <span class="ps">$1</span>')
              .replace(/:\s*(\d+\.?\d*)/g, ': <span class="pn">$1</span>')
              .replace(/:\s*(true|false)/g, ': <span class="pb">$1</span>')
              .replace(/:\s*(null)/g, ': <span class="pu">$1</span>');
}
function _extractMarkers(px) {
    const counts = {};
    for (const p of px) { if (p.marker) counts[p.marker] = (counts[p.marker] || 0) + 1; }
    return Object.keys(counts).filter(m => counts[m] >= 2).sort();
}
function _toast(msg, type) { app.showToast(msg, type); }
function _err(e) { console.error('[sub]', e); _toast(e.message || 'Ошибка', 'error'); }
async function _reload() {
    subs.value = (await api.listSubscriptions()).subscriptions || [];
}
function countByMarker(m) { return proxies.value.filter(p => p.marker === m).length; }
function countByCountry(c) { return proxies.value.filter(p => p.country === c).length; }

/* ---- persist default profile ---- */
async function _persistDefault() {
    if (!dp.value) return;
    await Promise.all([
        api.updateFilters(dp.value.filter),
        api.updateStrategy(dp.value.strategy)
    ]);
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
        if (d.proxies?.length) {
            proxies.value = d.proxies;
            markers.value = _extractMarkers(d.proxies);
        }
        await _reload();
        const count = d.total || d.proxy_count || 0;
        _toast(`+${count} прокси`, d.error ? 'error' : 'success');
    } catch (e) {
        _err(e);
        try {
            const cached = await api.getProxies();
            if (cached.proxies?.length) {
                proxies.value = cached.proxies;
                markers.value = cached.markers || _extractMarkers(cached.proxies);
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
        _toast(`Обновлено: ${n} прокси`, 'success');
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
function markerExcl(id) { return filters.value?.exclude_markers?.includes(id); }
async function toggleMarker(id) {
    if (!dp.value) return;
    const arr = dp.value.filter.exclude_markers || [];
    const i = arr.indexOf(id);
    if (i >= 0) arr.splice(i, 1);
    else arr.push(id);
    await api.updateFilters(dp.value.filter);
    await loadProfiles();
}

/* ---- countries ---- */
function countryState(c) {
    const f = filters.value;
    if (!f) return 'off';
    if (f.include_countries?.includes(c)) return 'in';
    if (f.exclude_countries?.includes(c)) return 'ex';
    return 'off';
}
async function toggleCountry(c) {
    if (!dp.value) return;
    const f = dp.value.filter;
    if (!f.include_countries) f.include_countries = [];
    if (!f.exclude_countries) f.exclude_countries = [];
    const ii = f.include_countries.indexOf(c);
    const ei = f.exclude_countries.indexOf(c);
    if (ii >= 0) { f.include_countries.splice(ii, 1); }
    else if (ei >= 0) { f.exclude_countries.splice(ei, 1); f.include_countries.push(c); }
    else { f.exclude_countries.push(c); }
    await api.updateFilters(f);
    await loadProfiles();
}

/* ---- strategy ---- */
async function setStrategy(type) {
    if (!dp.value) return;
    dp.value.strategy.type = type;
    await api.updateStrategy(dp.value.strategy);
    await loadProfiles();
}

/* ---- preview & apply ---- */
async function preview() {
    busy.value = true;
    try {
        await _persistDefault();
        previewData.value = await api.previewSubscriptions();
        showPreview.value = true;
    } catch (e) { _err(e); } finally { busy.value = false; }
}
async function applySubs() {
    if (!confirm('Применить и перезапустить Xkeen?')) return;
    busy.value = true;
    try {
        await _persistDefault();
        const d = await api.applySubscriptions();
        if (d.error) _toast(d.error, 'error');
        else { _toast('Применено. Xkeen перезапускается.', 'success'); showPreview.value = false; }
    } catch (e) { _err(e); } finally { busy.value = false; }
}

/* ---- profiles ---- */
async function loadProfiles() {
    try {
        profiles.value = await api.listProfiles();
    } catch (e) { console.error('[sub] loadProfiles:', e); }
}

function openNewProfile() {
    Object.assign(pe, {
        id: '', name: '', enabled: true, is_default: false,
        filter: { exclude_markers: [], include_markers: [], exclude_countries: [], include_countries: [], include_regex: '', exclude_regex: '', max_proxies: 0 },
        strategy: { type: 'random' }
    });
    Object.assign(peStr, { exclude_markers: '', include_markers: '', exclude_countries: '', include_countries: '' });
    showProfileModal.value = true;
}

function editProfile(p) {
    Object.assign(pe, JSON.parse(JSON.stringify(p)));
    // Pre-fill string fields from arrays
    Object.assign(peStr, {
        exclude_markers: (p.filter?.exclude_markers || []).join(', '),
        include_markers: (p.filter?.include_markers || []).join(', '),
        exclude_countries: (p.filter?.exclude_countries || []).join(', '),
        include_countries: (p.filter?.include_countries || []).join(', '),
    });
    showProfileModal.value = true;
}

function syncStrToArrays() {
    const split = s => (s || '').split(',').map(x => x.trim()).filter(Boolean);
    pe.filter.exclude_markers = split(peStr.exclude_markers);
    pe.filter.include_markers = split(peStr.include_markers);
    pe.filter.exclude_countries = split(peStr.exclude_countries);
    pe.filter.include_countries = split(peStr.include_countries);
}

async function saveProfile() {
    if (!pe.is_default && !pe.name?.trim()) {
        _toast('Введите название профиля', 'error');
        return;
    }
    syncStrToArrays();
    busy.value = true;
    try {
        if (pe.is_default) {
            await Promise.all([
                api.updateStrategy(pe.strategy),
                api.updateFilters(pe.filter)
            ]);
        } else if (pe.id) {
            await api.updateProfile(pe.id, pe);
        } else {
            await api.createProfile(pe);
        }
        showProfileModal.value = false;
        await loadProfiles();
        _toast('Профиль сохранён');
    } catch (e) { _err(e); } finally { busy.value = false; }
}

async function removeProfile(id) {
    if (!confirm('Удалить профиль?')) return;
    try { await api.deleteProfile(id); await loadProfiles(); _toast('Профиль удалён'); } catch (e) { _err(e); }
}

/* ---- virtual scroll ---- */
const scrollRef = ref(null);
const visibleStart = ref(0);
const ITEM_HEIGHT = 26;
const BUFFER = 10;

const visibleProxies = computed(() => {
    const list = filteredProxies.value;
    const start = Math.max(0, visibleStart.value - BUFFER);
    const end = Math.min(list.length, visibleStart.value + Math.ceil(600 / ITEM_HEIGHT) + BUFFER);
    return { items: list.slice(start, end), start, total: list.length };
});

function onScroll(e) {
    visibleStart.value = Math.floor(e.target.scrollTop / ITEM_HEIGHT);
}

/* ---- init ---- */
onMounted(async () => {
    try {
        const d = await api.listSubscriptions();
        subs.value = d.subscriptions || [];
        await Promise.all([loadProxies(), loadProfiles()]);
    } catch (e) { console.error('[sub] init error:', e); }
});
</script>

<template>
<div class="sub-wrapper">
  <!-- Toolbar -->
  <div class="sub-toolbar">
    <input type="url" v-model="newUrl" @keydown.enter="add()"
           placeholder="URL подписки… Enter → добавить" :disabled="busy">
    <button @click="add()" :disabled="busy || !newUrl.trim()" class="btn btn-primary btn-sm">Добавить</button>
    <div class="sub-sep"></div>
    <button @click="fetchAll()" :disabled="busy || !subs.length" class="btn btn-sm">Обновить все</button>
    <div class="sub-sep"></div>
    <button @click="preview()" :disabled="busy || !proxies.length" class="btn btn-sm">Предпросмотр</button>
    <button @click="applySubs()" :disabled="busy || !proxies.length" class="btn btn-primary btn-sm">Применить</button>
  </div>

  <!-- Body: two columns -->
  <div class="sub-body">
    <!-- LEFT: subscriptions + profiles + filters -->
    <div class="sub-left">
      <!-- Subscription cards -->
      <div v-for="s in subs" :key="s.id" class="sub-card" :class="{ editing: editId === s.id }">
        <div v-if="editId !== s.id" class="sub-row">
          <span class="dot" :class="s.enabled ? 'on' : 'off'"></span>
          <span class="name">{{ s.name || 'Без названия' }}</span>
          <span class="badge" v-if="s.proxy_count">{{ s.proxy_count }}</span>
          <span class="meta" v-if="s.last_fetch && s.last_fetch !== '0001-01-01T00:00:00Z'">{{ fmtTime(s.last_fetch) }}</span>
          <span class="err" v-if="s.last_error" :title="s.last_error">!</span>
          <span class="acts">
            <button @click="fetchOne(s.id)" :disabled="busy" title="Обновить">&#x21bb;</button>
            <button @click="editStart(s)" title="Редактировать">&#x270e;</button>
            <button @click="remove(s.id)" class="danger" title="Удалить">&#x2715;</button>
          </span>
        </div>
        <div v-else class="sub-edit">
          <input type="url" v-model="edit.url" class="sub-input" placeholder="URL">
          <div class="sub-edit-row">
            <input type="text" v-model="edit.name" placeholder="Название" class="sub-input">
            <input type="number" v-model.number="edit.interval" min="0" class="sub-input xs" title="Интервал (мин)" placeholder="мин">
            <label><input type="checkbox" v-model="edit.enabled"> Вкл</label>
            <button @click="editSave()" class="btn btn-primary btn-sm">Сохранить</button>
            <button @click="editCancel()" class="btn btn-sm">Отмена</button>
          </div>
        </div>
      </div>

      <!-- Empty state -->
      <div v-if="subs.length === 0" class="sub-empty">
        <p>Нет подписок</p>
        <p class="sub-empty-hint">Вставьте URL подписки в поле выше</p>
      </div>

      <!-- Profiles section -->
      <div class="sub-divider" v-if="profiles.length"></div>
      <div class="sub-row-label" v-if="profiles.length">
        Профили
        <button class="btn btn-sm" style="margin-left:auto" @click="openNewProfile()" :disabled="busy || profiles.length >= 10">+ Профиль</button>
      </div>
      <div v-for="p in profiles" :key="p.id" class="sub-card profile-card" :class="{ 'profile-default': p.is_default }">
        <div style="display:flex;align-items:center;gap:6px;min-width:0;flex:1">
          <span class="dot" :class="p.enabled ? 'on' : 'off'"></span>
          <strong style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">{{ p.name }}</strong>
          <span class="badge">{{ p.proxy_count }}/{{ p.total_proxy }}</span>
          <span class="strat-badge">{{ STRATS.find(s => s.v === p.strategy?.type)?.l || p.strategy?.type }}</span>
        </div>
        <div style="display:flex;gap:4px;flex-shrink:0">
          <button class="btn btn-sm" @click="editProfile(p)" :disabled="busy">Ред.</button>
          <button class="btn btn-sm" @click="removeProfile(p.id)" :disabled="busy || p.is_default" v-if="!p.is_default">Удалить</button>
        </div>
      </div>

      <!-- Filters for default profile (visible when proxies exist) -->
      <div class="sub-filters" v-if="proxies.length && dp">
        <div class="sub-divider"></div>

        <!-- Default profile strategy pills -->
        <div style="margin-bottom:6px">
          <div class="sub-row-label">Стратегия</div>
          <div class="strat-pills">
            <button v-for="s in STRATS" :key="s.v" class="spill" :class="{ active: strategy.type === s.v }"
                    @click="setStrategy(s.v)" :title="s.tip">{{ s.l }}</button>
          </div>
        </div>

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
          <input type="text" v-model="dp.filter.include_regex" @change="_persistDefault()" placeholder="Regex +" class="sub-input sm">
          <input type="text" v-model="dp.filter.exclude_regex" @change="_persistDefault()" placeholder="Regex −" class="sub-input sm">
          <input type="number" v-model.number="dp.filter.max_proxies" @change="_persistDefault()" min="0" class="sub-input xs"
                 title="Макс прокси (0 = без лимита)" placeholder="Лимит">
        </div>
      </div>
    </div>

    <!-- RIGHT: proxy list with virtual scroll -->
    <div class="sub-right">
      <template v-if="proxies.length">
        <div class="px-header">
          <input type="text" v-model="proxyQ" placeholder="Поиск…" class="sub-input">
          <span class="px-count">{{ filteredProxies.length }} / {{ proxies.length }}</span>
        </div>
        <div class="px-list" ref="scrollRef" @scroll="onScroll">
          <div :style="{ height: visibleProxies.total * ITEM_HEIGHT + 'px', position: 'relative' }">
            <div v-for="(p, idx) in visibleProxies.items" :key="p.tag"
                 class="px-row"
                 :style="{ position: 'absolute', top: (visibleProxies.start + idx) * ITEM_HEIGHT + 'px', left: 0, right: 0 }">
              <span class="px-country" :title="p.remarks">{{ p.country || '?' }}</span>
              <span class="px-marker" v-if="p.marker">{{ p.marker }}</span>
              <span class="px-remarks">{{ p.remarks || p.tag }}</span>
              <span class="px-tag mono">{{ p.tag }}</span>
            </div>
          </div>
        </div>
      </template>
      <div v-else class="sub-right-empty">
        <p v-if="subs.length === 0">Добавьте подписку и обновите её</p>
        <p v-else>Нажмите &#x21bb; на подписке для загрузки прокси</p>
      </div>
    </div>
  </div>

  <!-- Preview modal -->
  <div v-if="showPreview" class="modal-overlay" @click.self="showPreview = false">
    <div class="modal-box">
      <div class="modal-header">
        <h3>Предпросмотр</h3>
        <button @click="showPreview = false" class="btn btn-sm">Закрыть</button>
      </div>
      <div class="modal-body" v-if="previewData">
        <div class="preview-summary">
          <span>Прокси: <strong>{{ previewData.proxy_count }}</strong></span>
          <span v-if="previewData.profiles?.length">Профили: <strong>{{ previewData.profiles.length }}</strong></span>
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
        <button @click="applySubs()" :disabled="busy" class="btn btn-primary">Применить</button>
        <button @click="showPreview = false" class="btn">Закрыть</button>
      </div>
    </div>
  </div>

  <!-- Profile editor modal -->
  <div v-if="showProfileModal" class="modal-overlay" @click.self="showProfileModal = false">
    <div class="modal-box">
      <div class="modal-header">
        <h3>{{ pe.is_default ? 'Профиль по умолчанию' : (pe.id ? 'Редактировать профиль' : 'Новый профиль') }}</h3>
        <button @click="showProfileModal = false" class="btn btn-sm">Закрыть</button>
      </div>
      <div class="modal-body">
        <!-- Name -->
        <div class="pe-field" v-if="!pe.is_default">
          <label>Название</label>
          <input v-model="pe.name" placeholder="Название профиля" />
        </div>

        <!-- Strategy -->
        <div class="pe-field">
          <label>Стратегия</label>
          <div class="strat-pills">
            <button v-for="s in STRATS" :key="s.v" class="spill" :class="{ active: pe.strategy.type === s.v }"
                    @click="pe.strategy.type = s.v">{{ s.l }}</button>
          </div>
        </div>

        <!-- Filters -->
        <div class="pe-field">
          <label>Фильтры</label>
          <div class="pe-grid">
            <div>
              <span class="pe-label">Исключить маркеры</span>
              <input v-model="peStr.exclude_markers" placeholder="⚡, 🎮 через запятую" />
            </div>
            <div>
              <span class="pe-label">Включить маркеры</span>
              <input v-model="peStr.include_markers" placeholder="⚡, ⭐ через запятую" />
            </div>
            <div>
              <span class="pe-label">Исключить страны</span>
              <input v-model="peStr.exclude_countries" placeholder="DE, NL через запятую" />
            </div>
            <div>
              <span class="pe-label">Включить страны</span>
              <input v-model="peStr.include_countries" placeholder="US, UK через запятую" />
            </div>
            <div>
              <span class="pe-label">Regex включения</span>
              <input v-model="pe.filter.include_regex" placeholder="напр. Fast" />
            </div>
            <div>
              <span class="pe-label">Regex исключения</span>
              <input v-model="pe.filter.exclude_regex" placeholder="напр. LTE|Mobile" />
            </div>
            <div>
              <span class="pe-label">Макс прокси (0=все)</span>
              <input type="number" v-model.number="pe.filter.max_proxies" min="0" style="width:80px" />
            </div>
          </div>
        </div>
      </div>
      <div class="modal-footer">
        <button @click="saveProfile()" :disabled="busy" class="btn btn-primary">Сохранить</button>
        <button @click="showProfileModal = false" class="btn">Отмена</button>
      </div>
    </div>
  </div>
</div>
</template>
