/**
 * Tests for useConfigStore — file operations and backup management.
 *
 * loadFile / saveFile are already covered by store-editor.test.js.
 * This file covers the REMAINING functions: loadFiles, loadGroupedFiles,
 * showBackups, selectBackup, copyBackupContent, loadBackupToEditor,
 * openDiffModal, closeDiffModal, closeBackupsModal.
 */
// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { useConfigStore } from '../src/stores/config.js';

// Import the mocked config service so we can control return values.
// vi.mock is hoisted above imports, so we get the mocked version.
import * as configService from '../src/services/config.js';

vi.mock('../src/services/config.js', () => {
  const mock = {
    listFiles: vi.fn(),
    listFilesGrouped: vi.fn(),
    getFile: vi.fn(),
    saveFile: vi.fn(),
    getBackups: vi.fn(),
    getBackupContent: vi.fn(),
  };
  return { ...mock, default: mock };
});

// Mock other services needed by the app store (configStore calls useAppStore).
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

vi.mock('../src/utils/logger.js', () => ({
  error: vi.fn(),
  warn: vi.fn(),
  log: vi.fn(),
}));

function makeFile(name) {
  return { path: `/opt/etc/xkeen/${name}`, name };
}

describe('useConfigStore — loadFiles', () => {
  let config;

  beforeEach(() => {
    setActivePinia(createPinia());
    config = useConfigStore();
    vi.clearAllMocks();
  });

  it('calls listFiles with the mode and assigns result to files', async () => {
    const fakeFiles = [makeFile('01_log.json'), makeFile('05_routing.json')];
    configService.listFiles.mockResolvedValue(fakeFiles);

    await config.loadFiles('xray');

    expect(configService.listFiles).toHaveBeenCalledWith('xray');
    expect(config.files).toEqual(fakeFiles);
  });

  it('works for mihomo mode', async () => {
    const fakeFiles = [makeFile('01_log.yaml')];
    configService.listFiles.mockResolvedValue(fakeFiles);

    await config.loadFiles('mihomo');
    expect(configService.listFiles).toHaveBeenCalledWith('mihomo');
    expect(config.files).toEqual(fakeFiles);
  });
});

describe('useConfigStore — loadGroupedFiles', () => {
  let config;

  beforeEach(() => {
    setActivePinia(createPinia());
    config = useConfigStore();
    vi.clearAllMocks();
  });

  it('sets fileGroups and builds flat files list with _section and _label', async () => {
    const groups = [
      {
        section: 'core',
        label: 'Основные',
        files: [makeFile('01_log.json')],
      },
      {
        section: 'outbound',
        label: 'Исходящие',
        files: [makeFile('04_outbounds.json')],
      },
    ];
    configService.listFilesGrouped.mockResolvedValue(groups);

    await config.loadGroupedFiles();

    expect(configService.listFilesGrouped).toHaveBeenCalledOnce();
    expect(config.fileGroups).toEqual(groups);
    // Flat files list is built: each file gets _section and _label from its group.
    expect(config.files).toHaveLength(2);
    expect(config.files[0]._section).toBe('core');
    expect(config.files[0]._label).toBe('Основные');
    expect(config.files[1]._section).toBe('outbound');
    expect(config.files[1]._label).toBe('Исходящие');
  });

  it('sets empty arrays when no groups returned', async () => {
    configService.listFilesGrouped.mockResolvedValue([]);

    await config.loadGroupedFiles();

    expect(config.fileGroups).toEqual([]);
    expect(config.files).toEqual([]);
  });
});

describe('useConfigStore — showBackups', () => {
  let config;

  beforeEach(() => {
    setActivePinia(createPinia());
    config = useConfigStore();
    vi.clearAllMocks();
  });

  it('loads backup list, opens modal, selects first backup', async () => {
    const backups = [
      { path: '/backups/01_log_1700000000.json', created_at: '2026-03-01' },
    ];
    configService.getBackups.mockResolvedValue(backups);
    configService.getBackupContent.mockResolvedValue('{"log":{"level":"info"}}');

    // Set currentFile so showBackups can proceed.
    config.currentFile = {
      path: '/opt/etc/xkeen/01_log.json',
      content: '{"log":{"level":"info"}}',
    };
    config.lastSavedContent = '{"log":{"level":"info"}}';

    await config.showBackups();

    expect(configService.getBackups).toHaveBeenCalledWith('/opt/etc/xkeen/01_log.json');
    expect(config.backupsModal.show).toBe(true);
    expect(config.backupsModal.fileName).toBe('01_log.json');
    expect(config.backupsModal.backups).toEqual(backups);
    // First backup should be selected automatically.
    expect(config.backupsModal.selectedBackup).toEqual(backups[0]);
    expect(configService.getBackupContent).toHaveBeenCalledWith(backups[0].path);
    expect(config.backupsModal.diffContent).toBeTypeOf('string');
  });

  it('does nothing when no currentFile is set', async () => {
    config.currentFile = null;
    await config.showBackups();
    expect(config.backupsModal.show).toBe(false);
  });
});

describe('useConfigStore — closeBackupsModal', () => {
  let config;

  beforeEach(() => {
    setActivePinia(createPinia());
    config = useConfigStore();
    vi.clearAllMocks();
  });

  it('clears backup modal state', () => {
    config.backupsModal.show = true;
    config.backupsModal.selectedBackup = { path: '/test' };
    config.backupsModal.diffContent = 'diff';

    config.closeBackupsModal();

    expect(config.backupsModal.show).toBe(false);
    expect(config.backupsModal.selectedBackup).toBeNull();
    expect(config.backupsModal.diffContent).toBe('');
  });
});

describe('useConfigStore — copyBackupContent', () => {
  let config;

  beforeEach(() => {
    setActivePinia(createPinia());
    config = useConfigStore();
    vi.clearAllMocks();
  });

  it('fetches content and copies to clipboard', async () => {
    configService.getBackupContent.mockResolvedValue('backup content text');

    // Ensure navigator.clipboard exists in happy-dom.
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      writable: true,
      configurable: true,
    });

    await config.copyBackupContent({ path: '/backups/test.json' });

    expect(configService.getBackupContent).toHaveBeenCalledWith('/backups/test.json');
    expect(writeText).toHaveBeenCalledWith('backup content text');
  });
});

describe('useConfigStore — loadBackupToEditor', () => {
  let config;

  beforeEach(() => {
    setActivePinia(createPinia());
    config = useConfigStore();
    vi.clearAllMocks();
  });

  it('loads backup, closes modal, sets editorLoadContent', async () => {
    configService.getBackupContent.mockResolvedValue('{"restored":true}');

    config.backupsModal.show = true;
    config.editorLoadContent = null;

    await config.loadBackupToEditor({
      path: '/backups/01_log_1700000000.json',
    });

    expect(configService.getBackupContent).toHaveBeenCalledWith(
      '/backups/01_log_1700000000.json'
    );
    expect(config.backupsModal.show).toBe(false);
    expect(config.editorLoadContent).toBe('{"restored":true}');
  });
});

describe('useConfigStore — openDiffModal', () => {
  let config;

  beforeEach(() => {
    setActivePinia(createPinia());
    config = useConfigStore();
    vi.clearAllMocks();
  });

  it('computes diff and shows modal when content differs', () => {
    config.openDiffModal('{"a":1}', '{"a":2}');

    expect(config.diffModal.show).toBe(true);
    expect(config.diffModal.diffContent).toBeTypeOf('string');
    expect(config.diffModal.diffContent.length).toBeGreaterThan(0);
  });

  it('does NOT show diff when content equals saved content', () => {
    config.openDiffModal('{"a":1}', '{"a":1}');

    // The function shows a toast and returns early — diffModal stays hidden.
    expect(config.diffModal.show).toBe(false);
  });
});

describe('useConfigStore — closeDiffModal', () => {
  let config;

  beforeEach(() => {
    setActivePinia(createPinia());
    config = useConfigStore();
    vi.clearAllMocks();
  });

  it('clears diff modal state', () => {
    config.diffModal.show = true;
    config.diffModal.diffContent = 'some diff';

    config.closeDiffModal();

    expect(config.diffModal.show).toBe(false);
    expect(config.diffModal.diffContent).toBe('');
  });
});
