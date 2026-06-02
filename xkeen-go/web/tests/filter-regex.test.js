import { describe, it, expect } from 'vitest';
import { filterProxies } from '../src/services/filter.js';

function makeProxy(country, remarks, tag, protocol) {
    return {
        country,
        remarks,
        tag: tag || remarks.toLowerCase().replace(/\s+/g, '-'),
        protocol: protocol || 'vless',
    };
}

// ─── Test data ───

const ALL = [
    makeProxy('DE', 'Germany Fast Server'),
    makeProxy('NL', 'Netherlands Standard'),
    makeProxy('US', 'USA Premium Node'),
    makeProxy('JP', 'Japan Gaming'),
    makeProxy('DE', 'Germany Premium Server'),
    makeProxy('RU', 'Russia LTE Moscow'),
    makeProxy('', 'Unknown Location Proxy'),    // no country
    makeProxy('FI', 'Finland 🎮 Gaming'),       // emoji
];

// ═══════════════════════════════════════════
// 1. NO FILTER / NULL FILTER
// ═══════════════════════════════════════════

describe('No filter / null filter', () => {
    it('null filter returns all proxies', () => {
        expect(filterProxies(ALL, null)).toHaveLength(ALL.length);
    });

    it('undefined filter returns all proxies', () => {
        expect(filterProxies(ALL, undefined)).toHaveLength(ALL.length);
    });

    it('empty filter object returns all proxies', () => {
        expect(filterProxies(ALL, {})).toHaveLength(ALL.length);
    });

    it('empty arrays in filter return all proxies', () => {
        expect(filterProxies(ALL, {
            exclude_countries: [],
            include_countries: [],
            include_regexes: [],
            exclude_regexes: [],
            max_proxies: 0,
        })).toHaveLength(ALL.length);
    });

    it('empty input returns empty output', () => {
        expect(filterProxies([], { include_regexes: ['test'] })).toHaveLength(0);
    });
});

// ═══════════════════════════════════════════
// 2. COUNTRY FILTERS
// ═══════════════════════════════════════════

describe('Exclude countries', () => {
    it('exclude single country', () => {
        const r = filterProxies(ALL, { exclude_countries: ['DE'] });
        expect(r.every(p => p.country !== 'DE')).toBe(true);
        expect(r).toHaveLength(ALL.length - 2); // 2 DE proxies removed
    });

    it('exclude multiple countries', () => {
        const r = filterProxies(ALL, { exclude_countries: ['DE', 'JP'] });
        expect(r.every(p => p.country !== 'DE' && p.country !== 'JP')).toBe(true);
    });

    it('exclude is case-insensitive', () => {
        const r1 = filterProxies(ALL, { exclude_countries: ['de'] });
        const r2 = filterProxies(ALL, { exclude_countries: ['DE'] });
        expect(r1).toHaveLength(r2.length);
    });

    it('exclude does not remove proxies with empty country', () => {
        const r = filterProxies(ALL, { exclude_countries: ['DE'] });
        expect(r.some(p => p.remarks === 'Unknown Location Proxy')).toBe(true);
    });
});

describe('Include countries', () => {
    it('include single country', () => {
        const r = filterProxies(ALL, { include_countries: ['DE'] });
        // Empty country always passes include filter
        expect(r.every(p => !p.country || p.country === 'DE')).toBe(true);
        expect(r).toHaveLength(3); // 2 DE + 1 empty country
    });

    it('include multiple countries (OR)', () => {
        const r = filterProxies(ALL, { include_countries: ['DE', 'US'] });
        expect(r.every(p => !p.country || p.country === 'DE' || p.country === 'US')).toBe(true);
        expect(r).toHaveLength(4); // 2 DE + 1 US + 1 empty country
    });

    it('include is case-insensitive', () => {
        const r1 = filterProxies(ALL, { include_countries: ['de'] });
        const r2 = filterProxies(ALL, { include_countries: ['DE'] });
        expect(r1).toHaveLength(r2.length);
    });

    it('empty country always passes include filter', () => {
        const r = filterProxies(ALL, { include_countries: ['DE'] });
        expect(r.some(p => p.remarks === 'Unknown Location Proxy')).toBe(true);
    });
});

describe('Include + Exclude countries combined', () => {
    it('include DE+US, exclude DE', () => {
        const r = filterProxies(ALL, {
            include_countries: ['DE', 'US'],
            exclude_countries: ['DE'],
        });
        // exclude runs first, removes DE → then include keeps only US + empty country
        expect(r.every(p => !p.country || p.country === 'US')).toBe(true);
        expect(r).toHaveLength(2); // USA Premium Node + Unknown Location Proxy
    });
});

// ═══════════════════════════════════════════
// 3. INCLUDE REGEXES (OR logic)
// ═══════════════════════════════════════════

describe('Include regexes — OR logic', () => {
    it('single include regex', () => {
        const r = filterProxies(ALL, { include_regexes: ['Gaming'] });
        expect(r).toHaveLength(2); // Japan Gaming, Finland 🎮 Gaming
        expect(r.every(p => /gaming/i.test(p.remarks))).toBe(true);
    });

    it('two include regexes — OR (not AND)', () => {
        const r = filterProxies(ALL, { include_regexes: ['Fast', 'Premium'] });
        // Fast: Germany Fast Server
        // Premium: USA Premium Node, Germany Premium Server
        // OR union: 3
        expect(r).toHaveLength(3);
        expect(r.map(p => p.remarks).sort()).toEqual([
            'Germany Fast Server',
            'Germany Premium Server',
            'USA Premium Node',
        ]);
    });

    it('three include regexes — OR', () => {
        const r = filterProxies(ALL, { include_regexes: ['Fast', 'Gaming', 'LTE'] });
        // Fast: Germany Fast Server
        // Gaming: Japan Gaming, Finland 🎮 Gaming
        // LTE: Russia LTE Moscow
        expect(r).toHaveLength(4);
    });

    it('include regex is case-insensitive', () => {
        const r1 = filterProxies(ALL, { include_regexes: ['gaming'] });
        const r2 = filterProxies(ALL, { include_regexes: ['GAMING'] });
        expect(r1).toHaveLength(r2.length);
    });

    it('include regex with alternation (|)', () => {
        const r = filterProxies(ALL, { include_regexes: ['Fast|Premium'] });
        expect(r).toHaveLength(3);
    });

    it('include regex matching emoji', () => {
        const r = filterProxies(ALL, { include_regexes: ['🎮'] });
        expect(r).toHaveLength(1);
        expect(r[0].remarks).toBe('Finland 🎮 Gaming');
    });

    it('include regex with empty string pattern is ignored', () => {
        const r = filterProxies(ALL, { include_regexes: ['', 'Fast'] });
        expect(r).toHaveLength(1);
        expect(r[0].remarks).toBe('Germany Fast Server');
    });

    it('include regex with all invalid patterns passes all', () => {
        const r = filterProxies(ALL, { include_regexes: ['[invalid', '(?<name>...)'] });
        // All invalid → compiled array is empty → no filter applied → all pass
        expect(r).toHaveLength(ALL.length);
    });

    it('include regex with one valid among invalids', () => {
        const r = filterProxies(ALL, { include_regexes: ['[invalid', 'Fast', '(?<bad'] });
        expect(r).toHaveLength(1);
        expect(r[0].remarks).toBe('Germany Fast Server');
    });

    it('include regex that matches nothing returns empty', () => {
        const r = filterProxies(ALL, { include_regexes: ['ZZZZZnotfound'] });
        expect(r).toHaveLength(0);
    });
});

// ═══════════════════════════════════════════
// 4. EXCLUDE REGEXES
// ═══════════════════════════════════════════

describe('Exclude regexes', () => {
    it('single exclude regex', () => {
        const r = filterProxies(ALL, { exclude_regexes: ['Gaming'] });
        expect(r.every(p => !/gaming/i.test(p.remarks))).toBe(true);
        expect(r).toHaveLength(ALL.length - 2);
    });

    it('multiple exclude regexes — each removes independently', () => {
        const r = filterProxies(ALL, { exclude_regexes: ['Gaming', 'LTE'] });
        expect(r.every(p => !/gaming/i.test(p.remarks) && !/lte/i.test(p.remarks))).toBe(true);
    });

    it('exclude regex is case-insensitive', () => {
        const r1 = filterProxies(ALL, { exclude_regexes: ['gaming'] });
        const r2 = filterProxies(ALL, { exclude_regexes: ['GAMING'] });
        expect(r1).toHaveLength(r2.length);
    });

    it('exclude regex with empty string is skipped', () => {
        const r = filterProxies(ALL, { exclude_regexes: ['', 'Gaming'] });
        expect(r).toHaveLength(ALL.length - 2);
    });

    it('exclude regex with invalid pattern is skipped', () => {
        const r = filterProxies(ALL, { exclude_regexes: ['[invalid', 'Gaming'] });
        expect(r).toHaveLength(ALL.length - 2);
    });

    it('exclude regex that matches nothing returns all', () => {
        const r = filterProxies(ALL, { exclude_regexes: ['ZZZZZnotfound'] });
        expect(r).toHaveLength(ALL.length);
    });
});

// ═══════════════════════════════════════════
// 5. INCLUDE + EXCLUDE REGEX INTERACTION
// ═══════════════════════════════════════════

describe('Include + Exclude regex interaction', () => {
    it('include matches, exclude removes subset', () => {
        // include: "Server" → Germany Fast Server, Germany Premium Server (only 2 match)
        // exclude: "Fast" → remove Germany Fast Server
        const r = filterProxies(ALL, { include_regexes: ['Server'], exclude_regexes: ['Fast'] });
        expect(r).toHaveLength(1);
        expect(r[0].remarks).toBe('Germany Premium Server');
    });

    it('proxy matching both include and exclude is removed', () => {
        const r = filterProxies(ALL, { include_regexes: ['Premium'], exclude_regexes: ['Server'] });
        // include Premium: USA Premium Node, Germany Premium Server
        // exclude Server: remove Germany Premium Server
        expect(r).toHaveLength(1);
        expect(r[0].remarks).toBe('USA Premium Node');
    });

    it('user scenario: include Hysteria2, exclude LTE', () => {
        const proxies = [
            makeProxy('DE', '🇩🇪 Hysteria2 Fast'),
            makeProxy('DE', '🇩🇪 Hysteria2 LTE Berlin'),
            makeProxy('FI', '🇫🇮 VLESS Standard'),
            makeProxy('JP', '🇯🇵 Hysteria2 LTE Tokyo'),
        ];
        const r = filterProxies(proxies, {
            include_regexes: ['Hysteria2'],
            exclude_regexes: ['LTE'],
        });
        expect(r).toHaveLength(1);
        expect(r[0].remarks).toBe('🇩🇪 Hysteria2 Fast');
    });

    it('multiple excludes narrow down include results', () => {
        const proxies = [
            makeProxy('DE', 'Premium Gaming Server'),
            makeProxy('US', 'Premium Streaming Server'),
            makeProxy('JP', 'Premium Gaming Node'),
            makeProxy('NL', 'Standard Server'),
        ];
        const r = filterProxies(proxies, {
            include_regexes: ['Premium'],
            exclude_regexes: ['Gaming', 'Streaming'],
        });
        // include Premium: first 3
        // exclude Gaming: remove Premium Gaming Server, Premium Gaming Node
        // exclude Streaming: remove Premium Streaming Server
        // result: empty
        expect(r).toHaveLength(0);
    });

    it('include OR + exclude still works correctly', () => {
        const proxies = [
            makeProxy('DE', 'Fast Server'),
            makeProxy('US', 'Premium Node'),
            makeProxy('JP', 'Fast Gaming'),
            makeProxy('NL', 'Standard'),
        ];
        const r = filterProxies(proxies, {
            include_regexes: ['Fast', 'Premium'],  // OR
            exclude_regexes: ['Gaming'],
        });
        // include OR Fast|Premium: Fast Server, Premium Node, Fast Gaming
        // exclude Gaming: remove Fast Gaming
        expect(r).toHaveLength(2);
        expect(r.map(p => p.remarks).sort()).toEqual(['Fast Server', 'Premium Node']);
    });

    it('exclude removes from include match even when remarks has both keywords', () => {
        const proxies = [
            makeProxy('DE', 'Premium LTE Server'),
            makeProxy('US', 'Premium Fast Server'),
        ];
        const r = filterProxies(proxies, {
            include_regexes: ['Premium'],
            exclude_regexes: ['LTE'],
        });
        expect(r).toHaveLength(1);
        expect(r[0].remarks).toBe('Premium Fast Server');
    });
});

// ═══════════════════════════════════════════
// 6. COUNTRIES + REGEX COMBINED
// ═══════════════════════════════════════════

describe('Countries + regex combined', () => {
    it('include countries + include regex (AND between types)', () => {
        // Countries DE: Germany Fast Server, Germany Premium Server
        // Regex "Premium": USA Premium Node, Germany Premium Server
        // AND: Germany Premium Server
        const r = filterProxies(ALL, {
            include_countries: ['DE'],
            include_regexes: ['Premium'],
        });
        expect(r).toHaveLength(1);
        expect(r[0].remarks).toBe('Germany Premium Server');
    });

    it('include countries + exclude regex', () => {
        // Countries DE: Germany Fast Server, Germany Premium Server + Unknown Location (empty country)
        // Exclude "Fast": remove Germany Fast Server
        const r = filterProxies(ALL, {
            include_countries: ['DE'],
            exclude_regexes: ['Fast'],
        });
        expect(r).toHaveLength(2); // Germany Premium Server + Unknown Location Proxy
        expect(r.every(p => p.remarks !== 'Germany Fast Server')).toBe(true);
    });

    it('exclude countries + include regex', () => {
        // Exclude DE: remove Germany Fast Server, Germany Premium Server
        // Include "Server": Netherlands Standard (has Server in name)
        const r = filterProxies(ALL, {
            exclude_countries: ['DE'],
            include_regexes: ['Server'],
        });
        expect(r.some(p => p.country === 'DE')).toBe(false);
        expect(r.every(p => /server/i.test(p.remarks))).toBe(true);
    });

    it('full combo: include countries + include regex + exclude regex + exclude countries', () => {
        const r = filterProxies(ALL, {
            include_countries: ['DE', 'US'],
            exclude_countries: ['RU'],
            include_regexes: ['Server'],
            exclude_regexes: ['Premium'],
        });
        // include countries DE+US: Germany Fast Server, USA Premium Node, Germany Premium Server, + Unknown
        // exclude countries RU: no change (already filtered)
        // include regex "Server": Germany Fast Server, Germany Premium Server
        // exclude regex "Premium": remove Germany Premium Server
        // Unknown Location Proxy: passes include countries (empty), but no "Server" in remarks
        expect(r).toHaveLength(1);
        expect(r[0].remarks).toBe('Germany Fast Server');
    });
});

// ═══════════════════════════════════════════
// 7. MAX PROXIES
// ═══════════════════════════════════════════

describe('Max proxies', () => {
    it('max_proxies truncates result', () => {
        const r = filterProxies(ALL, { max_proxies: 3 });
        expect(r).toHaveLength(3);
    });

    it('max_proxies=0 does not truncate', () => {
        const r = filterProxies(ALL, { max_proxies: 0 });
        expect(r).toHaveLength(ALL.length);
    });

    it('max_proxies larger than result does not truncate', () => {
        const r = filterProxies(ALL, { max_proxies: 100 });
        expect(r).toHaveLength(ALL.length);
    });

    it('max_proxies applies after all other filters', () => {
        const r = filterProxies(ALL, {
            include_regexes: ['Server'],   // 4 matches
            max_proxies: 2,
        });
        expect(r).toHaveLength(2);
    });
});

// ═══════════════════════════════════════════
// 8. EDGE CASES
// ═══════════════════════════════════════════

describe('Edge cases', () => {
    it('proxy with empty remarks', () => {
        const proxies = [
            makeProxy('DE', ''),
            makeProxy('US', 'Premium Node'),
        ];
        // Include regex "Premium" — empty remarks doesn't match → filtered out
        const r = filterProxies(proxies, { include_regexes: ['Premium'] });
        expect(r).toHaveLength(1);
        // Exclude regex "Premium" — empty remarks doesn't match → passes
        const r2 = filterProxies(proxies, { exclude_regexes: ['Premium'] });
        expect(r2).toHaveLength(1);
        expect(r2[0].country).toBe('DE');
    });

    it('proxy with undefined remarks', () => {
        const proxies = [
            { country: 'DE', remarks: undefined, tag: 'p1', protocol: 'vless' },
            { country: 'US', remarks: 'Premium', tag: 'p2', protocol: 'vless' },
        ];
        const r = filterProxies(proxies, { include_regexes: ['Premium'] });
        expect(r).toHaveLength(1);
    });

    it('proxy with undefined country passes country filters', () => {
        const proxies = [
            { country: undefined, remarks: 'Test', tag: 'p1', protocol: 'vless' },
        ];
        // Include country DE — undefined country passes (treated as empty)
        const r = filterProxies(proxies, { include_countries: ['DE'] });
        expect(r).toHaveLength(1);
        // Exclude country DE — undefined country not in exclude set
        const r2 = filterProxies(proxies, { exclude_countries: ['DE'] });
        expect(r2).toHaveLength(1);
    });

    it('regex with special characters', () => {
        const proxies = [
            makeProxy('DE', 'Server (v2.0)'),
            makeProxy('US', 'Server v2.0'),
        ];
        const r = filterProxies(proxies, { include_regexes: ['\\(v2\\.0\\)'] });
        expect(r).toHaveLength(1);
        expect(r[0].remarks).toBe('Server (v2.0)');
    });

    it('duplicate proxies in input preserved', () => {
        const proxies = [
            makeProxy('DE', 'Test'),
            makeProxy('DE', 'Test'),
        ];
        const r = filterProxies(proxies, {});
        expect(r).toHaveLength(2);
    });

    it('filter order: exclude_countries → include_countries → include_regex → exclude_regex → max', () => {
        // If order were wrong, results would differ
        const proxies = [
            makeProxy('DE', 'Fast Premium'),
            makeProxy('US', 'Fast'),
            makeProxy('RU', 'Premium'),
            makeProxy('DE', 'Standard'),
        ];
        const r = filterProxies(proxies, {
            exclude_countries: ['RU'],         // remove RU/Premium
            include_countries: ['DE', 'US'],   // keep DE+US (empty passes too but none here)
            include_regexes: ['Fast'],         // keep Fast Premium, Fast
            exclude_regexes: ['Premium'],      // remove Fast Premium
            max_proxies: 1,                    // truncate to 1
        });
        expect(r).toHaveLength(1);
        expect(r[0].remarks).toBe('Fast');
    });
});
