/**
 * Pure proxy grouping/filtering helpers extracted from SubscriptionsTab.vue.
 * Zero dependencies — operates on plain proxy objects and filter configs.
 */

/**
 * Count proxies belonging to a given country.
 * @param {{country?: string}[]} proxies
 * @param {string} country
 * @returns {number}
 */
export function countByCountry(proxies, country) {
    return proxies.filter(p => p.country === country).length;
}

/**
 * Collect unique, alphabetically-sorted country codes from a proxy list.
 * Falsy countries (null/undefined/empty) are excluded.
 * @param {{country?: string}[]} proxies
 * @returns {string[]}
 */
export function uniqueCountries(proxies) {
    const set = new Set(proxies.map(p => p.country).filter(Boolean));
    return [...set].sort();
}

/**
 * Determine the state of a country within a filter object.
 * Mirrors SubscriptionsTab.vue `countryState()` behaviour exactly.
 *
 * @param {object} filters
 * @param {string[]} [filters.include_countries]
 * @param {string[]} [filters.exclude_countries]
 * @param {string} country
 * @returns {'in'|'ex'|'off'}
 */
export function countryState(filters, country) {
    if (!filters) return 'off';
    if (filters.include_countries?.includes(country)) return 'in';
    if (filters.exclude_countries?.includes(country)) return 'ex';
    return 'off';
}

/**
 * Filter proxy list by text query across multiple string fields.
 * Matches SubscriptionsTab.vue text filter exactly (lowercased, multi-field).
 *
 * @param {{tag?:string; remarks?:string; country?:string; protocol?:string}[]} proxies
 * @param {string} query
 * @returns {typeof proxies}
 */
export function textFilterProxies(proxies, query) {
    const q = query.toLowerCase();
    return proxies.filter(p =>
        [p.tag, p.remarks, p.country, p.protocol].some(v => (v || '').toLowerCase().includes(q))
    );
}
