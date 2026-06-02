/**
 * Proxy filtering logic for subscriptions.
 * Extracted from SubscriptionsTab.vue for testability.
 *
 * Filter order: exclude_countries → include_countries → include_regexes (OR) → exclude_regexes → max_proxies
 */

/**
 * Apply all filters to a proxy list.
 * @param {Array} proxies - list of proxy objects ({country, remarks, tag, protocol, ...})
 * @param {Object} filter - filter definition
 * @param {string[]} [filter.exclude_countries] - country codes to exclude
 * @param {string[]} [filter.include_countries] - country codes to include (empty country always passes)
 * @param {string[]} [filter.include_regexes] - regex patterns (OR logic, case-insensitive)
 * @param {string[]} [filter.exclude_regexes] - regex patterns to exclude (case-insensitive)
 * @param {number} [filter.max_proxies] - truncate result to this many
 * @returns {Array} filtered proxy list
 */
export function filterProxies(proxies, filter) {
    let list = [...proxies];
    const f = filter;
    if (!f) return list;

    // 1. Exclude countries
    if (f.exclude_countries?.length) {
        const ex = new Set(f.exclude_countries.map(c => c.toUpperCase()));
        list = list.filter(p => !ex.has((p.country || '').toUpperCase()));
    }

    // 2. Include countries (empty country passes through)
    if (f.include_countries?.length) {
        const inc = new Set(f.include_countries.map(c => c.toUpperCase()));
        list = list.filter(p => !p.country || inc.has(p.country.toUpperCase()));
    }

    // 3. Include regexes — OR logic (match ANY)
    if (f.include_regexes?.length) {
        const compiled = f.include_regexes
            .filter(p => p)
            .map(p => { try { return new RegExp(p, 'i'); } catch { return null; } })
            .filter(Boolean);
        if (compiled.length > 0) {
            list = list.filter(p => compiled.some(re => re.test(p.remarks || '')));
        }
    }

    // 4. Exclude regexes — each pattern removes matching proxies
    if (f.exclude_regexes?.length) {
        for (const pattern of f.exclude_regexes) {
            if (!pattern) continue;
            try {
                const re = new RegExp(pattern, 'i');
                list = list.filter(p => !re.test(p.remarks || ''));
            } catch { /* invalid regex — skip */ }
        }
    }

    // 5. Max proxies
    if (f.max_proxies > 0 && list.length > f.max_proxies) {
        list = list.slice(0, f.max_proxies);
    }

    return list;
}
