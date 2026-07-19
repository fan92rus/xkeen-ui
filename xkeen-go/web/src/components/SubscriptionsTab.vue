<script setup>
import { ref, reactive, computed, onMounted, watch, nextTick } from 'vue';
import { useAppStore } from '../stores/app.js';
import { useI18nStore } from '../stores/i18n.js';
import * as api from '../services/subscription.js';
import { fmtTime } from '../utils/format.js';
import { error as logError } from '../utils/logger.js';
import { filterProxies } from '../services/filter.js';
import { formatJson } from '../utils/json-format.js';
import { countByCountry as _countByCountry, countryState as _countryState, uniqueCountries, textFilterProxies } from '../utils/subscriptions-grouping.js';

const app = useAppStore();
const i18n = useI18nStore();

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

/* ---- active profile ---- */
const activeProfileId = ref(null);

const STRATS = [
    { v: 'all', l: i18n.t('subs.strategy_all'), tip: i18n.t('subs.strategy_all_desc') },
    { v: 'random', l: i18n.t('subs.strategy_random'), tip: i18n.t('subs.strategy_random_desc') },
    { v: 'roundrobin', l: i18n.t('subs.strategy_roundrobin'), tip: i18n.t('subs.strategy_roundrobin_desc') },
    { v: 'leastping', l: i18n.t('subs.strategy_lowlatency'), tip: i18n.t('subs.strategy_lowlatency_desc') },
    { v: 'leastload', l: i18n.t('subs.strategy_minload'), tip: i18n.t('subs.strategy_minload_desc') }
];

const FALLBACKS = [
    { v: '', l: i18n.t('subs.fallback_off'), tip: i18n.t('subs.fallback_off_desc') },
    { v: 'direct', l: i18n.t('subs.fallback_direct'), tip: i18n.t('subs.fallback_direct_desc') },
    { v: 'block', l: i18n.t('subs.fallback_block'), tip: i18n.t('subs.fallback_block_desc') }
];

/* ---- active profile computed ---- */
const activeProfile = computed(() => {
    if (!activeProfileId.value) return profiles.value.find(p => p.is_default) || null;
    return profiles.value.find(p => p.id === activeProfileId.value) || profiles.value.find(p => p.is_default) || null;
});

const dp = computed(() => profiles.value.find(p => p.is_default) || null);

const filters = computed(() => {
    const p = activeProfile.value;
    return p?.filter || {
        include_countries: [], exclude_countries: [],
        include_regexes: [], exclude_regexes: [], max_proxies: 0
    };
});

const strategy = computed(() => {
    const p = activeProfile.value;
    return p?.strategy || { type: 'all', replace_balancer_tag: false };
});

/* ---- new profile inline ---- */
const showNewProfileInput = ref(false);
const newProfileName = ref('');

/* ---- regex inline input state ---- */
const newIncRegex = ref('');
const showIncRegexInput = ref(false);
const newExcRegex = ref('');
const showExcRegexInput = ref(false);

async function addIncludeRegex() {
    const v = newIncRegex.value.trim();
    if (!v || !activeProfile.value) return;
    if (!activeProfile.value.filter.include_regexes) activeProfile.value.filter.include_regexes = [];
    activeProfile.value.filter.include_regexes.push(v);
    newIncRegex.value = '';
    showIncRegexInput.value = false;
    await _persistProfile();
    await loadProfiles();
}
async function removeIncludeRegex(i) {
    activeProfile.value?.filter.include_regexes?.splice(i, 1);
    await _persistProfile();
    await loadProfiles();
}
async function addExcludeRegex() {
    const v = newExcRegex.value.trim();
    if (!v || !activeProfile.value) return;
    if (!activeProfile.value.filter.exclude_regexes) activeProfile.value.filter.exclude_regexes = [];
    activeProfile.value.filter.exclude_regexes.push(v);
    newExcRegex.value = '';
    showExcRegexInput.value = false;
    await _persistProfile();
    await loadProfiles();
}
async function removeExcludeRegex(i) {
    activeProfile.value?.filter.exclude_regexes?.splice(i, 1);
    await _persistProfile();
    await loadProfiles();
}

/* ---- computed ---- */
const allCountries = computed(() => uniqueCountries(proxies.value));

const filteredProxies = computed(() => {
    let list = filterProxies(proxies.value, filters.value);
    const q = proxyQ.value.toLowerCase();
    if (q) {
        list = textFilterProxies(list, q);
    }
    return list;
});

/* ---- helpers ---- */
function _toast(msg, type) { app.showToast(msg, type); }
function _err(e) { logError('[sub]', e); _toast(e.message || i18n.t('subs.error_generic'), 'error'); }
async function _reload() {
    subs.value = (await api.listSubscriptions()).subscriptions || [];
}
function countByCountry(c) { return _countByCountry(proxies.value, c); }

/* ---- persist active profile ---- */
async function _persistProfile() {
    const p = activeProfile.value;
    if (!p) return;
    if (p.is_default) {
        await Promise.all([
            api.updateFilters(p.filter),
            api.updateStrategy(p.strategy)
        ]);
    } else {
        await api.updateProfile(p.id, p);
    }
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
        _toast(i18n.t('subs.added'), 'success');
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
    if (!confirm(i18n.t('subs.confirm_delete'))) return;
    try { await api.deleteSubscription(id); await _reload(); } catch (e) { _err(e); }
}

/* ---- fetch ---- */
async function fetchOne(id) {
    busy.value = true;
    try {
        const d = await api.fetchSubscription(id);
        // Reload ALL proxies from backend (now merged across subscriptions)
        await loadProxies();
        await _reload();
        const count = d.total || d.proxy_count || 0;
        _toast(i18n.t('subs.proxy_count', { n: count, total: proxies.value.length }), d.error ? 'error' : 'success');
    } catch (e) {
        _err(e);
        try {
            const cached = await api.getProxies();
            if (cached.proxies?.length) {
                proxies.value = cached.proxies;
            }
        } catch { /* ignore */ }
    } finally { busy.value = false; }
}
async function fetchAll() {
    busy.value = true;
    try {
        let n = 0, errors = 0;
        for (const s of subs.value.filter(x => x.enabled)) {
            try {
                const d = await api.fetchSubscription(s.id);
                n += d.total || 0;
            } catch { errors++; }
        }
        await _reload();
        await loadProxies();
        if (errors > 0) {
            _toast(i18n.t('subs.updated_with_errors', { n, total: proxies.value.length, errors }), 'error');
        } else {
            _toast(i18n.t('subs.updated_ok', { n, total: proxies.value.length }), 'success');
        }
    } catch (e) { _err(e); } finally { busy.value = false; }
}
async function loadProxies() {
    try {
        const d = await api.getProxies();
        proxies.value = d.proxies || [];
    } catch (e) {
        logError('[sub] loadProxies error:', e);
        proxies.value = [];
    }
}

/* ---- countries ---- */
function countryState(c) { return _countryState(filters.value, c); }
async function toggleCountry(c) {
    const p = activeProfile.value;
    if (!p) return;
    const f = p.filter;
    if (!f.include_countries) f.include_countries = [];
    if (!f.exclude_countries) f.exclude_countries = [];
    const ii = f.include_countries.indexOf(c);
    const ei = f.exclude_countries.indexOf(c);
    if (ii >= 0) { f.include_countries.splice(ii, 1); }
    else if (ei >= 0) { f.exclude_countries.splice(ei, 1); f.include_countries.push(c); }
    else { f.exclude_countries.push(c); }
    await _persistProfile();
    await loadProfiles();
}

/* ---- strategy ---- */
async function setStrategy(type) {
    const p = activeProfile.value;
    if (!p) return;
    p.strategy.type = type;
    await _persistProfile();
    await loadProfiles();
}

async function setFallback(v) {
    const p = activeProfile.value;
    if (!p) return;
    if (!p.strategy) p.strategy = { type: 'all' };
    p.strategy.fallback = v;
    await _persistProfile();
    await loadProfiles();
}

/* ---- preview & apply ---- */
async function preview() {
    busy.value = true;
    try {
        await _persistProfile();
        previewData.value = await api.previewSubscriptions();
        showPreview.value = true;
    } catch (e) { _err(e); } finally { busy.value = false; }
}
async function applySubs() {
    const target = app.currentMode === 'mihomo' ? 'Mihomo' : 'Xray';
    if (!confirm(i18n.t('subs.apply_restart', { target }))) return;
    busy.value = true;
    try {
        await _persistProfile();
        const opts = {};
        if (app.currentMode === 'mihomo') {
            opts.convertXrayRouting = confirm(i18n.t('subs.convert_routing'));
        }
        const d = await api.applySubscriptions(opts);
        if (d.error) _toast(d.error, 'error');
        else { _toast(i18n.t('subs.applied_restarting', { target }), 'success'); showPreview.value = false; }
    } catch (e) { _err(e); } finally { busy.value = false; }
}

/* ---- profiles ---- */
async function loadProfiles() {
    try {
        profiles.value = await api.listProfiles();
    } catch (e) {
        logError('[sub] loadProfiles:', e);
        _toast(i18n.t('subs.profiles_load_error'), 'error');
    }
}

function switchProfile(id) {
    activeProfileId.value = id;
}

function openNewProfile() {
    showNewProfileInput.value = true;
    newProfileName.value = '';
    nextTick(() => {
        const input = document.querySelector('.profile-tabs .ptab-new-input');
        if (input) input.focus();
    });
}

async function confirmNewProfile() {
    const name = newProfileName.value.trim();
    if (!name) {
        showNewProfileInput.value = false;
        return;
    }
    busy.value = true;
    try {
        await api.createProfile({
            name,
            enabled: true,
            strategy: { type: 'random', fallback: 'direct' }
        });
        showNewProfileInput.value = false;
        await loadProfiles();
        // Switch to the new profile
        const newP = profiles.value.find(p => p.name === name);
        if (newP) activeProfileId.value = newP.id;
        _toast(i18n.t('subs.profile_created'));
    } catch (e) { _err(e); } finally { busy.value = false; }
}

function cancelNewProfile() {
    showNewProfileInput.value = false;
    newProfileName.value = '';
}

async function removeProfile(id) {
    if (!confirm(i18n.t('subs.confirm_delete_profile'))) return;
    try {
        await api.deleteProfile(id);
        // If deleting active profile, switch to default
        if (activeProfileId.value === id) {
            const def = profiles.value.find(p => p.is_default);
            activeProfileId.value = def?.id || null;
        }
        await loadProfiles();
        _toast(i18n.t('subs.profile_deleted'));
    } catch (e) { _err(e); }
}

async function toggleProfileEnabled(p) {
    p.enabled = !p.enabled;
    if (!p.is_default) {
        await api.updateProfile(p.id, p);
    }
    await loadProfiles();
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
    } catch (e) { logError('[sub] init error:', e); }
});
</script>

<template>
  <div class="sub-wrapper">
    <!-- Toolbar -->
    <div class="sub-toolbar">
      <input
        v-model="newUrl" type="url" :placeholder="i18n.t('subs.url_placeholder')"
        :disabled="busy" @keydown.enter="add()"
      >
      <button :disabled="busy || !newUrl.trim()" class="btn btn-primary btn-sm" @click="add()">{{ i18n.t('subs.add_btn') }}</button>
      <div class="sub-sep" />
      <button :disabled="busy || !subs.length" class="btn btn-sm" @click="fetchAll()">{{ i18n.t('subs.refresh_all') }}</button>
      <div class="sub-sep" />
      <button :disabled="busy || !proxies.length" class="btn btn-sm" @click="preview()">{{ i18n.t('subs.preview') }}</button>
      <button :disabled="busy || !proxies.length" class="btn btn-primary btn-sm" @click="applySubs()">{{ i18n.t('subs.apply') }}</button>
    </div>

    <!-- Body: two columns -->
    <div class="sub-body">
      <!-- LEFT: subscriptions + profiles + filters -->
      <div class="sub-left">
        <!-- Subscription cards -->
        <div v-for="s in subs" :key="s.id" class="sub-card" :class="{ editing: editId === s.id, builtin: s.is_builtin }">
          <div v-if="editId !== s.id" class="sub-row">
            <span class="dot" :class="s.enabled ? 'on' : 'off'" />
            <span class="name">{{ s.name || i18n.t('subs.unnamed') }}</span>
            <span v-if="s.proxy_count" class="badge">{{ s.proxy_count }}</span>
            <span v-if="s.last_fetch && s.last_fetch !== '0001-01-01T00:00:00Z'" class="meta">{{ fmtTime(s.last_fetch) }}</span>
            <span v-if="s.last_error" class="err" :title="s.last_error">!</span>
            <span class="acts">
              <button :disabled="busy" :title="i18n.t('subs.refresh_btn')" @click="fetchOne(s.id)">&#x21bb;</button>
              <button v-if="!s.is_builtin" :title="i18n.t('subs.edit_btn')" @click="editStart(s)">&#x270e;</button>
              <button v-if="!s.is_builtin" class="danger" :title="i18n.t('subs.delete_btn')" @click="remove(s.id)">&#x2715;</button>
            </span>
          </div>
          <div v-else class="sub-edit">
            <input v-model="edit.url" type="url" class="sub-input" placeholder="URL">
            <div class="sub-edit-row">
              <input v-model="edit.name" type="text" :placeholder="i18n.t('subs.edit_name')" class="sub-input">
              <input v-model.number="edit.interval" type="number" min="0" class="sub-input xs" :title="i18n.t('subs.edit_interval')" :placeholder="i18n.t('subs.edit_interval_unit')">
              <label><input v-model="edit.enabled" type="checkbox"> {{ i18n.t('subs.edit_enabled') }}</label>
              <button class="btn btn-primary btn-sm" @click="editSave()">{{ i18n.t('subs.edit_save') }}</button>
              <button class="btn btn-sm" @click="editCancel()">{{ i18n.t('subs.edit_cancel') }}</button>
            </div>
          </div>
        </div>

        <!-- Empty state -->
        <div v-if="subs.length === 0" class="sub-empty">
          <p>{{ i18n.t('subs.no_subs') }}</p>
          <p class="sub-empty-hint">{{ i18n.t('subs.no_subs_hint') }}</p>
        </div>

        <!-- Profile Tabs -->
        <div class="sub-divider" />
        <div class="profile-tabs">
          <button
            v-for="p in profiles" :key="p.id"
            class="ptab" :class="{ active: activeProfile?.id === p.id }"
            @click="switchProfile(p.id)"
          >
            <span class="dot" :class="p.enabled ? 'on' : 'off'" @click.stop="toggleProfileEnabled(p)" />
            <span class="ptab-name">{{ p.name }}</span>
            <button v-if="!p.is_default" class="ptab-delete" :disabled="busy" :title="i18n.t('subs.delete_profile_btn')" @click.stop="removeProfile(p.id)">&times;</button>
          </button>
          <button
            v-if="!showNewProfileInput && profiles.length < 10"
            class="ptab ptab-add" :disabled="busy" :title="i18n.t('subs.new_profile')" @click="openNewProfile()"
          >
            +
          </button>
          <template v-if="showNewProfileInput">
            <input
              v-model="newProfileName"
              class="ptab-new-input new-profile-name"
              :placeholder="i18n.t('subs.profile_name_ph')"
              maxlength="30"
              @keydown.enter="confirmNewProfile()"
              @keydown.escape="cancelNewProfile()" @blur="cancelNewProfile()"
            >
          </template>
        </div>

        <!-- Filters for active profile -->
        <div v-if="activeProfile" class="sub-filters">
          <div class="sub-divider" />

          <!-- Strategy pills -->
          <div style="margin-bottom:6px">
            <div class="sub-row-label">{{ i18n.t('subs.strategy') }}</div>
            <div class="strat-pills">
              <button
                v-for="s in STRATS" :key="s.v" class="spill" :class="{ active: strategy.type === s.v }"
                :title="s.tip" @click="setStrategy(s.v)"
              >
                {{ s.l }}
              </button>
            </div>
          </div>

          <!-- Fallback pills -->
          <div style="margin-bottom:6px">
            <div class="sub-row-label">{{ i18n.t('subs.fallback') }}</div>
            <div class="strat-pills">
              <button
                v-for="f in FALLBACKS" :key="f.v || 'off'" class="spill"
                :class="{ active: (strategy.fallback || '') === f.v }"
                :title="f.tip"
                :disabled="strategy.type === 'all'"
                @click="setFallback(f.v)"
              >
                {{ f.l }}
              </button>
            </div>
          </div>

          <div class="px-stats">
            <span>{{ i18n.t('subs.total') }} <strong>{{ proxies.length }}</strong></span>
            <span>{{ i18n.t('subs.in_sample') }} <strong>{{ filteredProxies.length }}</strong></span>
            <span v-if="proxies.length - filteredProxies.length" class="px-stat-excl">
              {{ i18n.t('subs.excluded') }} <strong>{{ proxies.length - filteredProxies.length }}</strong>
            </span>
          </div>

          <!-- Countries -->
          <div v-if="allCountries.length">
            <div class="sub-row-label">{{ i18n.t('subs.countries') }}</div>
            <div class="country-cloud">
              <button
                v-for="c in allCountries" :key="c" class="cc"
                :class="'cc-' + countryState(c)" @click="toggleCountry(c)"
              >
                {{ c }} {{ countByCountry(c) }}
              </button>
            </div>
          </div>

          <!-- Regex include -->
          <div>
            <div class="sub-row-label">{{ i18n.t('subs.regex_include') }}</div>
            <div class="regex-pills">
              <span v-for="(r, i) in (activeProfile.filter.include_regexes || [])" :key="'inc'+i" class="rpill">
                {{ r }}
                <button class="rpill-del" :title="i18n.t('subs.remove_filter')" @click="removeIncludeRegex(i)">&times;</button>
              </span>
              <template v-if="showIncRegexInput">
                <input
                  v-model="newIncRegex" class="rpill-input"
                  placeholder="Pattern…" autofocus
                  @keydown.enter="addIncludeRegex()" @keydown.escape="showIncRegexInput = false"
                >
              </template>
              <button v-else class="rpill-add" @click="showIncRegexInput = true; newIncRegex = ''">+</button>
            </div>
          </div>

          <!-- Regex exclude -->
          <div>
            <div class="sub-row-label">{{ i18n.t('subs.regex_exclude') }}</div>
            <div class="regex-pills">
              <span v-for="(r, i) in (activeProfile.filter.exclude_regexes || [])" :key="'exc'+i" class="rpill rpill-excl">
                {{ r }}
                <button class="rpill-del" :title="i18n.t('subs.remove_filter')" @click="removeExcludeRegex(i)">&times;</button>
              </span>
              <template v-if="showExcRegexInput">
                <input
                  v-model="newExcRegex" class="rpill-input"
                  placeholder="Pattern…" autofocus
                  @keydown.enter="addExcludeRegex()" @keydown.escape="showExcRegexInput = false"
                >
              </template>
              <button v-else class="rpill-add" @click="showExcRegexInput = true; newExcRegex = ''">+</button>
            </div>
          </div>

          <!-- Max proxies -->
          <div class="sub-row-compact">
            <input
              v-model.number="activeProfile.filter.max_proxies" type="number" min="0" class="sub-input xs" :title="i18n.t('subs.max_proxies')"
              :placeholder="i18n.t('subs.limit')" @change="_persistProfile()"
            >
          </div>
        </div>
      </div>

      <!-- RIGHT: proxy list with virtual scroll -->
      <div class="sub-right">
        <template v-if="proxies.length">
          <div class="px-header">
            <input v-model="proxyQ" type="text" :placeholder="i18n.t('subs.search')" class="sub-input">
            <span class="px-count">{{ filteredProxies.length }} / {{ proxies.length }}</span>
          </div>
          <div ref="scrollRef" class="px-list" @scroll="onScroll">
            <div :style="{ height: visibleProxies.total * ITEM_HEIGHT + 'px', position: 'relative' }">
              <div
                v-for="(p, idx) in visibleProxies.items" :key="p.tag"
                class="px-row"
                :style="{ position: 'absolute', top: (visibleProxies.start + idx) * ITEM_HEIGHT + 'px', left: 0, right: 0 }"
              >
                <span class="px-country" :title="p.remarks">{{ p.country || '?' }}</span>
                <span class="px-remarks">{{ p.remarks || p.tag }}</span>
                <span class="px-tag mono">{{ p.tag }}</span>
              </div>
            </div>
          </div>
        </template>
        <div v-else class="sub-right-empty">
          <p v-if="subs.length === 0">{{ i18n.t('subs.hint_add_sub') }}</p>
          <p v-else>{{ i18n.t('subs.hint_refresh_sub') }}</p>
        </div>
      </div>
    </div>

    <!-- Preview modal -->
    <div v-if="showPreview" class="modal-overlay" @click.self="showPreview = false">
      <div class="modal-box">
        <div class="modal-header">
          <h3>{{ i18n.t('subs.preview_title') }}</h3>
          <button class="btn btn-sm" @click="showPreview = false">{{ i18n.t('subs.preview_close') }}</button>
        </div>
        <div v-if="previewData" class="modal-body">
          <div class="preview-summary">
            <span>{{ i18n.t('subs.preview_total') }} <strong>{{ previewData.proxy_count }}</strong></span>
            <span v-if="previewData.filtered_proxy_count !== undefined">{{ i18n.t('subs.preview_filtered') }} <strong>{{ previewData.filtered_proxy_count }}</strong></span>
            <span v-if="previewData.profiles?.length">{{ i18n.t('subs.preview_profiles') }} <strong>{{ previewData.profiles.length }}</strong></span>
          </div>
          <details>
            <summary>outbounds.json</summary>
            <pre class="preview-json" v-html="formatJson(previewData.outbounds)" />
          </details>
          <details>
            <summary>routing.json</summary>
            <pre class="preview-json" v-html="formatJson(previewData.routing)" />
          </details>
          <details v-if="previewData.observatory">
            <summary>observatory.json</summary>
            <pre class="preview-json" v-html="formatJson(previewData.observatory)" />
          </details>
        </div>
        <div class="modal-footer">
          <button :disabled="busy" class="btn btn-primary" @click="applySubs()">{{ i18n.t('subs.apply_btn') }}</button>
          <button class="btn" @click="showPreview = false">{{ i18n.t('subs.apply_close') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>
