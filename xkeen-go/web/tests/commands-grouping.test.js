import { describe, it, expect } from 'vitest';
import { groupCommandsByCategory } from '../src/utils/commands-grouping.js';

const cmd = (c, description, category, dangerous = false) =>
  ({ cmd: c, description, category, dangerous });

describe('groupCommandsByCategory', () => {
  it('groups commands into categories', () => {
    const out = groupCommandsByCategory([
      cmd('-start', 'Запуск', 'Управление прокси-клиентом'),
      cmd('-stop', 'Остановка', 'Управление прокси-клиентом'),
      cmd('-v', 'Версия', 'Информация'),
    ]);
    // 'Управление прокси-клиентом' has higher priority (index 10) than
    // 'Информация' (index 11), so it sorts first.
    expect(out).toHaveLength(2);
    expect(out[0].name).toBe('Управление прокси-клиентом');
    expect(out[0].commands).toHaveLength(2);
    expect(out[1].name).toBe('Информация');
    expect(out[1].commands[0].name).toBe('-v');
  });

  it('sorts categories by logical priority, not first-seen order', () => {
    const out = groupCommandsByCategory([
      cmd('-v', 'Версия', 'Информация'),
      cmd('-start', 'Запуск', 'Управление прокси-клиентом'),
      cmd('-i', 'Установка', 'Установка'),
    ]);
    // Priority order: Установка (0) < Управление (10) < Информация (11)
    expect(out.map(g => g.name)).toEqual([
      'Установка',
      'Управление прокси-клиентом',
      'Информация',
    ]);
  });

  it('sorts commands within a category alphabetically by flag', () => {
    const out = groupCommandsByCategory([
      cmd('-stop', 'Остановка', 'Управление прокси-клиентом'),
      cmd('-restart', 'Перезапуск', 'Управление прокси-клиентом'),
      cmd('-start', 'Запуск', 'Управление прокси-клиентом'),
      cmd('-status', 'Статус', 'Управление прокси-клиентом'),
    ]);
    expect(out).toHaveLength(1);
    expect(out[0].commands.map(c => c.name)).toEqual([
      '-restart',
      '-start',
      '-status',
      '-stop',
    ]);
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
      cmd('-d', 'd', 'G'),
      cmd('-a', 'a', 'G'),
      cmd('-c', 'c', 'G'),
      cmd('-b', 'b', 'G'),
    ]);
    expect(out).toHaveLength(1);
    expect(out[0].commands.map(c => c.name)).toEqual(['-a', '-b', '-c', '-d']);
  });

  it('places unknown categories after known ones, sorted alphabetically', () => {
    const out = groupCommandsByCategory([
      cmd('-zzz', 'z', 'ZZ Unknown'),
      cmd('-aaa', 'a', 'AA Unknown'),
      cmd('-start', 'Запуск', 'Управление прокси-клиентом'),
    ]);
    // Known category first, then unknowns alphabetically (AA < ZZ).
    expect(out.map(g => g.name)).toEqual([
      'Управление прокси-клиентом',
      'AA Unknown',
      'ZZ Unknown',
    ]);
  });
});

describe('groupCommandsByCategory — deterministic order (regression)', () => {
  // Regression: the backend serves commands in map-iteration order which
  // changes every process, making the Commands tab "jump around" on refresh.
  // The grouper must produce a STABLE output regardless of input order.

  const sample = [
    cmd('-start', 'Запуск', 'Управление прокси-клиентом'),
    cmd('-stop', 'Остановка', 'Управление прокси-клиентом'),
    cmd('-restart', 'Перезапуск', 'Управление прокси-клиентом'),
    cmd('-i', 'install', 'Установка'),
    cmd('-io', 'offline', 'Установка'),
    cmd('-remove', 'удалить', 'Удаление'),
    cmd('-v', 'версия', 'Информация'),
  ];

  // Deterministic shuffle helper (Fisher-Yates with a fixed permutation).
  function shuffled(arr, permutation) {
    const out = arr.slice();
    for (let i = out.length - 1; i > 0; i--) {
      const j = permutation[i] % (i + 1);
      [out[i], out[j]] = [out[j], out[i]];
    }
    return out;
  }

  it('produces identical output for multiple input orderings', () => {
    const baseline = JSON.stringify(groupCommandsByCategory(sample));
    // Try several different shuffles of the same input.
    const perms = [
      [3, 1, 4, 1, 5, 9, 2],
      [0, 0, 0, 0, 0, 0, 0],
      [6, 5, 4, 3, 2, 1, 0],
      [2, 4, 6, 1, 3, 5, 0],
    ];
    for (const p of perms) {
      const result = JSON.stringify(groupCommandsByCategory(shuffled(sample, p)));
      expect(result).toBe(baseline);
    }
  });

  it('produces the expected deterministic category + command order', () => {
    const out = groupCommandsByCategory(sample);
    // Categories in priority order: Установка, Удаление, Управление, Информация
    expect(out.map(g => g.name)).toEqual([
      'Установка',
      'Удаление',
      'Управление прокси-клиентом',
      'Информация',
    ]);
    // Commands sorted alphabetically within each category.
    expect(out[0].commands.map(c => c.name)).toEqual(['-i', '-io']);
    expect(out[1].commands.map(c => c.name)).toEqual(['-remove']);
    expect(out[2].commands.map(c => c.name)).toEqual(['-restart', '-start', '-stop']);
    expect(out[3].commands.map(c => c.name)).toEqual(['-v']);
  });
});
