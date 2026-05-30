// components/subscriptions.js — minimal, intuitive

import * as api from '../services/subscription.js';

const STRATS = [
    { v: 'all', l: 'Первый' }, { v: 'random', l: 'Случайный' },
    { v: 'roundrobin', l: 'По очереди' }, { v: 'leastping', l: 'Мин. пинг' },
    { v: 'leastload', l: 'Мин. нагрузка' }
];

function subscriptions() {
    return {
        subs: [], proxies: [], preview: null,
        filters: { include_markers: [], exclude_markers: ['0.5X'], include_countries: [], exclude_countries: [], include_regex: '', exclude_regex: '', max_proxies: 50 },
        strategy: { type: 'all' },
        busy: false, editId: null, edit: {}, newUrl: '', proxyQ: '', showPreview: false,
        markers: [], strats: STRATS,

        async init() {
            try {
                const [d, f, s] = await Promise.all([api.listSubscriptions(), api.getFilters(), api.getStrategy()]);
                this.subs = d.subscriptions || [];
                if (f.filters) Object.assign(this.filters, f.filters);
                if (s.strategy) this.strategy = s.strategy;
            } catch {}
        },

        // -- Subscriptions --

        async add() {
            const url = this.newUrl.trim();
            if (!url) return;
            this.busy = true;
            try {
                await api.addSubscription({ name: '', url, interval: 0, enabled: true });
                this.newUrl = '';
                await this._reload();
            } catch (e) { this._err(e); } finally { this.busy = false; }
        },

        editStart(s) { this.editId = s.id; this.edit = { ...s }; },
        editCancel() { this.editId = null; },

        async editSave() {
            try {
                await api.updateSubscription(this.editId, { name: this.edit.name, url: this.edit.url, interval: this.edit.interval, enabled: this.edit.enabled });
                this.editId = null;
                await this._reload();
            } catch (e) { this._err(e); }
        },

        async remove(id) {
            if (!confirm('Удалить подписку?')) return;
            try { await api.deleteSubscription(id); await this._reload(); } catch (e) { this._err(e); }
        },

        async fetchOne(id) {
            this.busy = true;
            try {
                const d = await api.fetchSubscription(id);
                await this._reload();
                this._toast(d.error ? d.error : `+${d.count || 0} прокси`, d.error ? 'error' : 'success');
                await this._loadProxies();
            } finally { this.busy = false; }
        },

        async fetchAll() {
            this.busy = true;
            try {
                let n = 0;
                for (const s of this.subs.filter(x => x.enabled)) {
                    try { n += (await api.fetchSubscription(s.id)).count || 0; } catch {}
                }
                await this._reload();
                await this._loadProxies();
                this._toast(`Обновлено ${n} прокси`, 'success');
            } finally { this.busy = false; }
        },

        // -- Markers: click toggles exclude (2-state, simple) --

        markerExcl(id) { return this.filters.exclude_markers.includes(id); },

        toggleMarker(id) {
            const i = this.filters.exclude_markers.indexOf(id);
            if (i >= 0) this.filters.exclude_markers.splice(i, 1);
            else this.filters.exclude_markers.push(id);
        },

        // -- Countries --

        addCountry(field, el) {
            const v = el.value.trim();
            if (!v) return;
            const arr = field === 'in' ? this.filters.include_countries : this.filters.exclude_countries;
            if (!arr.includes(v)) arr.push(v);
            el.value = '';
        },

        removeCountry(field, c) {
            const arr = field === 'in' ? this.filters.include_countries : this.filters.exclude_countries;
            const i = arr.indexOf(c);
            if (i >= 0) arr.splice(i, 1);
        },

        // -- Proxies --

        async _loadProxies() {
            try {
                const d = await api.getProxies();
                this.proxies = d.proxies || [];
                if (d.markers) this.markers = d.markers;
            } catch { this.proxies = []; }
        },

        get filteredProxies() {
            if (!this.proxyQ) return this.proxies;
            const q = this.proxyQ.toLowerCase();
            return this.proxies.filter(p =>
                [p.tag, p.remarks, p.country, p.protocol].some(v => (v || '').toLowerCase().includes(q))
            );
        },

        // -- Preview & Apply --

        async _persist() {
            await Promise.all([api.updateFilters(this.filters), api.updateStrategy(this.strategy)]);
        },

        async preview() {
            this.busy = true;
            try {
                await this._persist();
                this.preview = await api.previewSubscriptions();
                this.showPreview = true;
            } catch (e) { this._err(e); } finally { this.busy = false; }
        },

        async apply() {
            if (!confirm('Применить и перезапустить Xkeen?')) return;
            this.busy = true;
            try {
                await this._persist();
                const d = await api.applySubscriptions();
                if (d.error) this._toast(d.error, 'error');
                else { this._toast('Применено, Xkeen перезапускается', 'success'); this.showPreview = false; }
            } catch (e) { this._err(e); } finally { this.busy = false; }
        },

        // -- Helpers --

        fmtTime(t) {
            if (!t || t === '0001-01-01T00:00:00Z') return '';
            return new Date(t).toLocaleString('ru-RU', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
        },

        stratLabel(v) { return this.strats.find(s => s.v === (v || this.strategy.type))?.l || v || ''; },

        async _reload() { this.subs = (await api.listSubscriptions()).subscriptions || []; },
        _err(e) { this._toast(e.message || 'Ошибка', 'error'); },
        _toast(msg, type) { this.$store.app.showToast(msg, type); }
    };
}

document.addEventListener('alpine:init', () => Alpine.data('subscriptions', subscriptions));
