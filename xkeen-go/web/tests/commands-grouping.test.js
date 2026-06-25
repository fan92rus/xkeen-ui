import { describe, it, expect } from 'vitest';
import { groupCommandsByCategory } from '../src/utils/commands-grouping.js';

const cmd = (c, description, category, dangerous = false) =>
  ({ cmd: c, description, category, dangerous });

describe('groupCommandsByCategory', () => {
  it('groups commands into categories', () => {
    const out = groupCommandsByCategory([
      cmd('-start', 'Запуск', 'Управление'),
      cmd('-stop', 'Остановка', 'Управление'),
      cmd('-v', 'Версия', 'Информация'),
    ]);
    expect(out).toHaveLength(2);
    expect(out[0].name).toBe('Управление');
    expect(out[0].commands).toHaveLength(2);
    expect(out[1].name).toBe('Информация');
    expect(out[1].commands[0].name).toBe('-v');
  });

  it('preserves first-seen category order', () => {
    const out = groupCommandsByCategory([
      cmd('-b', 'B', 'Beta'),
      cmd('-a', 'A', 'Alpha'),
      cmd('-b2', 'B2', 'Beta'),
    ]);
    expect(out.map(g => g.name)).toEqual(['Beta', 'Alpha']);
    // Second Beta command appended to the existing Beta group.
    expect(out[0].commands).toHaveLength(2);
  });

  it('maps cmd→name and keeps description + dangerous', () => {
    const out = groupCommandsByCategory([
      cmd('-i', 'install', 'Установка', true),
      cmd('-v', 'version', 'Информация', false),
    ]);
    const install = out[0].commands[0];
    expect(install).toEqual({ name: '-i', description: 'install', dangerous: true });
    const ver = out[1].commands[0];
    expect(ver.dangerous).toBe(false);
  });

  it('normalizes missing dangerous flag to false', () => {
    const out = groupCommandsByCategory([
      { cmd: '-x', description: 'd', category: 'C' }, // no dangerous field
    ]);
    expect(out[0].commands[0].dangerous).toBe(false);
  });

  it('lumps commands with no category under "Прочее"', () => {
    const out = groupCommandsByCategory([
      { cmd: '-x', description: 'd', category: '', dangerous: false },
      { cmd: '-y', description: 'e', dangerous: false }, // category undefined
    ]);
    expect(out).toHaveLength(1);
    expect(out[0].name).toBe('Прочее');
    expect(out[0].commands).toHaveLength(2);
  });

  it('returns empty array for empty/null input', () => {
    expect(groupCommandsByCategory([])).toEqual([]);
    expect(groupCommandsByCategory(null)).toEqual([]);
    expect(groupCommandsByCategory(undefined)).toEqual([]);
  });

  it('handles a single category with many commands', () => {
    const out = groupCommandsByCategory([
      cmd('-a', 'a', 'G'),
      cmd('-b', 'b', 'G'),
      cmd('-c', 'c', 'G'),
      cmd('-d', 'd', 'G'),
    ]);
    expect(out).toHaveLength(1);
    expect(out[0].commands.map(c => c.name)).toEqual(['-a', '-b', '-c', '-d']);
  });
});
