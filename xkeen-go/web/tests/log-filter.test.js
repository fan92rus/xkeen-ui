import { describe, it, expect } from 'vitest';
import { filterLogs } from '../src/utils/log-filter.js';

const L = (level, message) => ({ level, message });
const sample = [
  L('error', 'connection refused'),
  L('info', 'started ok'),
  L('warning', 'high memory'),
  L('error', 'timeout'),
  L('info', 'Config loaded'),
];

describe('filterLogs', () => {
  it('returns all logs when level is "all" and no search', () => {
    expect(filterLogs(sample, 'all', '')).toHaveLength(sample.length);
  });

  it('filters by level', () => {
    const r = filterLogs(sample, 'error', '');
    expect(r).toHaveLength(2);
    expect(r.every(l => l.level === 'error')).toBe(true);
  });

  it('search is case-insensitive on the message', () => {
    const r = filterLogs(sample, 'all', 'config');
    expect(r).toHaveLength(1);
    expect(r[0].message).toBe('Config loaded');
  });

  it('combines level filter and search', () => {
    const r = filterLogs(sample, 'error', 'refused');
    expect(r).toHaveLength(1);
    expect(r[0].message).toBe('connection refused');
  });

  it('returns empty when nothing matches', () => {
    expect(filterLogs(sample, 'error', 'nomatch')).toHaveLength(0);
  });

  it('empty logs array returns empty', () => {
    expect(filterLogs([], 'all', '')).toEqual([]);
  });

  it('treats empty string search as no filter', () => {
    expect(filterLogs(sample, 'all', '')).toHaveLength(sample.length);
    expect(filterLogs(sample, 'info', '')).toHaveLength(2);
  });

  it('preserves input order', () => {
    const r = filterLogs(sample, 'all', '');
    expect(r).toEqual(sample);
  });
});
