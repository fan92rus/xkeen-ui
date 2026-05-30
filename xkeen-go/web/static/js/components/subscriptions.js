// components/subscriptions.js - Subscriptions tab for managing proxy subscriptions

import * as sub from '../services/subscription.js';

const KNOWN_MARKERS = ['⚡', '⭐', '🎮', '0.5X', '⬇️', '💎'];
const MARKER_LABELS = {
    '⚡': 'Быстрые',
    '⭐': 'Стандарт',
    '🎮': 'Гейминг',
    '0.5X': 'Мобильные',
    '⬇️': 'Загрузка',
    '💎': 'Авто'
};

const STRATEGY_OPTIONS = [
    { value: 'all', label: 'Все через первый', desc: 'Все routing rules → proxy' },
    { value: 'random', label: 'Случайный', desc: 'Xray balancer: random' },
    { value: 'roundrobin', label: 'По очереди', desc: 'Xray balancer: round-robin' },
    { value: 'leastping', label: 'Мин. пинг', desc: 'Xray balancer: leastPing (нужен Observatory)' },
    { value: 'leastload', label: 'Мин. нагрузка', desc: 'Xray balancer: leastLoad (нужен Observatory)' }
];

function subscriptionsComponent() {
    return {
        // State
        subscriptions: [],
        proxies: [],
        filteredProxies: [],
        filters: {
            include_markers: [],
            exclude_markers: ['0.5X'],
            include_countries: [],
            exclude_countries: [],
            include_regex: '',
            exclude_regex: '',
            max_proxies: 50
        },
        strategy: { type: 'all', fallback_tag: '' },
        preview: null,

        // UI state
        loading: false,
        fetching: false,
        applying: false,
        showPreview: false,
        showAddForm: false,

        // Add form
        newSub: { name: '', url: '', interval: 5, enabled: true },

        // Filter UI helpers
        newIncludeCountry: '',
        newExcludeCountry: '',
        newIncludeMarker: '',
        newExcludeMarker: '',
        countrySearch: '',

        // Constants exposed to template
        markerOptions: KNOWN_MARKERS,
        markerLabels: MARKER_LABELS,
        strategyOptions: STRATEGY_OPTIONS,

        // Edit state
        editingId: null,
        editSub: {},

        async init() {
            await this.loadAll();
        },

        async loadAll() {
            this.loading = true;
            try {
                await Promise.all([
                    this.loadSubscriptions(),
                    this.loadFilters(),
                    this.loadStrategy()
                ]);
            } finally {
                this.loading = false;
            }
        },

        async loadSubscriptions() {
            try {
                const data = await sub.listSubscriptions();
                this.subscriptions = data.subscriptions || [];
            } catch (e) {
                this.showToast('Ошибка загрузки подписок', 'error');
            }
        },

        async loadFilters() {
            try {
                const data = await sub.getFilters();
                if (data.filters) {
                    this.filters = { ...this.filters, ...data.filters };
                }
            } catch (e) {
                // Use defaults
            }
        },

        async loadStrategy() {
            try {
                const data = await sub.getStrategy();
                if (data.strategy) {
                    this.strategy = data.strategy;
                }
            } catch (e) {
                // Use defaults
            }
        },

        // === Subscription CRUD ===

        async addSubscription() {
            if (!this.newSub.url) {
                this.showToast('URL обязателен', 'error');
                return;
            }
            try {
                await sub.addSubscription({
                    name: this.newSub.name || 'Новая подписка',
                    url: this.newSub.url,
                    interval: this.newSub.interval || 0,
                    enabled: this.newSub.enabled
                });
                this.newSub = { name: '', url: '', interval: 5, enabled: true };
                this.showAddForm = false;
                await this.loadSubscriptions();
                this.showToast('Подписка добавлена', 'success');
            } catch (e) {
                this.showToast('Ошибка: ' + (e.message || 'не удалось добавить'), 'error');
            }
        },

        startEdit(s) {
            this.editingId = s.id;
            this.editSub = { ...s };
        },

        cancelEdit() {
            this.editingId = null;
            this.editSub = {};
        },

        async saveEdit() {
            try {
                await sub.updateSubscription(this.editingId, {
                    name: this.editSub.name,
                    url: this.editSub.url,
                    interval: this.editSub.interval,
                    enabled: this.editSub.enabled
                });
                this.editingId = null;
                this.editSub = {};
                await this.loadSubscriptions();
                this.showToast('Подписка обновлена', 'success');
            } catch (e) {
                this.showToast('Ошибка: ' + (e.message || 'не удалось обновить'), 'error');
            }
        },

        async deleteSubscription(id) {
            if (!confirm('Удалить подписку?')) return;
            try {
                await sub.deleteSubscription(id);
                await this.loadSubscriptions();
                this.showToast('Подписка удалена', 'success');
            } catch (e) {
                this.showToast('Ошибка удаления', 'error');
            }
        },

        async fetchOne(id) {
            this.fetching = true;
            try {
                const data = await sub.fetchSubscription(id);
                await this.loadSubscriptions();
                await this.loadProxies();
                if (data.error) {
                    this.showToast('Ошибка: ' + data.error, 'error');
                } else {
                    this.showToast(`Получено ${data.count || 0} прокси`, 'success');
                }
            } catch (e) {
                this.showToast('Ошибка fetch: ' + (e.message || ''), 'error');
            } finally {
                this.fetching = false;
            }
        },

        async fetchAll() {
            this.fetching = true;
            try {
                let total = 0;
                for (const s of this.subscriptions.filter(s => s.enabled)) {
                    try {
                        const data = await sub.fetchSubscription(s.id);
                        total += data.count || 0;
                    } catch (e) {
                        console.warn('Failed to fetch', s.name, e);
                    }
                }
                await this.loadSubscriptions();
                await this.loadProxies();
                this.showToast(`Обновлено ${total} прокси из ${this.subscriptions.filter(s => s.enabled).length} подписок`, 'success');
            } finally {
                this.fetching = false;
            }
        },

        // === Proxies ===

        async loadProxies() {
            try {
                const data = await sub.getProxies();
                this.proxies = data.proxies || [];
                this.applyLocalFilter();
            } catch (e) {
                this.proxies = [];
                this.filteredProxies = [];
            }
        },

        applyLocalFilter() {
            this.filteredProxies = this.proxies;
        },

        // === Filters ===

        async saveFilters() {
            try {
                await sub.updateFilters(this.filters);
                this.showToast('Фильтры сохранены', 'success');
                await this.loadProxies();
            } catch (e) {
                this.showToast('Ошибка сохранения фильтров', 'error');
            }
        },

        addIncludeMarker(m) {
            if (m && !this.filters.include_markers.includes(m)) {
                this.filters.include_markers.push(m);
            }
            this.newIncludeMarker = '';
        },

        removeIncludeMarker(m) {
            this.filters.include_markers = this.filters.include_markers.filter(x => x !== m);
        },

        addExcludeMarker(m) {
            if (m && !this.filters.exclude_markers.includes(m)) {
                this.filters.exclude_markers.push(m);
            }
            this.newExcludeMarker = '';
        },

        removeExcludeMarker(m) {
            this.filters.exclude_markers = this.filters.exclude_markers.filter(x => x !== m);
        },

        addIncludeCountry() {
            const c = this.newIncludeCountry.trim();
            if (c && !this.filters.include_countries.includes(c)) {
                this.filters.include_countries.push(c);
            }
            this.newIncludeCountry = '';
        },

        removeIncludeCountry(c) {
            this.filters.include_countries = this.filters.include_countries.filter(x => x !== c);
        },

        addExcludeCountry() {
            const c = this.newExcludeCountry.trim();
            if (c && !this.filters.exclude_countries.includes(c)) {
                this.filters.exclude_countries.push(c);
            }
            this.newExcludeCountry = '';
        },

        removeExcludeCountry(c) {
            this.filters.exclude_countries = this.filters.exclude_countries.filter(x => x !== c);
        },

        // === Strategy ===

        async saveStrategy() {
            try {
                await sub.updateStrategy(this.strategy);
                this.showToast('Стратегия сохранена', 'success');
            } catch (e) {
                this.showToast('Ошибка сохранения стратегии', 'error');
            }
        },

        strategyLabel(type) {
            const opt = STRATEGY_OPTIONS.find(o => o.value === type);
            return opt ? opt.label : type;
        },

        // === Apply / Preview ===

        async previewApply() {
            this.loading = true;
            try {
                const data = await sub.previewSubscriptions();
                this.preview = data;
                this.showPreview = true;
            } catch (e) {
                this.showToast('Ошибка предпросмотра: ' + (e.message || ''), 'error');
            } finally {
                this.loading = false;
            }
        },

        async apply() {
            if (!confirm('Применить изменения? Будут перезаписаны 04_outbounds.json и 05_routing.json, затем перезапущен Xkeen.')) return;
            this.applying = true;
            try {
                const data = await sub.applySubscriptions();
                if (data.error) {
                    this.showToast('Ошибка: ' + data.error, 'error');
                } else {
                    this.showToast('Настройки применены, Xkeen перезапускается', 'success');
                    this.showPreview = false;
                    // Refresh service status
                    this.$store.app.fetchServiceStatus();
                }
            } catch (e) {
                this.showToast('Ошибка применения: ' + (e.message || ''), 'error');
            } finally {
                this.applying = false;
            }
        },

        closePreview() {
            this.showPreview = false;
        },

        // === Helpers ===

        formatTime(t) {
            if (!t || t === '0001-01-01T00:00:00Z') return '—';
            return new Date(t).toLocaleString();
        },

        showToast(msg, type) {
            this.$store.app.showToast(msg, type);
        },

        getProxyCount(sub) {
            return sub.proxy_count || 0;
        },

        totalProxies() {
            return this.proxies.length;
        },

        filteredCount() {
            return this.filteredProxies.length;
        }
    };
}

document.addEventListener('alpine:init', () => {
    Alpine.data('subscriptions', subscriptionsComponent);
});
