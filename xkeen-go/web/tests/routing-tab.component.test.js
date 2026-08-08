/**
 * Component test for RoutingTab.vue transient UI state.
 *
 * Reproduces the bug where per-rule input state (domain text, suggestions,
 * regex warnings) was keyed by the v-for array index. Deleting or reordering
 * a rule shifted indices and leaked another rule's transient state into the
 * card that took its place. After the fix, state is keyed by rule.id so it
 * stays attached to the correct rule and does not survive a delete.
 */
// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { createPinia, setActivePinia } from 'pinia';

// ── Mock the routing service so no backend calls happen on mount ─────────
const TWO_RULES = {
	routing: {
		domainStrategy: 'AsIs',
		balancers: [],
		rules: [
			{ type: 'field', domain: ['geosite:google'], outboundTag: 'proxy' },
			{ type: 'field', domain: ['geosite:youtube'], outboundTag: 'direct' },
		],
	},
	__path: '/mock/05_routing.json',
};

vi.mock('../src/services/routing-rules.js', async (importOriginal) => {
	const actual = await importOriginal();
	return {
		...actual,
		getRouting: vi.fn(async () => TWO_RULES),
		getAvailableTags: vi.fn(async () => ({ outboundTags: ['proxy', 'direct'], balancerTags: [], allTags: ['direct', 'block', 'proxy'] })),
		fetchCategories: vi.fn(async () => null),
		// localStorage-backed helpers: use the real implementations.
		loadDisabledRules: actual.loadDisabledRules,
		saveDisabledRules: actual.saveDisabledRules,
	};
});

import RoutingTab from '../src/components/RoutingTab.vue';

function mountTab() {
	return mount(RoutingTab, { attachTo: document.body });
}

describe('RoutingTab transient UI state (keyed by rule.id)', () => {
	beforeEach(() => {
		setActivePinia(createPinia());
		localStorage.clear();
	});

	it('does not leak a deleted rule\'s domain input into the next card', async () => {
		const wrapper = mountTab();
		await flushPromises(); // onMounted: getRouting + getAvailableTags

		// Expand the first rule and type into its domain input.
		const firstHeader = wrapper.findAll('.rt-card-header')[0];
		await firstHeader.trigger('click');

		const domainInput = wrapper.find('.rt-tag-input');
		await domainInput.setValue('geosite:netflix');

		// Sanity: the first card now has transient domain input text.
		expect(wrapper.find('.rt-tag-input').element.value).toBe('geosite:netflix');

		// Collapse, then delete the first rule (two-click confirm).
		await firstHeader.trigger('click');
		const firstCard = wrapper.findAll('.rt-card')[0];
		// First click arms confirmation, second click deletes.
		await firstCard.find('.rt-icon-danger').trigger('click');
		await firstCard.find('.rt-icon-confirm').trigger('click');

		// After deletion the card that was second is now first. Expand it and
		// assert its domain input is empty — not the leaked "geosite:netflix".
		const newFirstHeader = wrapper.findAll('.rt-card-header')[0];
		await newFirstHeader.trigger('click');
		const newInput = wrapper.find('.rt-tag-input');
		expect(newInput.element.value).toBe('');
	});
});
