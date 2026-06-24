import { describe, it, expect } from 'vitest';
import { computeTagRates, computeChartData, totalOutboundRates } from '../src/utils/metrics-rates.js';

// ── Helpers ──

/** Build a snapshot with the shape the WebSocket delivers. */
function snap(ts, outbound, inbound) {
  const s = { ts };
  if (outbound !== undefined) s.outbound = outbound;
  if (inbound !== undefined) s.inbound = inbound;
  return s;
}

function tag(dl, ul) {
  return { downlink: dl, uplink: ul };
}

// ── computeTagRates ──

describe('computeTagRates', () => {
  it('returns empty when either snapshot is null/undefined', () => {
    expect(computeTagRates(null, snap(100))).toEqual({ inbound: [], outbound: [] });
    expect(computeTagRates(snap(100), null)).toEqual({ inbound: [], outbound: [] });
  });

  it('returns empty when dt <= 0', () => {
    expect(computeTagRates(snap(100), snap(100))).toEqual({ inbound: [], outbound: [] });
    expect(computeTagRates(snap(90), snap(100))).toEqual({ inbound: [], outbound: [] }); // negative dt
  });

  it('computes per-tag rates for both directions', () => {
    const cur = snap(110, { p1: tag(300, 150), p2: tag(200, 100) }, { p1: tag(500, 200) });
    const prev = snap(100, { p1: tag(200, 100), p2: tag(100, 50) }, { p1: tag(400, 150) });
    const r = computeTagRates(cur, prev);
    expect(r.inbound).toHaveLength(1);
    expect(r.inbound[0]).toEqual({ tag: 'p1', dl: 10, ul: 5 }); // (500-400)/10=10, (200-150)/10=5
    expect(r.outbound).toHaveLength(2);
    expect(r.outbound[0]).toEqual({ tag: 'p1', dl: 10, ul: 5 });
    expect(r.outbound[1]).toEqual({ tag: 'p2', dl: 10, ul: 5 });
  });

  it('clamps negative per-tag rates to 0', () => {
    // p1 counter reset: prev is HIGHER than cur
    const cur = snap(110, { p1: tag(50, 25) });
    const prev = snap(100, { p1: tag(200, 100) });
    const r = computeTagRates(cur, prev);
    expect(r.outbound).toHaveLength(1);
    expect(r.outbound[0].dl).toBeCloseTo(0);
    expect(r.outbound[0].ul).toBeCloseTo(0);
  });

  it('returns empty direction when cur[dir] is missing', () => {
    const cur = snap(110, { p1: tag(200, 100) });  // no inbound
    const prev = snap(100, { p1: tag(100, 50) });
    const r = computeTagRates(cur, prev);
    expect(r.inbound).toEqual([]);
    expect(r.outbound).toHaveLength(1);
  });

  it('returns empty direction when prev[dir] is missing', () => {
    const cur = snap(110, { p1: tag(200, 100) }, { p1: tag(300, 150) });
    const prev = snap(100, { p1: tag(100, 50) });  // no inbound
    const r = computeTagRates(cur, prev);
    expect(r.inbound).toEqual([]);   // prev lacks inbound
    expect(r.outbound).toHaveLength(1);
  });

  it('uses ?? 0 for sub-fields in tag data', () => {
    const cur = snap(110, { p1: { downlink: 200 } });  // missing uplink
    const prev = snap(100, { p1: tag(100, 50) });
    const r = computeTagRates(cur, prev);
    expect(r.outbound[0].dl).toBeCloseTo(10);
    expect(r.outbound[0].ul).toBeCloseTo(0);  // (0-50)/10 clamped to 0
  });
});

// ── computeChartData ──

describe('computeChartData', () => {
  it('returns null for history < 2 entries', () => {
    expect(computeChartData([])).toBeNull();
    expect(computeChartData([snap(1)])).toBeNull();
    expect(computeChartData(null)).toBeNull();
    expect(computeChartData(undefined)).toBeNull();
  });

  it('computes outbound aggregate across two snapshots', () => {
    const hist = [
      snap(100, { p1: tag(100, 50) }),
      snap(110, { p1: tag(200, 100) }),
    ];
    const r = computeChartData(hist);
    expect(r).not.toBeNull();
    expect(r.labels).toHaveLength(1);
    expect(r.dl[0]).toBeCloseTo(10);
    expect(r.ul[0]).toBeCloseTo(5);
  });

  it('only counts tags where cDL >= pDL (counter-reset guard)', () => {
    // p2 counter reset: cDL(50) < pDL(300) → entire tag skipped
    const hist = [
      snap(100, { p1: tag(100, 50), p2: tag(300, 150) }),
      snap(110, { p1: tag(200, 100), p2: tag(50, 25) }),
    ];
    const r = computeChartData(hist);
    expect(r.dl[0]).toBeCloseTo(10);   // only p1 contributes
    expect(r.ul[0]).toBeCloseTo(5);
  });

  it('skips i→i+1 pairs where dt <= 0', () => {
    const hist = [
      snap(100, { p1: tag(100, 50) }),
      snap(100, { p1: tag(120, 60) }), // dt=0 → skipped
      snap(110, { p1: tag(200, 100) }),
    ];
    const r = computeChartData(hist);
    expect(r.labels).toHaveLength(1);  // only the valid interval
    // The valid interval is between hist[2] and hist[1]: ts 110 - 100 = 10
    // ... wait no, actually i=1: dt=hist[1].ts - hist[0].ts = 100-100 = 0 → skipped
    // i=2: dt=hist[2].ts - hist[1].ts = 110-100 = 10
    // prev = hist[1], cur = hist[2]
    // cDL=200, pDL=120, cUL=100, pUL=60
    // dl = (200-120)/10 = 8, ul = (100-60)/10 = 4
    expect(r.dl[0]).toBeCloseTo(8);
    expect(r.ul[0]).toBeCloseTo(4);
  });

  it('returns tDL=0/tUL=0 when cur.outbound is missing', () => {
    const hist = [
      snap(100, { p1: tag(100, 50) }),
      snap(110),  // no outbound
    ];
    const r = computeChartData(hist);
    expect(r.dl[0]).toBeCloseTo(0);
    expect(r.ul[0]).toBeCloseTo(0);
  });

  it('returns tDL=0/tUL=0 when prev.outbound is missing', () => {
    const hist = [
      snap(100),  // no outbound
      snap(110, { p1: tag(200, 100) }),
    ];
    const r = computeChartData(hist);
    expect(r.dl[0]).toBeCloseTo(0);
    expect(r.ul[0]).toBeCloseTo(0);
  });

  it('uses ?? 0 for missing sub-fields preserving cDL >= pDL guard', () => {
    // prev p1 missing uplink → pUL = 0
    const hist = [
      snap(100, { p1: { downlink: 100 } }),   // uplink undefined
      snap(110, { p1: { downlink: 200, uplink: 100 } }),
    ];
    const r = computeChartData(hist);
    expect(r.dl[0]).toBeCloseTo(10);  // (200-100)/10
    // cDL(200) >= pDL(100) → true, so BOTH dl AND ul are added
    // cUL(100) - pUL(??0) = 100/10 = 10
    expect(r.ul[0]).toBeCloseTo(10);
  });

  it('handles a tag that appears only in interval end (new tag)', () => {
    // p2 is not in prev.outbound → pDL=0, pUL=0
    const hist = [
      snap(100, { p1: tag(100, 50) }),
      snap(110, { p1: tag(200, 100), p2: tag(300, 150) }),
    ];
    const r = computeChartData(hist);
    // p1: cDL(200) >= pDL(100) → (200-100)/10=10 dl, (100-50)/10=5 ul
    // p2: prev has no p2 → pDL=0, pUL=0 → cDL(300) >= 0 → (300-0)/10=30 dl, (150-0)/10=15 ul
    expect(r.dl[0]).toBeCloseTo(40);
    expect(r.ul[0]).toBeCloseTo(20);
  });

  it('handles multiple intervals', () => {
    const hist = [
      snap(100, { p1: tag(100, 50) }),
      snap(110, { p1: tag(200, 100) }),
      snap(120, { p1: tag(350, 175) }),
    ];
    const r = computeChartData(hist);
    expect(r.labels).toHaveLength(2);
    expect(r.dl).toHaveLength(2);
    expect(r.ul).toHaveLength(2);
    expect(r.dl[0]).toBeCloseTo(10);   // (200-100)/10
    expect(r.dl[1]).toBeCloseTo(15);   // (350-200)/10
    expect(r.ul[0]).toBeCloseTo(5);    // (100-50)/10
    expect(r.ul[1]).toBeCloseTo(7.5);  // (175-100)/10
  });
});

// ── totalOutboundRates ──

describe('totalOutboundRates', () => {
  it('sums all outbound rates', () => {
    const rates = {
      outbound: [
        { tag: 'p1', dl: 10, ul: 5 },
        { tag: 'p2', dl: 15, ul: 7.5 },
      ],
    };
    expect(totalOutboundRates(rates)).toEqual({ dl: 25, ul: 12.5 });
  });

  it('returns {dl:0,ul:0} for null/undefined input', () => {
    expect(totalOutboundRates(null)).toEqual({ dl: 0, ul: 0 });
    expect(totalOutboundRates(undefined)).toEqual({ dl: 0, ul: 0 });
  });

  it('returns {dl:0,ul:0} when outbound is missing or empty', () => {
    expect(totalOutboundRates({ inbound: [] })).toEqual({ dl: 0, ul: 0 });
    expect(totalOutboundRates({ outbound: [] })).toEqual({ dl: 0, ul: 0 });
  });

  it('handles single entry', () => {
    expect(totalOutboundRates({ outbound: [{ tag: 'p1', dl: 5, ul: 3 }] })).toEqual({ dl: 5, ul: 3 });
  });
});
