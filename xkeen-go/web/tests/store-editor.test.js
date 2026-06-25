/**
 * Tests for Pinia store config/editor integration.
 *
 * Covers: loadFile stores `modified`, saveFile passes expected_modified,
 * 409 reload, post-save content sync, and selectBackup diff base.
 */
// @vitest-environment happy-dom
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { useAppStore } from '../src/stores/app.js';

// Mock config service so we can control loadFile/saveFile responses
import * as configService from '../src/services/config.js';

vi.mock('../src/services/config.js', () => {
  const mock = {
    listFiles: vi.fn().mockResolvedValue([]),
    getFile: vi.fn(),
    saveFile: vi.fn(),
    getBackups: vi.fn().mockResolvedValue([]),
    getBackupContent: vi.fn().mockResolvedValue(''),
  };
  return { ...mock, default: mock };
});

// Mock other store dependencies (not under test)
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
  getModeInfo: vi.fn().mockResolvedValue({ xray_available: true, mihomo_available: false, mode: 'xray' }),
  setMode: vi.fn(),
}));

describe('app store — config/editor integration', () => {
  let app;

  beforeEach(() => {
    setActivePinia(createPinia());
    app = useAppStore();
    vi.clearAllMocks();
  });

  describe('loadFile', () => {
    it('stores `modified` from the response', async () => {
      configService.getFile.mockResolvedValue({
        path: '/opt/etc/xkeen/05_routing.json',
        content: '{"routing":{"rules":[]}}',
        valid: true,
        modified: 1718971200,
      });

      await app.loadFile('/opt/etc/xkeen/05_routing.json');

      expect(app.currentFile).not.toBeNull();
      expect(app.currentFile.path).toBe('/opt/etc/xkeen/05_routing.json');
      expect(app.currentFile.content).toBe('{"routing":{"rules":[]}}');
      expect(app.currentFile.valid).toBe(true);
      expect(app.currentFile.modified).toBe(1718971200);
    });

    it('stores lastSavedContent from loadFile', async () => {
      configService.getFile.mockResolvedValue({
        path: '/opt/etc/xkeen/01_log.json',
        content: '{"log":{"level":"info"}}',
        valid: true,
        modified: 1000,
      });

      await app.loadFile('/opt/etc/xkeen/01_log.json');
      expect(app.lastSavedContent).toBe('{"log":{"level":"info"}}');
    });
  });

  describe('saveFile', () => {
    beforeEach(async () => {
      // Pre-load a file so currentFile is set
      configService.getFile.mockResolvedValue({
        path: '/opt/etc/xkeen/04_outbounds.json',
        content: '{"outbounds":[{"tag":"proxy"}]}',
        valid: true,
        modified: 2000,
      });
      await app.loadFile('/opt/etc/xkeen/04_outbounds.json');
      vi.clearAllMocks();
    });

    it('passes expected_modified from currentFile.modified', async () => {
      configService.saveFile.mockResolvedValue({ modified: 2001 });

      const result = await app.saveFile('{"outbounds":[{"tag":"proxy"}]}');

      expect(result).toBe(true);
      expect(configService.saveFile).toHaveBeenCalledWith(
        '/opt/etc/xkeen/04_outbounds.json',
        '{"outbounds":[{"tag":"proxy"}]}',
        2000
      );
    });

    it('updates currentFile.content and currentFile.modified after success', async () => {
      configService.saveFile.mockResolvedValue({ modified: 2001 });

      await app.saveFile('{"outbounds":[{"tag":"proxy-v2"}]}');

      expect(app.currentFile.content).toBe('{"outbounds":[{"tag":"proxy-v2"}]}');
      expect(app.currentFile.modified).toBe(2001);
      expect(app.lastSavedContent).toBe('{"outbounds":[{"tag":"proxy-v2"}]}');
    });

    it('updates modified from response even if modified is 0 (valid epoch)', async () => {
      configService.saveFile.mockResolvedValue({ modified: 0 });

      await app.saveFile('{"outbounds":[{"tag":"proxy"}]}');

      expect(app.currentFile.modified).toBe(0);
    });

    it('reloads the file on 409 conflict and returns false', async () => {
      const conflictErr = new Error('Conflict');
      conflictErr.status = 409;
      configService.saveFile.mockRejectedValueOnce(conflictErr);

      // After reload, loadFile will be called again
      configService.getFile.mockResolvedValue({
        path: '/opt/etc/xkeen/04_outbounds.json',
        content: '{"outbounds":[{"tag":"proxy-conflict"}]}',
        valid: true,
        modified: 2099,
      });

      const result = await app.saveFile('{"outbounds":[{"tag":"proxy"}]}');

      expect(result).toBe(false);
      // Should have reloaded the file
      expect(configService.getFile).toHaveBeenCalledWith('/opt/etc/xkeen/04_outbounds.json');
      expect(app.currentFile.content).toBe('{"outbounds":[{"tag":"proxy-conflict"}]}');
      expect(app.currentFile.modified).toBe(2099);
    });

    it('does NOT reload on non-409 errors', async () => {
      const genericErr = new Error('Network error');
      genericErr.status = 500;
      configService.saveFile.mockRejectedValueOnce(genericErr);

      const result = await app.saveFile('{"outbounds":[{"tag":"proxy"}]}');

      expect(result).toBe(false);
      // loadFile should NOT have been called again
      expect(configService.getFile).not.toHaveBeenCalled();
    });
  });

  describe('selectBackup', () => {
    it('uses lastSavedContent as diff base (not currentFile.content)', async () => {
      // Setup: pre-loaded file
      configService.getFile.mockResolvedValue({
        path: '/test/file.json',
        content: 'original',
        valid: true,
        modified: 100,
      });
      await app.loadFile('/test/file.json');
      vi.clearAllMocks();

      // Simulate unsaved edits in currentFile.content
      app.currentFile.content = 'unsaved edits';

      // Mock backup content
      configService.getBackupContent.mockResolvedValue('backup content');

      // Select a backup
      await app.selectBackup({ path: '/backups/file_1680000000.json' });

      // The diff should be between lastSavedContent ('original') and backup,
      // NOT between unsaved edits ('unsaved edits') and backup
      expect(configService.getBackupContent).toHaveBeenCalledWith('/backups/file_1680000000.json');
      expect(app.backupsModal.diffContent).toBeTypeOf('string');
    });
  });
});
