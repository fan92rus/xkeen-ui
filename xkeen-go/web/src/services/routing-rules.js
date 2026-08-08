// routing-rules.js — read/write 05_routing.json via existing config API
import { getFile, saveFile, listFiles } from './config.js';
import { get } from './api.js';

const ROUTING_FILE = '05_routing.json';
const OUTBOUND_FILE = '04_outbounds.json';

// Built-in Xray tags that always exist
const BUILTIN_TAGS = ['direct', 'block'];

// Resolve the full path of a config file via the files API.
// Cache the resolved paths to avoid duplicate listFiles calls.
const _pathCache = {};

/** Clear the path resolution cache. Call after save to force re-resolution. */
export function clearPathCache() {
	Object.keys(_pathCache).forEach(k => delete _pathCache[k]);
}

async function resolvePath(name) {
	if (_pathCache[name]) return _pathCache[name];
	const files = await listFiles('xray');
	const found = files.find(f => f.name === name);
	if (!found) throw new Error(`${name} not found in config directory`);
	_pathCache[name] = found.path;
	return _pathCache[name];
}

// ── Read/write routing config ──

export async function getRouting() {
	const fullPath = await resolvePath(ROUTING_FILE);
	const resp = await getFile(fullPath);
	const parsed = JSON.parse(resp.content);
	// Attach the resolved path so the UI can use it as a stable key for
	// browser-side persistence (e.g. disabled rules in localStorage).
	if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
		parsed.__path = fullPath;
	}
	return parsed;
}

export async function saveRouting(routing) {
	const fullPath = await resolvePath(ROUTING_FILE);
	// Read the existing file so we can update only the `routing` key while
	// preserving any sibling top-level fields (e.g. `comment`, `version`).
	let existing = {};
	try {
		const resp = await getFile(fullPath);
		existing = JSON.parse(resp.content) || {};
	} catch {
		// File may not exist yet on first save — start from an empty object.
	}
	const merged = { ...existing, routing };
	const content = JSON.stringify(merged, null, 2);
	const result = await saveFile(fullPath, content);
	clearPathCache();
	return result;
}

// ── Disabled-rule persistence (browser localStorage) ──
// Disabled rules are dropped from the Xray config (serializeRule → null) but
// kept in the UI so the user can toggle them back on. They are persisted to
// localStorage keyed by the routing-file path, keeping Xray's 05_routing.json
// clean and free of non-standard fields.

function disabledStorageKey(fullPath) {
	return `xkeen-routing-disabled:${fullPath}`;
}

/** Read previously-disabled rules for the given routing file path. */
export function loadDisabledRules(fullPath) {
	try {
		const raw = localStorage.getItem(disabledStorageKey(fullPath));
		return raw ? JSON.parse(raw) : [];
	} catch {
		return [];
	}
}

/** Persist disabled rules (normalized shape) for the given routing file path. */
export function saveDisabledRules(fullPath, rules) {
	try {
		localStorage.setItem(disabledStorageKey(fullPath), JSON.stringify(rules));
	} catch {
		// localStorage may be unavailable (private mode, quota) — non-fatal.
	}
}

// ── Available outbound/balancer tag discovery ──

/** Parse outbound tags from 04_outbounds.json */
async function getOutboundTags() {
	try {
		const fullPath = await resolvePath(OUTBOUND_FILE);
		const resp = await getFile(fullPath);
		const data = JSON.parse(resp.content);
		const outbounds = data.outbounds || [];
		return outbounds.map(o => o.tag).filter(Boolean);
	} catch {
		return [];
	}
}

/** Parse balancer tags from the routing config itself */
async function getBalancerTags() {
	try {
		const data = await getRouting();
		const r = data.routing || data;
		return (r.balancers || []).map(b => b.tag).filter(Boolean);
	} catch {
		return [];
	}
}

/**
 * Fetch all valid tags: built-in + outbound tags + balancer tags.
 * Returns { outboundTags: string[], balancerTags: string[], allTags: string[] }
 */
export async function getAvailableTags() {
	const [outboundTags, balancerTags] = await Promise.all([
		getOutboundTags(),
		getBalancerTags(),
	]);
	const all = [...BUILTIN_TAGS, ...outboundTags, ...balancerTags];
	return { outboundTags, balancerTags, allTags: [...new Set(all)] };
}

/**
 * Validate a single rule's action against available tags.
 * Returns null if valid, or an error string.
 */
export function validateAction(action, availableTags) {
	if (!action || !action.tag) return 'Action missing tag';
	const tag = action.tag;
	// Built-in tags are always valid
	if (BUILTIN_TAGS.includes(tag)) return null;
	// Check against known tags
	if (!availableTags.includes(tag)) {
		return `Outlet «${tag}» not found in config`;
	}
	return null;
}

// ── Domain/IP entry parsing ──

export function parseEntry(raw) {
	// ext:database.dat:category
	const extMatch = raw.match(/^ext:([^:]+\.dat):(.+)$/);
	if (extMatch) {
		return { type: 'ext', db: extMatch[1], value: extMatch[2], raw };
	}
	// geosite:category
	if (raw.startsWith('geosite:')) {
		return { type: 'geosite', value: raw.slice(8), raw };
	}
	// geoip:code
	if (raw.startsWith('geoip:')) {
		return { type: 'geoip', value: raw.slice(6), raw };
	}
	// regexp:pattern
	if (raw.startsWith('regexp:')) {
		return { type: 'regexp', value: raw.slice(7), raw };
	}
	// domain:foo.com
	if (raw.startsWith('domain:')) {
		return { type: 'domain', value: raw.slice(7), raw };
	}
	// full:foo.com
	if (raw.startsWith('full:')) {
		return { type: 'full', value: raw.slice(5), raw };
	}
	// CIDR (ip only)
	if (/^\d+\.\d+\.\d+\.\d+\/\d+$/.test(raw)) {
		return { type: 'cidr', value: raw, raw };
	}
	if (/^[0-9a-fA-F:]+\/\d+$/.test(raw)) {
		return { type: 'cidr', value: raw, raw };
	}
	// Plain domain/IP
	return { type: 'plain', value: raw, raw };
}

export function entryLabel(e) {
	switch (e.type) {
		case 'ext': return `ext:${e.db}:${e.value}`;
		case 'geosite': return `geosite:${e.value}`;
		case 'geoip': return `geoip:${e.value}`;
		case 'regexp': return `/${e.value}/`;
		case 'domain': return `*.${e.value}`;
		case 'full': return e.value;
		case 'cidr': return e.value;
		default: return e.value;
	}
}

export function entryIcon(e) {
	switch (e.type) {
		case 'ext':
		case 'geosite': return '📁';
		case 'geoip': return '🌍';
		case 'regexp': return '⚙️';
		case 'cidr': return '🔢';
		default: return '🌐';
	}
}

// ── Rule ID generation (collision-safe under rapid clicks) ──

let _ruleIdCounter = 0;
export function generateRuleId() {
	_ruleIdCounter++;
	return `rule-${Date.now()}-${_ruleIdCounter}`;
}

// ── Port validation ──

/**
 * Validate an Xray port specification: a single port (1-65535), a range
 * ("8080-8090"), or a comma-separated list of either. Returns an error
 * string when invalid, or null when valid. Empty string is valid (optional).
 */
export function validatePort(input) {
	const val = (input || '').trim();
	if (!val) return null;
	const parts = val.split(',');
	for (const part of parts) {
		const range = part.trim().split('-');
		for (const p of range) {
			const n = Number(p.trim());
			if (!Number.isInteger(n) || n < 1 || n > 65535) {
				return `Invalid port: ${p.trim()}`;
			}
		}
		if (range.length === 2 && Number(range[0]) > Number(range[1])) {
			return `Port range start > end: ${part.trim()}`;
		}
	}
	return null;
}

// ── CIDR / IP validation ──

/**
 * Validate an IPv4/IPv6 address, optionally with CIDR prefix. Returns an
 * error string when invalid, or null when valid. Empty = valid (optional).
 */
export function validateCidr(input) {
	const val = (input || '').trim();
	if (!val) return null;

	// Split address and optional prefix length
	const slash = val.lastIndexOf('/');
	const addr = slash >= 0 ? val.slice(0, slash) : val;
	const prefix = slash >= 0 ? Number(val.slice(slash + 1)) : null;

	if (!addr) return 'Empty address';

	// Try IPv4
	const v4 = addr.split('.');
	if (v4.length === 4) {
		for (const octet of v4) {
			const n = Number(octet);
			if (!Number.isInteger(n) || n < 0 || n > 255) {
				return `Invalid IPv4 octet: ${octet}`;
			}
		}
		if (prefix !== null && (prefix < 0 || prefix > 32)) {
			return `Invalid prefix length: ${prefix}`;
		}
		return null;
	}

	// Try IPv6 — accept if it contains ':' and is non-empty
	if (addr.includes(':')) {
		if (prefix !== null && (prefix < 0 || prefix > 128)) {
			return `Invalid prefix length: ${prefix}`;
		}
		return null;
	}

	return `Invalid address: ${addr}`;
}

// ── Rule normalization ──

export function normalizeRule(rule, index) {
	const action = rule.outboundTag
		? { kind: 'outbound', tag: rule.outboundTag }
		: rule.balancerTag
			? { kind: 'balancer', tag: rule.balancerTag }
			: { kind: 'outbound', tag: 'direct' };

	const domains = (rule.domain || []).map(parseEntry);
	const ips = (rule.ip || []).map(parseEntry);
	const networks = rule.network ? rule.network.split(',').map(s => s.trim()) : [];

	return {
		id: `rule-${index}`,
		name: rule.name || guessRuleName(domains, ips, action),
		disabled: rule.disabled === true,
		domains,
		ips,
		networks,
		port: rule.port || '',
		inbound: rule.inboundTag || [],
		action,
		raw: rule,
	};
}

// ── Rule filtering (for the search box) ──

/**
 * Filter normalized rules by a free-text query. Matches case-insensitively
 * against the rule name, domain/IP entry values and raw text, and the action
 * tag. Returns the same rule references (no cloning) so the caller's
 * reactivity is preserved.
 */
export function filterRules(rules, query) {
	const q = (query || '').trim().toLowerCase();
	if (!q) return rules;
	return rules.filter(rule => {
		if ((rule.name || '').toLowerCase().includes(q)) return true;
		if ((rule.action?.tag || '').toLowerCase().includes(q)) return true;
		const inDomains = rule.domains?.some(d =>
			(d.value || '').toLowerCase().includes(q) ||
			(d.raw || '').toLowerCase().includes(q));
		if (inDomains) return true;
		const inIps = rule.ips?.some(ip =>
			(ip.value || '').toLowerCase().includes(q) ||
			(ip.raw || '').toLowerCase().includes(q));
		return inIps;
	});
}

function guessRuleName(domains, ips, action) {
	if (domains.length > 0) {
		const first = domains[0];
		if (first.type === 'ext' || first.type === 'geosite') {
			if (first.value.includes('ru')) return '🇷🇺 RU Direct';
			if (first.value.includes('ads')) return '🚫 Block Ads';
		}
		return `🌐 ${entryLabel(first)}`;
	}
	if (ips.length > 0) return `🌍 ${entryLabel(ips[0])}`;
	if (action.tag === 'direct') return '📭 Catch-all';
	return `→ ${action.tag}`;
}

// ── Common geosite/geoip categories for autocomplete ──

export const COMMON_GEOSITE = [
	// v2fly categories
	{ value: 'category-ads', label: 'Реклама (block list)', db: 'geosite_v2fly.dat' },
	{ value: 'category-ads-ru', label: 'Российская реклама', db: 'geosite_v2fly.dat' },
	{ value: 'category-ru', label: 'Российские сайты', db: 'geosite_v2fly.dat' },
	{ value: 'category-gov-ru', label: 'Российские гос. сайты', db: 'geosite_v2fly.dat' },
	{ value: 'category-gov', label: 'Государственные сайты', db: 'geosite_v2fly.dat' },
	{ value: 'category-social', label: 'Соц. сети', db: 'geosite_v2fly.dat' },
	{ value: 'category-streaming', label: 'Стриминг', db: 'geosite_v2fly.dat' },
	{ value: 'category-media', label: 'Медиа', db: 'geosite_v2fly.dat' },
	{ value: 'category-gaming', label: 'Игры', db: 'geosite_v2fly.dat' },
	{ value: 'category-dev', label: 'Разработка', db: 'geosite_v2fly.dat' },
	// Individual sites
	{ value: 'google', label: 'Google', db: 'geosite_v2fly.dat' },
	{ value: 'youtube', label: 'YouTube', db: 'geosite_v2fly.dat' },
	{ value: 'netflix', label: 'Netflix', db: 'geosite_v2fly.dat' },
	{ value: 'telegram', label: 'Telegram', db: 'geosite_v2fly.dat' },
	{ value: 'facebook', label: 'Facebook', db: 'geosite_v2fly.dat' },
	{ value: 'twitter', label: 'Twitter/X', db: 'geosite_v2fly.dat' },
	{ value: 'instagram', label: 'Instagram', db: 'geosite_v2fly.dat' },
	{ value: 'vk', label: 'ВКонтакте', db: 'geosite_v2fly.dat' },
	{ value: 'yandex', label: 'Яндекс', db: 'geosite_v2fly.dat' },
	{ value: 'openai', label: 'OpenAI', db: 'geosite_v2fly.dat' },
	{ value: 'github', label: 'GitHub', db: 'geosite_v2fly.dat' },
	{ value: 'steam', label: 'Steam', db: 'geosite_v2fly.dat' },
	{ value: 'discord', label: 'Discord', db: 'geosite_v2fly.dat' },
	{ value: 'reddit', label: 'Reddit', db: 'geosite_v2fly.dat' },
	{ value: 'twitch', label: 'Twitch', db: 'geosite_v2fly.dat' },
	{ value: 'spotify', label: 'Spotify', db: 'geosite_v2fly.dat' },
	// zkeen custom
	{ value: 'domains', label: 'xkeen: основные домены', db: 'zkeen.dat' },
	{ value: 'other', label: 'xkeen: прочие', db: 'zkeen.dat' },
	{ value: 'politic', label: 'xkeen: политика', db: 'zkeen.dat' },
];

export const COMMON_GEOIP = [
	{ value: 'private', label: 'RFC1918 (LAN)', flag: '🏠' },
	{ value: 'ru', label: 'Russia', flag: '🇷🇺' },
	{ value: 'cn', label: 'China', flag: '🇨🇳' },
	{ value: 'us', label: 'USA', flag: '🇺🇸' },
	{ value: 'de', label: 'Germany', flag: '🇩🇪' },
	{ value: 'fr', label: 'France', flag: '🇫🇷' },
	{ value: 'gb', label: 'UK', flag: '🇬🇧' },
	{ value: 'nl', label: 'Netherlands', flag: '🇳🇱' },
	{ value: 'ua', label: 'Ukraine', flag: '🇺🇦' },
	{ value: 'by', label: 'Belarus', flag: '🇧🇾' },
	{ value: 'kz', label: 'Kazakhstan', flag: '🇰🇿' },
	// zkeen custom IPs
	{ value: 'discord', label: 'Discord IPs', db: 'zkeenip.dat', flag: '🎮' },
	{ value: 'google', label: 'Google IPs', db: 'zkeenip.dat', flag: '🔍' },
	{ value: 'youtube', label: 'YouTube IPs', db: 'zkeenip.dat', flag: '📺' },
	{ value: 'cloudflare', label: 'Cloudflare', db: 'zkeenip.dat', flag: '☁️' },
	{ value: 'amazon', label: 'Amazon/AWS', db: 'zkeenip.dat', flag: '📦' },
	{ value: 'telegram', label: 'Telegram IPs', db: 'zkeenip.dat', flag: '✈️' },
	{ value: 'meta', label: 'Meta/Facebook', db: 'zkeenip.dat', flag: '👥' },
	{ value: 'hetzner', label: 'Hetzner', db: 'zkeenip.dat', flag: '🖥️' },
	{ value: 'ovh', label: 'OVH', db: 'zkeenip.dat', flag: '🖥️' },
	{ value: 'digitalocean', label: 'DigitalOcean', db: 'zkeenip.dat', flag: '🌊' },
	{ value: 'vultr', label: 'Vultr', db: 'zkeenip.dat', flag: '🌋' },
	{ value: 'linode', label: 'Linode', db: 'zkeenip.dat', flag: '🐘' },
	{ value: 'azure', label: 'Azure', db: 'zkeenip.dat', flag: '☁️' },
	{ value: 'fastly', label: 'Fastly CDN', db: 'zkeenip.dat', flag: '⚡' },
	{ value: 'gcore', label: 'Gcore CDN', db: 'zkeenip.dat', flag: '🌐' },
];

// ── Serialization (UI rule → Xray wire format) ──

export function serializeRule(rule) {
	// Disabled rules are dropped from the Xray config entirely. The UI keeps
	// them around (toggled off) and persists them separately so a reload does
	// not silently lose them; see save() in RoutingTab.vue.
	if (rule.disabled) return null;
	const obj = { ...rule.raw }; // preserve unknown fields from original (protocol, routeOnly, etc.)
	obj.type = 'field'; // required by Xray for field routing rules
	delete obj.domain;
	delete obj.ip;
	delete obj.network;
	delete obj.port;
	delete obj.outboundTag;
	delete obj.balancerTag;
	delete obj.inboundTag;
	if (rule.domains.length) obj.domain = rule.domains.map(d => d.raw);
	if (rule.ips.length) obj.ip = rule.ips.map(ip => ip.raw);
	if (rule.networks.length) obj.network = rule.networks.join(',');
	if (rule.port) obj.port = rule.port;
	if (rule.inbound.length) obj.inboundTag = rule.inbound;
	if (rule.action.kind === 'balancer') obj.balancerTag = rule.action.tag;
	else obj.outboundTag = rule.action.tag;
	// Persist a user-chosen name; drop the auto-guessed one so the JSON stays
	// clean and existing round-trip equality is preserved.
	const guessed = guessRuleName(rule.domains, rule.ips, rule.action);
	if (rule.name && rule.name !== guessed) obj.name = rule.name;
	else delete obj.name;
	return obj;
}

// ── Category autocomplete from backend ──

export async function fetchCategories() {
	return get('/api/routing/categories');
}
