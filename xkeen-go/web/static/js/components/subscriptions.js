// components/subscriptions.js - Subscriptions tab

import * as sub from '../services/subscription.js';

const MARKERS = [
    { id: '⚡', label: 'Быстрые' },
    { id: '⭐', label: 'Стандарт' },
    { id: '🎮', label: 'Гейминг' },
    { id: '0.5X', label: 'Мобильные' },
    { id: '⬇️', label: 'Загрузка' },
    { id: '💎', label: 'Авто' }
];

const STRATEGIES = [
    { value: 'all', label: 'Все через первый', icon: '📌' },
    { value: 'random', label: 'Случайный', icon: '🎲' },
    { value: 'roundrobin', label: 'По очереди', icon: '🔄' },
    { value: 'leastping', label: 'Мин. пинг', icon: '🏓' },
    { value: 'leastload', label: 'Мин. нагрузка', icon: '📊' }
];

function subscriptionsComponent() {
    return {
        subscriptions: [],
        proxies: [],
        filters: { include_markers: [], exclude_markers: ['0.5X'], include_countries: [], exclude_countries: [], include_regex: '', exclude_regex: '', max_proxies: 50 },
        strategy: { type: 'all', fallback_tag: '' },
        preview: null,

        loading: false,
        fetching: false,
        applying: false,
        showPreview: false,

        // Add subscription inline
        showAdd: false,
        newSub: { name: '', url: '', interval: 5, enabled: true },

        // Edit
        editId: null,
        editData: {},

        // Country input
        newIncCountry: '',
        newExcCountry: '',

        // Sections open/close
        sections: { subs: true, filters: true, strategy: false, proxies: false },

        // Proxy search
        proxySearch: '',
        proxySort: '',

        async init() {
            await this.loadAll();
        },

        async loadAll() {
            this.loading = true;
            try {
                const [d, f, s] = await Promise.all([sub.listSubscriptions(), sub.getFilters(), sub.getStrategy()]);
                this.subscriptions = d.subscriptions || [];
                if (f.filters) Object.assign(this.filters, f.filters);
                if (s.strategy) this.strategy = s.strategy;
            } catch (e) {
                console.error(e);
            } finally {
                this.loading = false;
            }
        },

        // === Subscriptions ===

        async addSub() {
            if (!this.newSub.url) return;
            try {
                await sub.addSubscription({ name: this.newSub.name || 'Новая подписка', url: this.newSub.url, interval: this.newSub.interval || 0, enabled: this.newSub.enabled });
                this.newSub = { name: '', url: '', interval: 5, enabled: true };
                this.showAdd = false;
                await this._reloadSubs();
            } catch (e) { this._toast('Ошибка: ' + e.message, 'error'); }
        },

        editStart(s) { this.editId = s.id; this.editData = { ...s }; },
        editCancel() { this.editId = null; this.editData = {}; },

        async editSave() {
            try {
                await sub.updateSubscription(this.editId, { name: this.editData.name, url: this.editData.url, interval: this.editData.interval, enabled: this.editData.enabled });
                this.editId = null;
                await this._reloadSubs();
            } catch (e) { this._toast('Ошибка: ' + e.message, 'error'); }
        },

        async removeSub(id) {
            if (!confirm('Удалить подписку?')) return;
            try { await sub.deleteSubscription(id); await this._reloadSubs(); } catch (e) { this._toast('Ошибка', 'error'); }
        },

        async fetchOne(id) {
            this.fetching = true;
            try {
                const d = await sub.fetchSubscription(id);
                await this._reloadSubs();
                this._toast(d.error ? ('Ошибка: ' + d.error) : (`+${d.count || 0} прокси`), d.error ? 'error' : 'success');
                if (this.proxies.length) await this._loadProxies();
            } finally { this.fetching = false; }
        },

        async fetchAll() {
            this.fetching = true;
            try {
                let total = 0;
                for (const s of this.subscriptions.filter(s => s.enabled)) {
                    try { const d = await sub.fetchSubscription(s.id); total += d.count || 0; } catch {}
                }
                await this._reloadSubs();
                this._toast(`Обновлено ${total} прокси`, 'success');
                if (this.proxies.length) await this._loadProxies();
            } finally { this.fetching = false; }
        },

        // === Filters ===

        toggleMarker(marker, list) {
            const arr = list === 'include' ? this.filters.include_markers : this.filters.exclude_markers;
            const idx = arr.indexOf(marker);
            if (idx >= 0) arr.splice(idx, 1);
            else arr.push(marker);
        },

        markerActive(marker, list) {
            return list === 'include' ? this.filters.include_markers.includes(marker) : this.filters.exclude_markers.includes(marker);
        },

        addCountry(list) {
            const key = list === 'include' ? 'newIncCountry' : 'newExcCountry';
            const arr = list === 'include' ? this.filters.include_countries : this.filters.exclude_countries;
            const v = this[key].trim();
            if (v && !arr.includes(v)) arr.push(v);
            this[key] = '';
        },

        removeCountry(list, c) {
            const arr = list === 'include' ? this.filters.include_countries : this.filters.exclude_countries;
            const idx = arr.indexOf(c);
            if (idx >= 0) arr.splice(idx, 1);
        },

        async saveFilters() {
            try { await sub.updateFilters(this.filters); this._toast('Фильтры сохранены', 'success'); } catch { this._toast('Ошибка', 'error'); }
        },

        // Unique countries from current proxies
        get proxyCountries() {
            const s = new Set(this.proxies.map(p => p.country).filter(Boolean));
            return [...s].sort();
        },

        // === Strategy ===

        async saveStrategy() {
            try { await sub.updateStrategy(this.strategy); this._toast('Стратегия сохранена', 'success'); } catch { this._toast('Ошибка', 'error'); }
        },

        strategyLabel(type) {
            return STRATEGIES.find(o => o.value === (type || this.strategy.type))?.label || type;
        },

        strategyIcon(type) {
            return STRATEGIES.find(o => o.value === (type || this.strategy.type))?.icon || '📌';
        },

        // === Proxies ===

        async loadProxies() {
            await this._loadProxies();
            if (this.proxies.length) this.sections.proxies = true;
        },

        async _loadProxies() {
            try { const d = await sub.getProxies(); this.proxies = d.proxies || []; } catch { this.proxies = []; }
        },

        get filteredProxies() {
            let list = this.proxies;
            if (this.proxySearch) {
                const q = this.proxySearch.toLowerCase();
                list = list.filter(p =>
                    (p.tag || '').toLowerCase().includes(q) ||
                    (p.remarks || '').toLowerCase().includes(q) ||
                    (p.country || '').toLowerCase().includes(q) ||
                    (p.protocol || '').toLowerCase().includes(q)
                );
            }
            return list;
        },

        // === Apply ===

        async previewApply() {
            this.loading = true;
            try { this.preview = await sub.previewSubscriptions(); this.showPreview = true; } catch (e) { this._toast('Ошибка: ' + e.message, 'error'); }
            finally { this.loading = false; }
        },

        async apply() {
            if (!confirm('Перезаписать outbounds, routing и перезапустить Xkeen?')) return;
            this.applying = true;
            try {
                const d = await sub.applySubscriptions();
                if (d.error) this._toast('Ошибка: ' + d.error, 'error');
                else { this._toast('Применено, Xkeen перезапускается', 'success'); this.showPreview = false; }
            } catch (e) { this._toast('Ошибка: ' + e.message, 'error'); }
            finally { this.applying = false; }
        },

        // === Helpers ===

        toggleSection(key) { this.sections[key] = !this.sections[key]; },

        fmtTime(t) {
            if (!t || t === '0001-01-01T00:00:00Z') return '';
            return new Date(t).toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
        },

        async _reloadSubs() {
            const d = await sub.listSubscriptions();
            this.subscriptions = d.subscriptions || [];
        },

        _toast(msg, type) { this.$store.app.showToast(msg, type); }
    };
}

document.addEventListener('alpine:init', () => {
    Alpine.data('subscriptions', subscriptionsComponent);
});

export { MARKERS, STRATEGIES };
