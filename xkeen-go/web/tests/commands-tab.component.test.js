/**
 * Component test for CommandsTab.vue.
 *
 * The command palette is NO LONGER hardcoded — it is fetched from the backend
 * registry (GET /api/xkeen/commands), which parses `xkeen -help`. These tests
 * mock the service so we assert the fetch→group→render flow without a live
 * backend, and confirm phantom commands never reach the UI.
 */
// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { mount, flushPromises } from '@vue/test-utils';
import { createPinia, setActivePinia } from 'pinia';

// Stub the interactive service so no real WebSocket is created on click.
vi.mock('../services/interactive.js', () => ({
  InteractiveSession: class {
    constructor() { this.connected = false; }
    connect() {}
    send() {}
    close() {}
  },
}));

// Mock the backend command registry. Individual tests override the resolved
// value via mockGetCommands (default = a small representative sample).
import * as xkeenService from '../src/services/xkeen.js';
const mockGetCommands = vi.fn();
vi.spyOn(xkeenService, 'getCommands').mockImplementation((...args) => mockGetCommands(...args));

import CommandsTab from '../src/components/CommandsTab.vue';

// A representative backend payload mirroring the real registry shape.
const SAMPLE = [
  { cmd: '-start', description: 'Запуск', category: 'Управление прокси-клиентом', dangerous: false },
  { cmd: '-status', description: 'Статус', category: 'Управление прокси-клиентом', dangerous: false },
  { cmd: '-v', description: 'Версия XKeen', category: 'Информация', dangerous: false },
  { cmd: '-i', description: 'Установка XKeen', category: 'Установка', dangerous: true },
  { cmd: '-remove', description: 'Полная деинсталляция', category: 'Удаление', dangerous: true },
];

function mountCommands() {
  return mount(CommandsTab, { global: { plugins: [createPinia()] } });
}

describe('CommandsTab', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    mockGetCommands.mockReset();
    mockGetCommands.mockResolvedValue(SAMPLE);
  });

  it('mounts, fetches the palette, and renders backend commands', async () => {
    const w = mountCommands();
    await flushPromises();
    expect(mockGetCommands).toHaveBeenCalledTimes(1);
    expect(w.exists()).toBe(true);
  });

  it('shows a loading indicator before the fetch resolves', async () => {
    // Never-resolving promise keeps the component in the loading state.
    mockGetCommands.mockReturnValue(new Promise(() => {}));
    const w = mountCommands();
    await flushPromises();
    expect(w.text()).toContain('Загрузка списка команд');
  });

  it('renders the fetched commands grouped by category', async () => {
    const w = mountCommands();
    await flushPromises();
    const text = w.text();
    // Categories from the backend.
    expect(text).toContain('Управление прокси-клиентом');
    expect(text).toContain('Информация');
    expect(text).toContain('Установка');
    // Real commands.
    expect(text).toContain('-start');
    expect(text).toContain('-status');
    expect(text).toContain('-v');
    expect(text).toContain('-i');
  });

  it('does NOT render phantom commands that were in the old hardcoded list', async () => {
    const w = mountCommands();
    await flushPromises();
    const text = w.text();
    for (const phantom of ['-tpx', '-cb', '-rrk', '-rrx', '-rrm', '-drk', '-drx', '-drm', '-modules', '-delmodules']) {
      expect(text).not.toContain(phantom);
    }
  });

  it('flags dangerous commands with btn-danger from the backend flag', async () => {
    const w = mountCommands();
    await flushPromises();
    const items = w.findAll('.command-item');
    const installItem = items.find(el => el.find('.command-name').text() === '-i');
    expect(installItem, 'expected an -i command item').toBeTruthy();
    expect(installItem.find('button').classes()).toContain('btn-danger');

    const statusItem = items.find(el => el.find('.command-name').text() === '-status');
    expect(statusItem, 'expected a -status command item').toBeTruthy();
    expect(statusItem.find('button').classes()).toContain('btn-primary');
  });

  it('shows an error message when the backend fetch fails', async () => {
    mockGetCommands.mockRejectedValue(new Error('network down'));
    const w = mountCommands();
    await flushPromises();
    expect(w.text()).toContain('Не удалось загрузить список команд');
    expect(w.text()).toContain('network down');
  });

  it('shows an empty-state hint when the registry returns no commands', async () => {
    // xkeen not installed → backend returns []. UI must not crash and should
    // tell the user why there is nothing to run.
    mockGetCommands.mockResolvedValue([]);
    const w = mountCommands();
    await flushPromises();
    expect(w.text()).toContain('Команды недоступны');
    expect(w.findAll('.command-item')).toHaveLength(0);
  });
});
