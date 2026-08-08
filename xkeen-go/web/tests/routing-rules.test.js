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
	loadDisabledRules,
	saveDisabledRules,
	filterRules,
	generateRuleId,
	validatePort,
	validateCidr,
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

	it('parses IPv6 CIDR 2001:db8::/32', () => {
		const result = parseEntry('2001:db8::/32');
		expect(result).toEqual({
			type: 'cidr',
			value: '2001:db8::/32',
			raw: '2001:db8::/32',
		});
	});

	it('parses IPv6 CIDR fe80::1/128', () => {
		const result = parseEntry('fe80::1/128');
		expect(result.type).toBe('cidr');
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

	it('preserves a user-provided name instead of guessing', () => {
		const rule = { outboundTag: 'direct', domain: ['google.com'], name: 'My custom label' };
		const result = normalizeRule(rule, 0);
		expect(result.name).toBe('My custom label');
	});

	it('falls back to a guessed name when no user name is present', () => {
		const rule = { outboundTag: 'direct', domain: ['google.com'] };
		const result = normalizeRule(rule, 0);
		expect(result.name).not.toBe('');
		expect(typeof result.name).toBe('string');
	});

	it('reads a disabled flag so rules can be toggled off in the UI', () => {
		const rule = { outboundTag: 'direct', domain: ['google.com'], disabled: true };
		const result = normalizeRule(rule, 0);
		expect(result.disabled).toBe(true);
	});

	it('defaults disabled to false when the flag is absent', () => {
		const rule = { outboundTag: 'direct', domain: ['google.com'] };
		const result = normalizeRule(rule, 0);
		expect(result.disabled).toBe(false);
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

	it('writes the user-edited name into the serialized output', () => {
		const rule = {
			id: 'rule-0',
			name: 'My renamed rule',
			domains: [parseEntry('google.com')],
			ips: [],
			networks: [],
			port: '',
			inbound: [],
			action: { kind: 'outbound', tag: 'proxy' },
			raw: { outboundTag: 'proxy' }, // raw has no name — emulates user rename
		};
		const result = serializeRule(rule);
		expect(result.name).toBe('My renamed rule');
	});

	it('returns null for a disabled rule so save can filter it out', () => {
		const rule = {
			id: 'rule-0',
			name: 'Off rule',
			disabled: true,
			domains: [parseEntry('google.com')],
			ips: [],
			networks: [],
			port: '',
			inbound: [],
			action: { kind: 'outbound', tag: 'direct' },
			raw: { outboundTag: 'direct' },
		};
		expect(serializeRule(rule)).toBeNull();
	});

	it('preserves extra fields from raw, sets type to field', () => {
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
		expect(result.type).toBe('field');
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

	it('always sets type to field even if missing from raw', () => {
		const rule = {
			id: 'rule-0',
			name: 'Test',
			domains: [parseEntry('x.com')],
			ips: [],
			networks: [],
			port: '',
			inbound: [],
			action: { kind: 'outbound', tag: 'direct' },
			raw: {}, // no type field
		};
		const result = serializeRule(rule);
		expect(result.type).toBe('field');
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

// ── Round-trip: normalizeRule → serializeRule preserves the config ──

describe('round-trip: normalize → serialize', () => {
	it('preserves a simple direct rule with geosite domain', () => {
		const raw = {
			type: 'field',
			domain: ['geosite:category-ads'],
			outboundTag: 'block',
		};
		const result = serializeRule(normalizeRule(raw, 0));
		expect(result).toEqual(raw);
	});

	it('preserves a rule with multiple domain types (ext, geosite, domain)', () => {
		const raw = {
			type: 'field',
			domain: ['ext:geosite.dat:google', 'geosite:youtube', 'domain:example.com'],
			outboundTag: 'proxy',
		};
		const result = serializeRule(normalizeRule(raw, 1));
		expect(result).toEqual(raw);
	});

	it('preserves IP rules (geoip, CIDR IPv4/IPv6)', () => {
		const raw = {
			type: 'field',
			ip: ['geoip:ru', '10.0.0.0/8', '2001:db8::/32'],
			outboundTag: 'direct',
		};
		const result = serializeRule(normalizeRule(raw, 2));
		expect(result).toEqual(raw);
	});

	it('preserves network field (tcp,udp)', () => {
		const raw = {
			type: 'field',
			network: 'tcp,udp',
			outboundTag: 'proxy',
		};
		const result = serializeRule(normalizeRule(raw, 0));
		expect(result).toEqual(raw);
	});

	it('preserves port and inboundTag fields', () => {
		const raw = {
			type: 'field',
			port: '443',
			inboundTag: ['socks-in'],
			outboundTag: 'proxy',
		};
		const result = serializeRule(normalizeRule(raw, 0));
		expect(result).toEqual(raw);
	});

	it('preserves balancerTag rules', () => {
		const raw = {
			type: 'field',
			domain: ['geosite:netflix'],
			balancerTag: 'cdn',
		};
		const result = serializeRule(normalizeRule(raw, 0));
		expect(result).toEqual(raw);
	});

	it('preserves unknown/extra Xray fields (protocol, routeOnly)', () => {
		const raw = {
			type: 'field',
			domain: ['geosite:category-ads'],
			protocol: ['bittorrent'],
			routeOnly: true,
			outboundTag: 'block',
		};
		const result = serializeRule(normalizeRule(raw, 0));
		expect(result).toEqual(raw);
	});

	it('preserves a full realistic routing rules array', () => {
		const fullConfig = [
			{
				type: 'field',
				domain: ['geosite:category-ads'],
				outboundTag: 'block',
			},
			{
				type: 'field',
				domain: ['geosite:ru', 'geosite:category-gov-ru'],
				ip: ['geoip:ru', 'geoip:private'],
				outboundTag: 'direct',
			},
			{
				type: 'field',
				domain: ['ext:geosite.dat:google', 'ext:geosite.dat:youtube'],
				network: 'tcp,udp',
				port: '443,80',
				outboundTag: 'proxy',
			},
			{
				type: 'field',
				balancerTag: 'cdn',
				domain: ['geosite:netflix'],
			},
			{
				type: 'field',
				outboundTag: 'direct', // catch-all
				inboundTag: ['socks-in', 'http-in'],
			},
		];

		const roundTripped = fullConfig.map((r, i) => serializeRule(normalizeRule(r, i)));
		expect(roundTripped).toEqual(fullConfig);
	});

	it('does not add empty arrays for missing optional fields', () => {
		const raw = {
			type: 'field',
			outboundTag: 'direct',
		};
		const result = serializeRule(normalizeRule(raw, 0));
		expect(result).toEqual(raw);
		expect(result.ip).toBeUndefined();
		expect(result.network).toBeUndefined();
		expect(result.port).toBeUndefined();
		expect(result.inboundTag).toBeUndefined();
	});

	it('is idempotent: double normalize → serialize produces the same result', () => {
		const raw = {
			type: 'field',
			domain: ['geosite:category-ads', 'domain:example.com'],
			ip: ['geoip:ru', '10.0.0.0/8'],
			network: 'tcp,udp',
			port: '443',
			outboundTag: 'proxy',
		};

		// First round-trip
		const firstPass = serializeRule(normalizeRule(raw, 0));
		// Second round-trip on the result
		const secondPass = serializeRule(normalizeRule(firstPass, 0));

		expect(secondPass).toEqual(firstPass);
		expect(firstPass).toEqual(raw);
	});
});

describe('disabled-rule persistence (localStorage)', () => {
	const KEY = '/path/to/05_routing.json';

	beforeEach(() => {
		localStorage.clear();
	});

	it('saveDisabledRules then loadDisabledRules round-trips the rules', () => {
		const rules = [
			{ outboundTag: 'proxy', domain: ['ads.com'], name: 'Ads off', disabled: true },
		];
		saveDisabledRules(KEY, rules);
		expect(loadDisabledRules(KEY)).toEqual(rules);
	});

	it('loadDisabledRules returns [] when nothing stored', () => {
		expect(loadDisabledRules(KEY)).toEqual([]);
	});

	it('loadDisabledRules returns [] on malformed JSON', () => {
		localStorage.setItem(`xkeen-routing-disabled:${KEY}`, '{not json');
		expect(loadDisabledRules(KEY)).toEqual([]);
	});

	it('keys disabled rules per routing-file path', () => {
		saveDisabledRules('/a.json', [{ outboundTag: 'direct', disabled: true }]);
		saveDisabledRules('/b.json', [{ outboundTag: 'proxy', disabled: true }]);
		expect(loadDisabledRules('/a.json')).toHaveLength(1);
		expect(loadDisabledRules('/b.json')).toHaveLength(1);
		expect(loadDisabledRules('/a.json')[0].outboundTag).toBe('direct');
		expect(loadDisabledRules('/b.json')[0].outboundTag).toBe('proxy');
	});
});

describe('filterRules', () => {
	const rules = [
		normalizeRule({ outboundTag: 'proxy', domain: ['geosite:netflix'], name: 'Netflix' }, 0),
		normalizeRule({ outboundTag: 'direct', domain: ['geosite:category-ru'], name: 'RU Direct' }, 1),
		normalizeRule({ outboundTag: 'block', ip: ['geoip:cn'], name: 'Block CN' }, 2),
	];

	it('returns all rules when the query is empty', () => {
		expect(filterRules(rules, '')).toHaveLength(3);
		expect(filterRules(rules, '   ')).toHaveLength(3);
	});

	it('matches by rule name (case-insensitive)', () => {
		expect(filterRules(rules, 'netflix')).toHaveLength(1);
		expect(filterRules(rules, 'NETFLIX')).toHaveLength(1);
	});

	it('matches by domain entry value', () => {
		expect(filterRules(rules, 'netflix')).toHaveLength(1);
	});

	it('matches by domain raw text', () => {
		expect(filterRules(rules, 'category-ru')).toHaveLength(1);
	});

	it('matches by IP entry value', () => {
		expect(filterRules(rules, 'cn')).toHaveLength(1);
	});

	it('matches by action tag', () => {
		const r = filterRules(rules, 'block');
		expect(r).toHaveLength(1);
		expect(r[0].action.tag).toBe('block');
	});

	it('returns nothing when no rule matches', () => {
		expect(filterRules(rules, 'zzz-nope')).toHaveLength(0);
	});

	it('returns rules unchanged (same references) when query is empty', () => {
		const r = filterRules(rules, '');
		expect(r[0]).toBe(rules[0]);
	});
});

describe('generateRuleId', () => {
	it('returns unique IDs on rapid successive calls', () => {
		const a = generateRuleId();
		const b = generateRuleId();
		const c = generateRuleId();
		expect(a).not.toBe(b);
		expect(b).not.toBe(c);
		expect(a).not.toBe(c);
	});

	it('produces string IDs starting with "rule-"', () => {
		expect(generateRuleId()).toMatch(/^rule-/);
	});
});

describe('validatePort', () => {
	it('accepts a single valid port', () => {
		expect(validatePort('443')).toBeNull();
		expect(validatePort('80')).toBeNull();
	});
	it('accepts a port range with dash', () => {
		expect(validatePort('8080-8090')).toBeNull();
	});
	it('accepts comma-separated ports', () => {
		expect(validatePort('80,443,8080')).toBeNull();
	});
	it('rejects port > 65535', () => {
		expect(validatePort('70000')).not.toBeNull();
	});
	it('rejects port < 1', () => {
		expect(validatePort('0')).not.toBeNull();
	});
	it('rejects non-numeric garbage', () => {
		expect(validatePort('abc')).not.toBeNull();
	});
	it('accepts empty string (field not required)', () => {
		expect(validatePort('')).toBeNull();
	});
});

describe('validateCidr', () => {
	it('accepts a valid IPv4 CIDR', () => {
		expect(validateCidr('192.168.1.0/24')).toBeNull();
	});
	it('accepts a plain IPv4 address', () => {
		expect(validateCidr('10.0.0.1')).toBeNull();
	});
	it('accepts a valid IPv6 address', () => {
		expect(validateCidr('::1')).toBeNull();
		expect(validateCidr('2001:db8::1/128')).toBeNull();
	});
	it('rejects octet > 255', () => {
		expect(validateCidr('192.168.999.1')).not.toBeNull();
	});
	it('rejects prefix length > 32 for IPv4', () => {
		expect(validateCidr('10.0.0.0/33')).not.toBeNull();
	});
	it('accepts empty string (field not required)', () => {
		expect(validateCidr('')).toBeNull();
	});
});

describe('saveRouting preserves top-level fields', () => {
	it('keeps fields outside the routing object intact', async () => {
		// Simulate an existing 05_routing.json with extra top-level keys
		const existing = {
			routing: { domainStrategy: 'AsIs', rules: [] },
			comment: 'do not touch',
			version: 42,
		};
		getFile.mockResolvedValue({ content: JSON.stringify(existing) });
		saveFile.mockResolvedValue({});
		listFiles.mockResolvedValue([{ name: '05_routing.json', path: '/xray/05_routing.json' }]);

		const { saveRouting } = await import('../src/services/routing-rules.js');
		await saveRouting({ domainStrategy: 'IPIfNonMatch', rules: [{ type: 'field', outboundTag: 'direct' }] });

		expect(saveFile).toHaveBeenCalledTimes(1);
		const written = JSON.parse(saveFile.mock.calls[0][1]);
		// routing should be updated
		expect(written.routing.domainStrategy).toBe('IPIfNonMatch');
		// other top-level fields must survive
		expect(written.comment).toBe('do not touch');
		expect(written.version).toBe(42);
	});
});
