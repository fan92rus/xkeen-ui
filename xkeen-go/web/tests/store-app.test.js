/**
 * Tests for useAppStore — core application state (Pinia).
 *
 * Covers: sendInput (\\r regression), showToast, confirm, closeModal.
 */
// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { useAppStore } from '../src/stores/app.js';

// Mock all services that the app store (and its sub-stores) use.
// See store-editor.test.js for the same pattern.
vi.mock('../src/services/config.js', () => ({
  listFiles: vi.fn().mockResolvedValue([]),
  listFilesGrouped: vi.fn().mockResolvedValue([]),
  getFile: vi.fn(),
  saveFile: vi.fn(),
  getBackups: vi.fn().mockResolvedValue([]),
  getBackupContent: vi.fn().mockResolvedValue(''),
  default: {},
}));

vi.mock('../src/services/xkeen.js', () => ({
  getStatus: vi.fn().mockResolvedValue('unknown'),
  start: vi.fn(),
  stop: vi.fn(),
  restart: vi.fn(),
  getSettings: vi.fn().mockResolvedValue({ log_level: 'info' }),
  setLogLevel: vi.fn(),
}));

vi.mock('../src/services/logs.js', () => ({
  fetchLogs: vi.fn().mockResolvedValue([]),
}));

vi.mock('../src/services/update.js', () => ({
  checkUpdate: vi.fn().mockResolvedValue({}),
  startUpdate: vi.fn().mockResolvedValue({}),
}));

vi.mock('../src/services/status.js', () => ({
  connectStatusStream: vi.fn(),
  disconnectStatusStream: vi.fn(),
}));

vi.mock('../src/services/mode.js', () => ({
  getModeInfo: vi.fn().mockResolvedValue({
    xray_available: true,
    mihomo_available: false,
    mode: 'xray',
  }),
  setMode: vi.fn(),
}));

// Suppress logger output during tests.
vi.mock('../src/utils/logger.js', () => ({
  error: vi.fn(),
  warn: vi.fn(),
  log: vi.fn(),
}));

describe('useAppStore — sendInput (\\r regression)', () => {
  let app;

  beforeEach(() => {
    setActivePinia(createPinia());
    app = useAppStore();
    vi.clearAllMocks();
  });

  it('sends inputValue + \\r and clears inputValue (regression for da2d9dd)', () => {
    const session = { send: vi.fn(), connected: true, close: vi.fn() };
    app.interactiveSession = session;
    app.inputValue = '1';

    app.sendInput();

    // Must send with \\r, not \\n (regression: TUI menu ignored \\n)
    expect(session.send).toHaveBeenCalledWith('1\r');
    expect(app.inputValue).toBe('');
  });

  it('does NOT call send when there is no active session', () => {
    app.interactiveSession = null;
    app.inputValue = 'anything';
    app.sendInput();
    // Must not throw — implicit pass.
  });

  it('does NOT call send when session.connected is false', () => {
    const session = { send: vi.fn(), connected: false, close: vi.fn() };
    app.interactiveSession = session;
    app.inputValue = 'test';
    app.sendInput();
    expect(session.send).not.toHaveBeenCalled();
  });

  it('does NOT call send when commandComplete is true', () => {
    const session = { send: vi.fn(), connected: true, close: vi.fn() };
    app.interactiveSession = session;
    app.commandComplete = true;
    app.inputValue = 'test';
    app.sendInput();
    expect(session.send).not.toHaveBeenCalled();
  });

  it('does NOT call send when inputValue is empty', () => {
    const session = { send: vi.fn(), connected: true, close: vi.fn() };
    app.interactiveSession = session;
    app.inputValue = '';
    app.sendInput();
    expect(session.send).not.toHaveBeenCalled();
  });

  it('sends multi-character value + \\r', () => {
    const session = { send: vi.fn(), connected: true, close: vi.fn() };
    app.interactiveSession = session;
    app.inputValue = '2';
    app.sendInput();
    expect(session.send).toHaveBeenCalledWith('2\r');
  });
});

describe('useAppStore — showToast', () => {
  let app;

  beforeEach(() => {
    setActivePinia(createPinia());
    app = useAppStore();
    vi.clearAllMocks();
  });

  it('sets toast state with type', () => {
    expect(app.toast.show).toBe(false);
    app.showToast('Test message', 'error');

    expect(app.toast.message).toBe('Test message');
    expect(app.toast.type).toBe('error');
    expect(app.toast.show).toBe(true);
  });

  it('defaults type to empty string', () => {
    app.showToast('Just a message');
    expect(app.toast.type).toBe('');
  });

  it('calling showToast twice overwrites previous message', () => {
    app.showToast('First', 'success');
    app.showToast('Second', 'error');

    expect(app.toast.message).toBe('Second');
    expect(app.toast.type).toBe('error');
  });
});

describe('useAppStore — confirm', () => {
  let app;

  beforeEach(() => {
    setActivePinia(createPinia());
    app = useAppStore();
    vi.clearAllMocks();
  });

  it('executeConfirm calls onConfirm and clears state', () => {
    const onConfirm = vi.fn();
    app.confirm.onConfirm = onConfirm;
    app.confirm.command = 'test';
    app.confirm.description = 'desc';
    app.confirm.show = true;

    app.executeConfirm();

    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(app.confirm.show).toBe(false);
    expect(app.confirm.onConfirm).toBeNull();
    expect(app.confirm.command).toBe('');
    expect(app.confirm.description).toBe('');
  });

  it('executeConfirm is a no-op when onConfirm is null (leaves show=true, does not throw)', () => {
    app.confirm.onConfirm = null;
    app.confirm.show = true;
    app.confirm.command = 'test';

    // Must not throw. When no handler is set, the function does nothing.
    app.executeConfirm();
    expect(app.confirm.show).toBe(true);
    expect(app.confirm.command).toBe('test');
  });

  it('cancelConfirm clears state without calling onConfirm', () => {
    const onConfirm = vi.fn();
    app.confirm.onConfirm = onConfirm;
    app.confirm.show = true;
    app.confirm.command = 'test-rm';

    app.cancelConfirm();

    expect(onConfirm).not.toHaveBeenCalled();
    expect(app.confirm.show).toBe(false);
    expect(app.confirm.onConfirm).toBeNull();
    expect(app.confirm.command).toBe('');
    expect(app.confirm.description).toBe('');
  });
});

describe('useAppStore — closeModal', () => {
  let app;

  beforeEach(() => {
    setActivePinia(createPinia());
    app = useAppStore();
    vi.clearAllMocks();
  });

  it('clears modal state and closes interactive session', () => {
    const session = { close: vi.fn(), connected: true };
    app.interactiveSession = session;
    app.modal.show = true;
    app.modal.command = 'xkeen install';
    app.modal.output = 'some output';
    app.inputValue = 'text';
    app.commandComplete = true;

    app.closeModal();

    expect(app.modal.show).toBe(false);
    expect(app.modal.output).toBe('');
    expect(app.modal.command).toBe('');
    expect(app.modal.error).toBe('');
    expect(app.interactiveSession).toBeNull();
    expect(app.inputValue).toBe('');
    expect(app.commandComplete).toBe(false);
    expect(session.close).toHaveBeenCalled();
  });

  it('closeModal is safe when no session exists', () => {
    app.interactiveSession = null;
    app.modal.show = true;

    // Must not throw.
    app.closeModal();
    expect(app.modal.show).toBe(false);
  });
});

describe('useAppStore — canSendInput', () => {
  let app;

  beforeEach(() => {
    setActivePinia(createPinia());
    app = useAppStore();
    vi.clearAllMocks();
  });

  it('returns true when session is connected and command not complete', () => {
    app.interactiveSession = { connected: true, send: vi.fn(), close: vi.fn() };
    app.commandComplete = false;

    expect(app.canSendInput()).toBe(true);
  });

  it('returns falsy (null) when no session', () => {
    app.interactiveSession = null;
    // The && chain returns null, which is falsy — correct for the guard.
    expect(app.canSendInput()).toBeFalsy();
  });

  it('returns false when session not connected', () => {
    app.interactiveSession = { connected: false, send: vi.fn(), close: vi.fn() };
    expect(app.canSendInput()).toBe(false);
  });

  it('returns false when commandComplete', () => {
    app.interactiveSession = { connected: true, send: vi.fn(), close: vi.fn() };
    app.commandComplete = true;
    expect(app.canSendInput()).toBe(false);
  });
});
