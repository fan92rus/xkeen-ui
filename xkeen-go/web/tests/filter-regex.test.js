import { describe, it, expect, beforeEach } from 'vitest';

// Reproduce the filter logic from SubscriptionsTab.vue filteredProxies computed
// This is the EXACT current code (with AND bug)
function filterProxiesCurrent(proxies, filter) {
    let list = [...proxies];
    const f = filter;

    if (f.exclude_countries?.length) {
        const ex = new Set(f.exclude_countries.map(c => c.toUpperCase()));
        list = list.filter(p => !ex.has((p.country || '').toUpperCase()));
    }
    if (f.include_countries?.length) {
        const inc = new Set(f.include_countries.map(c => c.toUpperCase()));
        list = list.filter(p => !p.country || inc.has(p.country.toUpperCase()));
    }
    // BUG: sequential filter = AND logic
    if (f.include_regexes?.length) {
        for (const pattern of f.include_regexes) {
            if (!pattern) continue;
            try {
                const re = new RegExp(pattern, 'i');
                list = list.filter(p => re.test(p.remarks || ''));
            } catch { /* skip */ }
        }
    }
    if (f.exclude_regexes?.length) {
        for (const pattern of f.exclude_regexes) {
            if (!pattern) continue;
            try {
                const re = new RegExp(pattern, 'i');
                list = list.filter(p => !re.test(p.remarks || ''));
            } catch { /* skip */ }
        }
    }
    if (f.max_proxies > 0 && list.length > f.max_proxies) {
        list = list.slice(0, f.max_proxies);
    }
    return list;
}

// Fixed version: OR logic for include_regexes
function filterProxiesFixed(proxies, filter) {
    let list = [...proxies];
    const f = filter;

    if (f.exclude_countries?.length) {
        const ex = new Set(f.exclude_countries.map(c => c.toUpperCase()));
        list = list.filter(p => !ex.has((p.country || '').toUpperCase()));
    }
    if (f.include_countries?.length) {
        const inc = new Set(f.include_countries.map(c => c.toUpperCase()));
        list = list.filter(p => !p.country || inc.has(p.country.toUpperCase()));
    }
    // FIX: single pass, proxy passes if it matches ANY include regex
    if (f.include_regexes?.length) {
        const compiled = f.include_regexes
            .filter(p => p)
            .map(p => { try { return new RegExp(p, 'i'); } catch { return null; } })
            .filter(Boolean);
        if (compiled.length > 0) {
            list = list.filter(p =>
                compiled.some(re => re.test(p.remarks || ''))
            );
        }
    }
    if (f.exclude_regexes?.length) {
        for (const pattern of f.exclude_regexes) {
            if (!pattern) continue;
            try {
                const re = new RegExp(pattern, 'i');
                list = list.filter(p => !re.test(p.remarks || ''));
            } catch { /* skip */ }
        }
    }
    if (f.max_proxies > 0 && list.length > f.max_proxies) {
        list = list.slice(0, f.max_proxies);
    }
    return list;
}

function makeProxy(country, remarks, tag) {
    return { country, remarks, tag: tag || remarks.toLowerCase().replace(/\s+/g, '-'), protocol: 'vless' };
}

describe('Include Regex filter logic', () => {
    const proxies = [
        makeProxy('DE', 'Germany Fast Server'),
        makeProxy('NL', 'Netherlands Standard'),
        makeProxy('US', 'USA Premium Node'),
        makeProxy('JP', 'Japan Gaming'),
        makeProxy('DE', 'Germany Premium Server'),
    ];

    it('BUG REPRO: two include regexes work as AND (current code)', () => {
        // "Fast|Premium" matches: Germany Fast Server, USA Premium Node, Germany Premium Server
        // "Server" matches: Germany Fast Server, Germany Premium Server
        // AND: only those matching BOTH = Germany Fast Server, Germany Premium Server (2)
        // OR: those matching EITHER = Germany Fast Server, USA Premium Node, Germany Premium Server (3)

        const filter = {
            include_regexes: ['Fast|Premium', 'Server'],
        };

        const result = filterProxiesCurrent(proxies, filter);
        // Current code applies them sequentially (AND):
        // After "Fast|Premium": [Germany Fast Server, USA Premium Node, Germany Premium Server]
        // After "Server": [Germany Fast Server, Germany Premium Server] — USA Premium Node lost!
        expect(result.length).toBe(2); // BUG: should be 3
        expect(result.map(p => p.remarks)).toEqual([
            'Germany Fast Server',
            'Germany Premium Server',
        ]);
        // "USA Premium Node" is incorrectly filtered out because it doesn't match "Server"
    });

    it('FIX: two include regexes should work as OR', () => {
        const filter = {
            include_regexes: ['Fast|Premium', 'Server'],
        };

        const result = filterProxiesFixed(proxies, filter);
        expect(result.length).toBe(3); // OR: match ANY regex
        expect(result.map(p => p.remarks).sort()).toEqual([
            'Germany Fast Server',
            'Germany Premium Server',
            'USA Premium Node',
        ]);
    });

    it('single include regex works correctly in both versions', () => {
        const filter = {
            include_regexes: ['Fast|Premium'],
        };

        const current = filterProxiesCurrent(proxies, filter);
        const fixed = filterProxiesFixed(proxies, filter);

        expect(current.length).toBe(3);
        expect(fixed.length).toBe(3);
        expect(current.map(p => p.remarks).sort()).toEqual(fixed.map(p => p.remarks).sort());
    });

    it('BUG REPRO: three include regexes — only intersection passes', () => {
        const filter = {
            include_regexes: ['Fast', 'Premium', 'Server'],
        };

        const current = filterProxiesCurrent(proxies, filter);
        // After "Fast": [Germany Fast Server]
        // After "Premium": [] (nothing with Fast also has Premium)
        // After "Server": [] (still empty)
        expect(current.length).toBe(0); // BUG: nothing matches all three

        const fixed = filterProxiesFixed(proxies, filter);
        // OR: anything matching Fast OR Premium OR Server
        expect(fixed.length).toBe(3); // Germany Fast Server, USA Premium Node, Germany Premium Server
    });

    it('exclude regexes work correctly (AND = OR for exclusion)', () => {
        const filter = {
            exclude_regexes: ['Gaming', 'Standard'],
        };

        const current = filterProxiesCurrent(proxies, filter);
        const fixed = filterProxiesFixed(proxies, filter);

        // For exclusion, AND and OR give same result
        expect(current.length).toBe(3);
        expect(current.map(p => p.remarks).sort()).toEqual([
            'Germany Fast Server',
            'Germany Premium Server',
            'USA Premium Node',
        ]);
    });

    it('combined: include regex + exclude regex', () => {
        const filter = {
            include_regexes: ['Fast|Premium'],
            exclude_regexes: ['Server'],
        };

        const fixed = filterProxiesFixed(proxies, filter);
        // Include "Fast|Premium": Germany Fast Server, USA Premium Node, Germany Premium Server
        // Exclude "Server": remove Germany Fast Server, Germany Premium Server
        // Result: USA Premium Node
        expect(fixed.length).toBe(1);
        expect(fixed[0].remarks).toBe('USA Premium Node');
    });

    it('combined: countries + include regex OR', () => {
        const filter = {
            include_countries: ['DE', 'US'],
            include_regexes: ['Fast|Gaming'],
        };

        const fixed = filterProxiesFixed(proxies, filter);
        // Countries: DE, US → Germany Fast Server, USA Premium Node, Germany Premium Server
        // Include regex OR: Fast|Gaming → Germany Fast Server (matches "Fast")
        expect(fixed.length).toBe(1);
        expect(fixed[0].remarks).toBe('Germany Fast Server');
    });

    it('empty include_regexes passes all', () => {
        const filter = { include_regexes: [] };
        const fixed = filterProxiesFixed(proxies, filter);
        expect(fixed.length).toBe(5);
    });

    it('invalid regex is skipped', () => {
        const filter = {
            include_regexes: ['[invalid', 'Fast'],
        };
        const fixed = filterProxiesFixed(proxies, filter);
        // "[invalid" is skipped, "Fast" matches: Germany Fast Server
        expect(fixed.length).toBe(1);
        expect(fixed[0].remarks).toBe('Germany Fast Server');
    });
});
