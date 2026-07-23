// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
	parseEntry,
	normalizeRule,
	entryLabel,
	entryIcon,
	serializeRule,
	validateAction,
	clearPathCache,
	COMMON_GEOSITE,
	COMMON_GEOIP,
} from '../src/services/routing-rules.js';

// ── Mock config.js for getAvailableTags tests ──
vi.mock('../src/services/config.js', () => ({
	listFiles: vi.fn(),
	getFile: vi.fn(),
	saveFile: vi.fn(),
}));

import { listFiles, getFile, saveFile } from '../src/services/config.js';

const mockOutbounds = {
	outbounds: [
		{ tag: 'proxy', protocol: 'vmess' },
		{ tag: 'warp', protocol: 'wireguard' },
		{ tag: '', protocol: 'direct' }, // empty tag should be filtered
	],
};

const mockRouting = {
	routing: {
		rules: [],
		balancers: [{ tag: 'default' }, { tag: 'cdn' }],
	},
};

beforeEach(() => {
	vi.clearAllMocks();
	clearPathCache();
	listFiles.mockResolvedValue([
		{ name: '04_outbounds.json', path: '/cfg/04_outbounds.json' },
		{ name: '05_routing.json', path: '/cfg/05_routing.json' },
	]);
});

// ── validateAction ──

describe('validateAction', () => {
	it('returns null for built-in direct tag', () => {
		expect(validateAction({ tag: 'direct' }, ['proxy'])).toBeNull();
	});

	it('returns null for built-in block tag', () => {
		expect(validateAction({ tag: 'block' }, [])).toBeNull();
	});

	it('returns null for known outbound tag', () => {
		expect(validateAction({ tag: 'proxy' }, ['proxy', 'warp'])).toBeNull();
	});

	it('returns null for known balancer tag', () => {
		expect(validateAction({ tag: 'cdn' }, ['cdn'])).toBeNull();
	});

	it('returns error for null action', () => {
		expect(validateAction(null, [])).toBe('Action missing tag');
	});

	it('returns error for action without tag', () => {
		expect(validateAction({}, ['proxy'])).toBe('Action missing tag');
	});

	it('returns error for action with empty tag', () => {
		expect(validateAction({ tag: '' }, ['proxy'])).toBe('Action missing tag');
	});

	it('returns error for unknown tag', () => {
		const err = validateAction({ tag: 'nonexistent' }, ['proxy', 'warp']);
		expect(err).toContain('nonexistent');
		expect(err).toContain('not found');
	});

	it('returns error for tag not in available set', () => {
		expect(validateAction({ tag: 'proxy' }, [])).toContain('not found');
		expect(validateAction({ tag: 'proxy' }, ['other'])).toContain('not found');
	});

	it('built-in tags take priority even when in available set', () => {
		expect(validateAction({ tag: 'direct' }, [])).toBeNull();
		expect(validateAction({ tag: 'block' }, [])).toBeNull();
	});
});

// ── getAvailableTags ──

import { getAvailableTags } from '../src/services/routing-rules.js';

describe('getAvailableTags', () => {
	it('returns outbound + balancer + built-in tags', async () => {
		getFile.mockImplementation(async (path) => {
			if (path.includes('04_outbounds')) return { content: JSON.stringify(mockOutbounds) };
			if (path.includes('05_routing')) return { content: JSON.stringify(mockRouting) };
			throw new Error('unexpected path');
		});

		const result = await getAvailableTags();

		expect(result.outboundTags).toEqual(['proxy', 'warp']);
		expect(result.balancerTags).toEqual(['default', 'cdn']);
		expect(result.allTags).toEqual(['direct', 'block', 'proxy', 'warp', 'default', 'cdn']);
	});

	it('deduplicates tags across categories', async () => {
		const outbounds = { outbounds: [{ tag: 'proxy' }] };
		const routing = { routing: { rules: [], balancers: [{ tag: 'proxy' }] } };

		getFile.mockImplementation(async (path) => {
			if (path.includes('04_outbounds')) return { content: JSON.stringify(outbounds) };
			if (path.includes('05_routing')) return { content: JSON.stringify(routing) };
			throw new Error('unexpected path');
		});

		const result = await getAvailableTags();
		expect(result.allTags.filter(t => t === 'proxy')).toHaveLength(1);
	});

	it('returns empty arrays on missing outbounds file', async () => {
		getFile.mockImplementation(async (path) => {
			if (path.includes('04_outbounds')) throw new Error('not found');
			if (path.includes('05_routing')) return { content: JSON.stringify(mockRouting) };
			throw new Error('unexpected path');
		});

		const result = await getAvailableTags();
		expect(result.outboundTags).toEqual([]);
		expect(result.balancerTags).toEqual(['default', 'cdn']);
		expect(result.allTags).toContain('direct');
	});

	it('returns empty arrays on missing routing file', async () => {
		getFile.mockImplementation(async (path) => {
			if (path.includes('04_outbounds')) return { content: JSON.stringify(mockOutbounds) };
			if (path.includes('05_routing')) throw new Error('not found');
			throw new Error('unexpected path');
		});

		const result = await getAvailableTags();
		expect(result.outboundTags).toEqual(['proxy', 'warp']);
		expect(result.balancerTags).toEqual([]);
	});

	it('handles outbounds without outbounds key', async () => {
		getFile.mockImplementation(async (path) => {
			if (path.includes('04_outbounds')) return { content: '{}' };
			if (path.includes('05_routing')) return { content: JSON.stringify(mockRouting) };
			throw new Error('unexpected path');
		});

		const result = await getAvailableTags();
		expect(result.outboundTags).toEqual([]);
	});

	it('includes built-in direct and block in allTags', async () => {
		getFile.mockImplementation(async (path) => {
			if (path.includes('04_outbounds')) return { content: JSON.stringify({ outbounds: [] }) };
			if (path.includes('05_routing')) return { content: JSON.stringify({ routing: { rules: [] } }) };
			throw new Error('unexpected path');
		});

		const result = await getAvailableTags();
		expect(result.allTags).toContain('direct');
		expect(result.allTags).toContain('block');
	});

	it('calls listFiles once per unique path, then caches', async () => {
		getFile.mockResolvedValue({ content: '{}' });

		await getAvailableTags(); // 2 listFiles calls (04 + 05, both cold)
		await getAvailableTags(); // 0 (both cached)
		await getAvailableTags(); // 0

		expect(listFiles).toHaveBeenCalledTimes(2);
	});
});

// ── clearPathCache ──

describe('clearPathCache', () => {
	it('forces listFiles to be called on next getAvailableTags', async () => {
		getFile.mockResolvedValue({ content: '{}' });

		await getAvailableTags();
		// 2 listFiles calls so far
		clearPathCache();
		await getAvailableTags();

		expect(listFiles).toHaveBeenCalledTimes(4); // 2 + 2 after cache clear
	});
});

// ── saveRouting invalidates cache ──

describe('saveRouting invalidates cache', () => {
	it('re-resolves paths on next getRouting after save', async () => {
		getFile.mockResolvedValue({ content: '{}' });
		saveFile.mockResolvedValue({});

		// First call: populate cache — 2 listFiles (04 + 05)
		await getAvailableTags();
		expect(listFiles).toHaveBeenCalledTimes(2);

		// Save routing — uses cached path internally, then calls clearPathCache
		const { saveRouting } = await import('../src/services/routing-rules.js');
		await saveRouting({ rules: [] });
		// saveRouting calls resolvePath(05) which is cached → 0 listFiles
		expect(listFiles).toHaveBeenCalledTimes(2);

		// Next getAvailableTags — cache cleared, re-lists 04 + 05
		await getAvailableTags();
		expect(listFiles).toHaveBeenCalledTimes(4); // 2 + 2
	});
});

// ── Existing tests (unchanged) ──

describe('parseEntry', () => {
	it('parses ext:geosite_v2fly.dat:category-ru', () => {
		const result = parseEntry('ext:geosite_v2fly.dat:category-ru');
		expect(result).toEqual({
			type: 'ext',
			db: 'geosite_v2fly.dat',
			value: 'category-ru',
			raw: 'ext:geosite_v2fly.dat:category-ru',
		});
	});

	it('parses geosite:google', () => {
		const result = parseEntry('geosite:google');
		expect(result).toEqual({
			type: 'geosite',
			value: 'google',
			raw: 'geosite:google',
		});
	});

	it('parses geoip:ru', () => {
		const result = parseEntry('geoip:ru');
		expect(result).toEqual({
			type: 'geoip',
			value: 'ru',
			raw: 'geoip:ru',
		});
	});

	it('parses regexp:^.*\\.ru$', () => {
		const result = parseEntry('regexp:^.*\\.ru$');
		expect(result).toEqual({
			type: 'regexp',
			value: '^.*\\.ru$',
			raw: 'regexp:^.*\\.ru$',
		});
	});

	it('parses domain:example.com', () => {
		const result = parseEntry('domain:example.com');
		expect(result).toEqual({
			type: 'domain',
			value: 'example.com',
			raw: 'domain:example.com',
		});
	});

	it('parses full:www.example.com', () => {
		const result = parseEntry('full:www.example.com');
		expect(result).toEqual({
			type: 'full',
			value: 'www.example.com',
			raw: 'full:www.example.com',
		});
	});

	it('parses CIDR 192.168.1.0/24', () => {
		const result = parseEntry('192.168.1.0/24');
		expect(result).toEqual({
			type: 'cidr',
			value: '192.168.1.0/24',
			raw: '192.168.1.0/24',
		});
	});

	it('parses plain domain google.com', () => {
		const result = parseEntry('google.com');
		expect(result).toEqual({
			type: 'plain',
			value: 'google.com',
			raw: 'google.com',
		});
	});

	it('handles empty string', () => {
		const result = parseEntry('');
		expect(result).toEqual({
			type: 'plain',
			value: '',
			raw: '',
		});
	});

	it('parses ext:custom.dat:my-category', () => {
		const result = parseEntry('ext:custom.dat:my-category');
		expect(result).toEqual({
			type: 'ext',
			db: 'custom.dat',
			value: 'my-category',
			raw: 'ext:custom.dat:my-category',
		});
	});
});

describe('entryLabel', () => {
	it('ext', () => expect(entryLabel({ type: 'ext', db: 'db', value: 'v' })).toBe('ext:db:v'));
	it('geosite', () => expect(entryLabel({ type: 'geosite', value: 'v' })).toBe('geosite:v'));
	it('geoip', () => expect(entryLabel({ type: 'geoip', value: 'v' })).toBe('geoip:v'));
	it('regexp', () => expect(entryLabel({ type: 'regexp', value: 'v' })).toBe('/v/'));
	it('domain', () => expect(entryLabel({ type: 'domain', value: 'v' })).toBe('*.v'));
	it('full', () => expect(entryLabel({ type: 'full', value: 'v' })).toBe('v'));
	it('cidr', () => expect(entryLabel({ type: 'cidr', value: 'v' })).toBe('v'));
	it('plain', () => expect(entryLabel({ type: 'plain', value: 'v' })).toBe('v'));
});

describe('entryIcon', () => {
	it('ext', () => expect(entryIcon({ type: 'ext' })).toBe('📁'));
	it('geosite', () => expect(entryIcon({ type: 'geosite' })).toBe('📁'));
	it('geoip', () => expect(entryIcon({ type: 'geoip' })).toBe('🌍'));
	it('regexp', () => expect(entryIcon({ type: 'regexp' })).toBe('⚙️'));
	it('cidr', () => expect(entryIcon({ type: 'cidr' })).toBe('🔢'));
	it('plain', () => expect(entryIcon({ type: 'plain' })).toBe('🌐'));
});

describe('normalizeRule', () => {
	it('parses outboundTag rule', () => {
		const rule = { outboundTag: 'proxy', domain: ['google.com'], port: '443' };
		const result = normalizeRule(rule, 0);
		expect(result.action).toEqual({ kind: 'outbound', tag: 'proxy' });
		expect(result.domains).toHaveLength(1);
		expect(result.ips).toHaveLength(0);
		expect(result.port).toBe('443');
		expect(result.id).toBe('rule-0');
	});

	it('parses balancerTag rule', () => {
		const rule = { balancerTag: 'default-balancer', ip: ['1.2.3.4'] };
		const result = normalizeRule(rule, 5);
		expect(result.action).toEqual({ kind: 'balancer', tag: 'default-balancer' });
		expect(result.ips).toHaveLength(1);
		expect(result.id).toBe('rule-5');
	});

	it('defaults to direct when no tag', () => {
		const rule = { domain: ['example.com'] };
		const result = normalizeRule(rule, 0);
		expect(result.action).toEqual({ kind: 'outbound', tag: 'direct' });
	});

	it('parses domain array', () => {
		const rule = { domain: ['google.com', 'geosite:youtube'], outboundTag: 'proxy' };
		const result = normalizeRule(rule, 0);
		expect(result.domains).toHaveLength(2);
		expect(result.domains[0].type).toBe('plain');
		expect(result.domains[1].type).toBe('geosite');
	});

	it('parses ip array', () => {
		const rule = { ip: ['10.0.0.0/8', '192.168.1.1'], outboundTag: 'direct' };
		const result = normalizeRule(rule, 0);
		expect(result.ips).toHaveLength(2);
		expect(result.ips[0].type).toBe('cidr');
		expect(result.ips[1].type).toBe('plain');
	});

	it('splits network tcp,udp', () => {
		const rule = { network: 'tcp,udp', outboundTag: 'proxy' };
		const result = normalizeRule(rule, 0);
		expect(result.networks).toEqual(['tcp', 'udp']);
	});

	it('splits network with spaces', () => {
		const rule = { network: 'tcp, udp', outboundTag: 'proxy' };
		const result = normalizeRule(rule, 0);
		expect(result.networks).toEqual(['tcp', 'udp']);
	});

	it('preserves port', () => {
		const rule = { outboundTag: 'proxy', port: '8443' };
		const result = normalizeRule(rule, 0);
		expect(result.port).toBe('8443');
	});

	it('preserves inboundTag', () => {
		const rule = { outboundTag: 'proxy', inboundTag: ['socks-in'] };
		const result = normalizeRule(rule, 0);
		expect(result.inbound).toEqual(['socks-in']);
	});

	it('formats id as rule-{index}', () => {
		const rule = { outboundTag: 'proxy' };
		expect(normalizeRule(rule, 3).id).toBe('rule-3');
		expect(normalizeRule(rule, 99).id).toBe('rule-99');
	});
});

describe('serializeRule', () => {
	it('serializes a regular outbound rule', () => {
		const rule = {
			id: 'rule-0',
			name: 'Test',
			domains: [parseEntry('google.com')],
			ips: [],
			networks: [],
			port: '',
			inbound: [],
			action: { kind: 'outbound', tag: 'proxy' },
			raw: {},
		};
		const result = serializeRule(rule);
		expect(result.domain).toEqual(['google.com']);
		expect(result.outboundTag).toBe('proxy');
		expect(result.balancerTag).toBeUndefined();
	});

	it('serializes a balancer rule', () => {
		const rule = {
			id: 'rule-1',
			name: 'Test',
			domains: [],
			ips: [parseEntry('1.1.1.0/24')],
			networks: ['tcp'],
			port: '443',
			inbound: [],
			action: { kind: 'balancer', tag: 'my-balancer' },
			raw: {},
		};
		const result = serializeRule(rule);
		expect(result.ip).toEqual(['1.1.1.0/24']);
		expect(result.network).toBe('tcp');
		expect(result.port).toBe('443');
		expect(result.balancerTag).toBe('my-balancer');
		expect(result.outboundTag).toBeUndefined();
	});

	it('preserves extra fields from raw, removes type', () => {
		const rule = {
			id: 'rule-0',
			name: 'Test',
			domains: [],
			ips: [],
			networks: [],
			port: '',
			inbound: [],
			action: { kind: 'outbound', tag: 'direct' },
			raw: { type: 'field', protocol: 'http', userLevel: 0, routeOnly: true },
		};
		const result = serializeRule(rule);
		expect(result.protocol).toBe('http');
		expect(result.userLevel).toBe(0);
		expect(result.routeOnly).toBe(true);
		expect(result.outboundTag).toBe('direct');
		expect(result.type).toBeUndefined();
	});

	it('omits empty arrays/port', () => {
		const rule = {
			id: 'rule-0',
			name: 'Test',
			domains: [parseEntry('x.com')],
			ips: [],
			networks: [],
			port: '',
			inbound: [],
			action: { kind: 'outbound', tag: 'direct' },
			raw: {},
		};
		const result = serializeRule(rule);
		expect(result.domain).toEqual(['x.com']);
		expect(result.ip).toBeUndefined();
		expect(result.network).toBeUndefined();
		expect(result.port).toBeUndefined();
	});

	it('preserves inboundTag from inbound array', () => {
		const rule = {
			id: 'rule-0',
			name: 'Test',
			domains: [],
			ips: [],
			networks: [],
			port: '',
			inbound: ['socks-in', 'http-in'],
			action: { kind: 'outbound', tag: 'proxy' },
			raw: {},
		};
		const result = serializeRule(rule);
		expect(result.inboundTag).toEqual(['socks-in', 'http-in']);
	});
});

describe('hardcoded category lists', () => {
	it('COMMON_GEOSITE has entries', () => {
		expect(COMMON_GEOSITE.length).toBeGreaterThan(0);
		for (const c of COMMON_GEOSITE) {
			expect(c).toHaveProperty('value');
			expect(c).toHaveProperty('label');
		}
	});

	it('COMMON_GEOIP has entries', () => {
		expect(COMMON_GEOIP.length).toBeGreaterThan(0);
		for (const c of COMMON_GEOIP) {
			expect(c).toHaveProperty('value');
			expect(c).toHaveProperty('label');
		}
	});
});
