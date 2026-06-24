import { describe, it, expect } from 'vitest';
import {
    countByCountry,
    uniqueCountries,
    countryState,
    textFilterProxies,
} from '../src/utils/subscriptions-grouping.js';

/* ---- fixture ---- */
const proxies = [
    { tag: '🇺🇸 US-01', country: 'US', remarks: 'LA DC', protocol: 'shadowsocks', server: '1.2.3.4', port: 443 },
    { tag: '🇺🇸 US-02', country: 'US', remarks: 'NYC',    protocol: 'vless',       server: '5.6.7.8', port: 8443 },
    { tag: '🇩🇪 DE-01', country: 'DE', remarks: 'Frankfurt', protocol: 'vless', server: '9.10.11.12', port: 443 },
    { tag: '🇬🇧 GB-01', country: 'GB', remarks: 'London', protocol: 'shadowsocks', server: '13.14.15.16', port: 443 },
    { tag: '🇯🇵 JP-01', country: 'JP', remarks: 'Tokyo', protocol: 'trojan', server: '17.18.19.20', port: 443 },
    { tag: 'no-country', remarks: 'No origin', protocol: 'shadowsocks', server: '21.22.23.24', port: 80 },
];

/* ---- countByCountry ---- */

describe('countByCountry', () => {
    it('returns count for existing country', () => {
        expect(countByCountry(proxies, 'US')).toBe(2);
        expect(countByCountry(proxies, 'DE')).toBe(1);
        expect(countByCountry(proxies, 'GB')).toBe(1);
        expect(countByCountry(proxies, 'JP')).toBe(1);
    });

    it('returns 0 for country with no proxies', () => {
        expect(countByCountry(proxies, 'RU')).toBe(0);
        expect(countByCountry(proxies, '')).toBe(0);
    });

    it('returns 0 for empty proxy list', () => {
        expect(countByCountry([], 'US')).toBe(0);
    });

    it('handles proxies without country field', () => {
        const p = [{ tag: 'no-country' }, { tag: 'US-tag', country: 'US' }];
        expect(countByCountry(p, 'US')).toBe(1);
        expect(countByCountry(p, '')).toBe(0);
    });
});

/* ---- uniqueCountries ---- */

describe('uniqueCountries', () => {
    it('returns sorted unique countries', () => {
        expect(uniqueCountries(proxies)).toEqual(['DE', 'GB', 'JP', 'US']);
    });

    it('filters out falsy countries', () => {
        expect(uniqueCountries(proxies)).not.toContain('');
        expect(uniqueCountries(proxies)).not.toContain(undefined);
    });

    it('returns empty array for empty input', () => {
        expect(uniqueCountries([])).toEqual([]);
    });

    it('deduplicates when passes twice same country', () => {
        const p = [{ country: 'RU' }, { country: 'RU' }, { country: 'CN' }];
        expect(uniqueCountries(p)).toEqual(['CN', 'RU']);
    });

    it('handles all-falsy countries', () => {
        const p = [{ tag: 'a' }, { tag: 'b' }];
        expect(uniqueCountries(p)).toEqual([]);
    });
});

/* ---- countryState ---- */

const sampleFilters = {
    include_countries: ['US', 'DE'],
    exclude_countries: ['RU', 'CN'],
};

describe('countryState', () => {
    it('returns "in" for included country', () => {
        expect(countryState(sampleFilters, 'US')).toBe('in');
        expect(countryState(sampleFilters, 'DE')).toBe('in');
    });

    it('returns "ex" for excluded country', () => {
        expect(countryState(sampleFilters, 'RU')).toBe('ex');
        expect(countryState(sampleFilters, 'CN')).toBe('ex');
    });

    it('returns "off" for neutral country', () => {
        expect(countryState(sampleFilters, 'GB')).toBe('off');
        expect(countryState(sampleFilters, 'JP')).toBe('off');
    });

    it('returns "off" for null/undefined filters', () => {
        expect(countryState(null, 'US')).toBe('off');
        expect(countryState(undefined, 'US')).toBe('off');
    });

    it('returns "off" for empty filter arrays', () => {
        expect(countryState({ include_countries: [], exclude_countries: [] }, 'US')).toBe('off');
    });

    it('include takes precedence over exclude if both contain same country', () => {
        const f = { include_countries: ['US'], exclude_countries: ['US'] };
        expect(countryState(f, 'US')).toBe('in');
    });

    it('handles missing arrays (undefined)', () => {
        const f = { include_countries: undefined, exclude_countries: undefined };
        expect(countryState(f, 'US')).toBe('off');
    });
});

/* ---- textFilterProxies ---- */

describe('textFilterProxies', () => {
    it('filters by tag', () => {
        const r = textFilterProxies(proxies, 'US-01');
        expect(r).toHaveLength(1);
        expect(r[0].tag).toBe('🇺🇸 US-01');
    });

    it('filters by country code', () => {
        const r = textFilterProxies(proxies, 'de');
        expect(r).toHaveLength(1);
        expect(r[0].country).toBe('DE');
    });

    it('filters by remarks', () => {
        const r = textFilterProxies(proxies, 'frankfurt');
        expect(r).toHaveLength(1);
    });

    it('filters by protocol', () => {
        const r = textFilterProxies(proxies, 'vless');
        expect(r).toHaveLength(2); // US-02 (vless) + DE-01 (vless)
    });

    it('is case-insensitive', () => {
        const r = textFilterProxies(proxies, 'tokyo');
        expect(r).toHaveLength(1);
    });

    it('returns all proxies for empty query', () => {
        expect(textFilterProxies(proxies, '')).toHaveLength(6);
    });

    it('returns empty for non-matching query', () => {
        expect(textFilterProxies(proxies, 'zzz_nonexistent')).toHaveLength(0);
    });

    it('handles empty input list', () => {
        expect(textFilterProxies([], 'US')).toHaveLength(0);
    });

    it('handles nullish field values gracefully', () => {
        const p = [
            { tag: null, remarks: undefined, country: null, protocol: null },
            { tag: 'valid', remarks: '', country: '', protocol: '' },
        ];
        // The query doesn't match any empty/null fields lowercased
        expect(textFilterProxies(p, 'valid')).toHaveLength(1);
        expect(textFilterProxies(p, '')).toHaveLength(2);
    });
});
