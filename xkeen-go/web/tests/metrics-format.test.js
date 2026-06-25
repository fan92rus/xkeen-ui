import { describe, it, expect } from 'vitest';
import { fmtBytes, fmtRate, fmtRateShort, fmtDelay, fmtTime, fmtTimeShort, fmtDuration, percentile } from '../src/utils/metrics-format.js';

describe('fmtBytes', () => {
	it('zero', () => { expect(fmtBytes(0)).toBe('0 B'); });
	it('negative', () => { expect(fmtBytes(-1)).toBe('0 B'); });
	it('bytes', () => { expect(fmtBytes(500)).toBe('500 B'); });
	it('KB', () => { expect(fmtBytes(2048)).toBe('2.0 KB'); });
	it('MB', () => { expect(fmtBytes(5 * 1024 * 1024)).toBe('5.0 MB'); });
	it('GB', () => { expect(fmtBytes(3 * 1024 * 1024 * 1024)).toBe('3.0 GB'); });
	it('TB', () => { expect(fmtBytes(2 * 1024 * 1024 * 1024 * 1024)).toBe('2.0 TB'); });
});

describe('fmtRate', () => {
	it('zero', () => { expect(fmtRate(0)).toBe('0 B/s'); });
	it('negative', () => { expect(fmtRate(-100)).toBe('0 B/s'); });
	it('B/s', () => { expect(fmtRate(500)).toBe('500 B/s'); });
	it('KB/s', () => { expect(fmtRate(1500)).toBe('1.5 KB/s'); });
	it('MB/s', () => { expect(fmtRate(10 * 1024 * 1024)).toBe('10.0 MB/s'); });
});

describe('fmtRateShort', () => {
	it('zero', () => { expect(fmtRateShort(0)).toBe('0'); });
	it('B', () => { expect(fmtRateShort(500)).toBe('500B'); });
	it('K', () => { expect(fmtRateShort(1500)).toBe('1.5K'); });
	it('M', () => { expect(fmtRateShort(5 * 1024 * 1024)).toBe('5.0M'); });
	it('G', () => { expect(fmtRateShort(3 * 1024 * 1024 * 1024)).toBe('3.0G'); });
});

describe('fmtDelay', () => {
	it('zero', () => { expect(fmtDelay(0)).toBe('—'); });
	it('negative', () => { expect(fmtDelay(-5)).toBe('—'); });
	it('ms', () => { expect(fmtDelay(150)).toBe('150 ms'); });
	it('seconds', () => { expect(fmtDelay(2500)).toBe('2.5 s'); });
});

describe('fmtTime', () => {
	it('formats unix timestamp to HH:MM:SS', () => {
		const ts = 1700000000; // known timestamp
		const result = fmtTime(ts);
		expect(result).toMatch(/^\d{2}:\d{2}:\d{2}$/);
	});
});

describe('fmtTimeShort', () => {
	it('formats unix timestamp to MM:SS', () => {
		const ts = 1700000000;
		const result = fmtTimeShort(ts);
		expect(result).toMatch(/^\d{2}:\d{2}$/);
	});
});

describe('fmtDuration', () => {
	it('seconds only', () => { expect(fmtDuration(45)).toBe('45с'); });
	it('minutes and seconds', () => { expect(fmtDuration(125)).toBe('2м 5с'); });
	it('hours and minutes', () => { expect(fmtDuration(7500)).toBe('2ч 5м'); });
	it('zero', () => { expect(fmtDuration(0)).toBe('0с'); });
});

describe('percentile', () => {
	it('empty array', () => { expect(percentile([], 95)).toBe(0); });
	it('single element', () => { expect(percentile([42], 95)).toBe(42); });
	it('p50 of sorted data', () => { expect(percentile([1, 2, 3, 4, 5], 50)).toBe(3); });
	it('p95 of sorted data', () => { expect(percentile([1, 2, 3, 4, 100], 95)).toBe(100); });
	it('p100 returns max', () => { expect(percentile([1, 2, 3, 4, 5], 100)).toBe(5); });
});
