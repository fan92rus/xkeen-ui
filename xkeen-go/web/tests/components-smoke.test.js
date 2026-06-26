/**
 * Smoke test: mount all components with minimal mocking.
 *
 * Catches regressions like:
 *  - sections[i] where i is out of bounds
 *  - ref access on undefined computed/state
 *  - missing v-if guards for async state
 *
 * Each component is mounted with a mocked backend that returns safe defaults.
 */
// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, beforeAll, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { createPinia, setActivePinia } from 'pinia';

// ── Global mocks ─────────────────────────────────────────────────────────

// Mock fetch so no component ever hits a real backend.
const MOCK_OK = JSON.stringify({ ok: true });

beforeAll(() => {
  global.fetch = vi.fn(() =>
    Promise.resolve({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ ok: true }),
      headers: new Map(),
    })
  );
  // Mock EventSource for services that use SSE (not used by components during
  // mount, but future-proof).
  global.EventSource = vi.fn(() => ({
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    close: vi.fn(),
  }));
});

// Mock all service modules so they return safe defaults without real fetch.
vi.mock('../services/logs.js', () => ({
  createLogStream: vi.fn(() => ({
    onMessage: vi.fn(),
    onError: vi.fn(),
    close: vi.fn(),
  })),
}));

vi.mock('../services/metrics.js', () => ({
  getProxyNames: vi.fn(() => Promise.resolve([])),
  getMetricsPort: vi.fn(() => Promise.resolve({ metrics_port: 0 })),
  createMetricsStream: vi.fn(() => ({
    onMessage: vi.fn(),
    onError: vi.fn(),
    close: vi.fn(),
  })),
}));

vi.mock('../services/subscription.js', () => ({
  getSubscriptions: () => Promise.resolve([]),
  getProfiles: () => Promise.resolve([]),
  getAutoApply: () => Promise.resolve({ enabled: false, cron: '' }),
  getSubscriptionFiles: () => Promise.resolve({}),
  listProfiles: () => Promise.resolve([]),
  listProxies: () => Promise.resolve([]),
  listSubscriptions: () => Promise.resolve({ subscriptions: [] }),
  applySubscriptions: () => Promise.resolve({ ok: true }),
  updateAutoApply: () => Promise.resolve({ enabled: false, cron: '' }),
  addSubscription: () => Promise.resolve({ id: 'mock' }),
  deleteSubscription: () => Promise.resolve({ ok: true }),
  updateSubscription: () => Promise.resolve({ ok: true }),
  fetchSubscription: () => Promise.resolve({ ok: true }),
  previewSubscriptions: () => Promise.resolve({ proxies: [], profiles: [] }),
  createProfile: () => Promise.resolve({ id: 'mock' }),
  deleteProfile: () => Promise.resolve({ ok: true }),
  updateProfile: () => Promise.resolve({ ok: true }),
  updateStrategy: () => Promise.resolve({ ok: true }),
  updateFilters: () => Promise.resolve({ ok: true }),
  getProxies: () => Promise.resolve([]),
}));

vi.mock('../services/install.js', () => ({
  getAWGStatus: vi.fn(() => Promise.resolve({ installed: false })),
  installAWG: vi.fn(() => Promise.resolve()),
  setupAWGInit: vi.fn(() => Promise.resolve({ ok: true })),
}));

vi.mock('../services/xkeen.js', () => ({
  getCommands: vi.fn(() => Promise.resolve({ commands: [], error: '' })),
}));

vi.mock('../services/interactive.js', () => ({
  InteractiveSession: class {
    constructor() { this.connected = false; }
    connect() {}
    send() {}
    close() {}
  },
}));

// Mock CodeMirror imports for EditorTab — heavy and not needed for mount check.
// EditorView must be a callable constructor: component does `new EditorView({...})`.
// vi.mock factories are hoisted — everything must be inline.
vi.mock('codemirror', () => {
  function EV() { this.destroy = vi.fn(); }
  EV.theme = function() { return []; };
  EV.lineWrapping = [];
  return { EditorView: EV, basicSetup: [] };
});
vi.mock('@codemirror/lang-json', () => ({ json: () => [] }));
vi.mock('@codemirror/lang-yaml', () => ({ yaml: () => [] }));
vi.mock('@codemirror/theme-one-dark', () => ({ oneDark: [] }));
vi.mock('@codemirror/state', () => ({ EditorState: {} }));
vi.mock('@codemirror/view', () => {
  function EV() { this.destroy = vi.fn(); }
  EV.theme = function() { return []; };
  EV.lineWrapping = [];
  return { EditorView: EV, keymap: () => [] };
});

// Mock Chart.js for MetricsTab — must provide all exports that
// useMetricsChart.js imports. vi.mock factory is hoisted, so classes must
// be defined inline (no variables from the outer scope).
vi.mock('chart.js', () => {
  const Mc = class {
    constructor() { this.data = {}; this.update = vi.fn(); this.destroy = vi.fn(); }
    static register() {}
  };
  const C = class {};
  return {
    Chart: Mc,
    LineController: C,
    LineElement: C,
    PointElement: C,
    LinearScale: C,
    CategoryScale: C,
    Filler: C,
    Legend: C,
    Tooltip: C,
    registerables: [],
  };
});

// ── Components ───────────────────────────────────────────────────────────

import CommandsTab from '../src/components/CommandsTab.vue';
import EditorTab from '../src/components/EditorTab.vue';
import LogsTab from '../src/components/LogsTab.vue';
import MetricsTab from '../src/components/MetricsTab.vue';
import SettingsTab from '../src/components/SettingsTab.vue';

// ── Helpers ──────────────────────────────────────────────────────────────

// Track Vue rendering errors/warnings during each test.
function trackErrors(testFn) {
  return async () => {
    const errors = [];
    const warnings = [];

    const origErrorHandler = globalThis?.onerror?.bind?.(globalThis);
    // Can't easily hook Vue's error handler from outside — instead rely on
    // the fact that undefined access throws synchronously during render.

    await testFn(errors, warnings);
  };
}

function mountWithPinia(component) {
  return mount(component, {
    global: {
      plugins: [createPinia()],
      stubs: {
        // CodeMirror renders nothing in stub mode; we just need it not to throw.
        Codemirror: { template: '<div />' },
      },
    },
  });
}

// ── Tests ────────────────────────────────────────────────────────────────

describe('Component smoke tests', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  it('SettingsTab mounts without errors and renders all sections', async () => {
    const w = mountWithPinia(SettingsTab);
    await flushPromises();

    expect(w.exists()).toBe(true);

    // All 7 sections must render — this catches the sections[6] bug.
    const sections = w.findAll('.s-section');
    expect(sections.length).toBe(7);

    // Check each section has a heading (h2).
    const headings = w.findAll('.s-title');
    expect(headings.length).toBe(7);

    // Verify key sections exist by content.
    expect(w.html()).toContain('Режим');
    expect(w.html()).toContain('Логирование');
    expect(w.html()).toContain('Обновления');
    expect(w.html()).toContain('Безопасность');
    expect(w.html()).toContain('Автообновление');
    expect(w.html()).toContain('Метрики');
    expect(w.html()).toContain('AmneziaWG');
  });

  it('CommandsTab mounts without errors', async () => {
    const w = mountWithPinia(CommandsTab);
    await flushPromises();
    expect(w.exists()).toBe(true);
    expect(() => w.html()).not.toThrow();
  });

  it('EditorTab mounts without errors', async () => {
    const w = mountWithPinia(EditorTab);
    await flushPromises();
    expect(w.exists()).toBe(true);
  });

  it('LogsTab mounts without errors', async () => {
    const w = mountWithPinia(LogsTab);
    await flushPromises();
    expect(w.exists()).toBe(true);
  });

  it('MetricsTab mounts without errors', async () => {
    const w = mountWithPinia(MetricsTab);
    await flushPromises();
    expect(w.exists()).toBe(true);
  });

});
