/**
 * Proxy filtering logic for subscriptions.
 * Extracted from SubscriptionsTab.vue for testability.
 *
 * Filter chain (applied in order, AND logic between steps, OR within list):
 * countries → protocols → fingerprints → network → TLS → regexes → max_proxies
 */

/**
 * Apply all filters to a proxy list.
 * @param {Array} proxies - list of proxy objects ({country, remarks, tag, protocol, ...})
 * @param {Object} filter - filter definition
 * @param {string[]} [filter.exclude_countries] - country codes to exclude
 * @param {string[]} [filter.include_countries] - country codes to include (empty country always passes)
 * @param {string[]} [filter.exclude_protocols] - protocols to exclude
 * @param {string[]} [filter.include_protocols] - protocols to include
 * @param {string[]} [filter.exclude_fingerprints] - fingerprints to exclude
 * @param {string[]} [filter.include_fingerprints] - fingerprints to include (empty fingerprint blocked)
 * @param {string[]} [filter.exclude_network] - network types to exclude
 * @param {string[]} [filter.include_network] - network types to include
 * @param {string[]} [filter.exclude_tls] - TLS security to exclude
 * @param {string[]} [filter.include_tls] - TLS security to include
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

    // 3. Protocols
    if (f.exclude_protocols?.length) {
        const ex = new Set(f.exclude_protocols.map(v => v.toLowerCase()));
        list = list.filter(p => !ex.has((p.protocol || '').toLowerCase()));
    }
    if (f.include_protocols?.length) {
        const inc = new Set(f.include_protocols.map(v => v.toLowerCase()));
        list = list.filter(p => inc.has((p.protocol || '').toLowerCase()));
    }

    // 4. Fingerprints (empty fingerprint blocked when include filter is active)
    if (f.exclude_fingerprints?.length) {
        const ex = new Set(f.exclude_fingerprints.map(v => v.toLowerCase()));
        list = list.filter(p => !ex.has((p.fingerprint || '').toLowerCase()));
    }
    if (f.include_fingerprints?.length) {
        const inc = new Set(f.include_fingerprints.map(v => v.toLowerCase()));
        list = list.filter(p => p.fingerprint && inc.has(p.fingerprint.toLowerCase()));
    }

    // 5. Network
    if (f.exclude_network?.length) {
        const ex = new Set(f.exclude_network.map(v => v.toLowerCase()));
        list = list.filter(p => !ex.has((p.network || '').toLowerCase()));
    }
    if (f.include_network?.length) {
        const inc = new Set(f.include_network.map(v => v.toLowerCase()));
        list = list.filter(p => !p.network || inc.has(p.network.toLowerCase()));
    }

    // 6. TLS
    if (f.exclude_tls?.length) {
        const ex = new Set(f.exclude_tls.map(v => v.toLowerCase()));
        list = list.filter(p => !ex.has((p.tls_security || '').toLowerCase()));
    }
    if (f.include_tls?.length) {
        const inc = new Set(f.include_tls.map(v => v.toLowerCase()));
        list = list.filter(p => !p.tls_security || inc.has(p.tls_security.toLowerCase()));
    }

    // 7. Include regexes — OR logic (match ANY)
    if (f.include_regexes?.length) {
        const compiled = f.include_regexes
            .filter(p => p)
            .map(p => { try { return new RegExp(p, 'i'); } catch { return null; } })
            .filter(Boolean);
        if (compiled.length > 0) {
            list = list.filter(p => compiled.some(re => re.test(p.remarks || '')));
        }
    }

    // 8. Exclude regexes — each pattern removes matching proxies
    if (f.exclude_regexes?.length) {
        for (const pattern of f.exclude_regexes) {
            if (!pattern) continue;
            try {
                const re = new RegExp(pattern, 'i');
                list = list.filter(p => !re.test(p.remarks || ''));
            } catch { /* invalid regex — skip */ }
        }
    }

    // 9. Max proxies
    if (f.max_proxies > 0 && list.length > f.max_proxies) {
        list = list.slice(0, f.max_proxies);
    }

    return list;
}
