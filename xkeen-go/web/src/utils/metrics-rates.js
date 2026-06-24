/**
 * Pure rate-math functions extracted from MetricsTab.vue.
 *
 * These functions compute traffic rates (bytes/sec) from repeated snapshots
 * of cumulative byte counters (downlink/uplink per proxy tag). They preserve
 * the EXACT semantics of the original Vue computed properties so that the
 * component can delegate its rate math here without behavioural change.
 */

/**
 * Compute aggregate { dl, ul } for one direction object pair.
 * Clamps per-tag rate to ≥ 0 (matching tagRates behaviour).
 * Guards dt ≤ 0 and missing prev → returns { dl: 0, ul: 0 }.
 *
 * @param {Object|null|undefined} curDir  current snapshot's direction map
 * @param {Object|null|undefined} prevDir previous snapshot's direction map
 * @param {number} dt elapsed seconds between snapshots
 * @returns {{ dl: number, ul: number }}
 */
export function computeSnapRates(curDir, prevDir, dt) {
  let dl = 0, ul = 0;
  if (!curDir || !prevDir || dt <= 0) return { dl, ul };
  for (const tag of Object.keys(curDir)) {
    const cDL = curDir[tag]?.downlink ?? 0;
    const pDL = prevDir[tag]?.downlink ?? 0;
    const cUL = curDir[tag]?.uplink ?? 0;
    const pUL = prevDir[tag]?.uplink ?? 0;
    dl += Math.max(0, (cDL - pDL) / dt);
    ul += Math.max(0, (cUL - pUL) / dt);
  }
  return { dl, ul };
}

/**
 * Compute per-tag rates for both directions between two snapshots.
 * Mirrors the tagRates computed property EXACTLY:
 * - iterates 'inbound' and 'outbound' directions
 * - clamps each tag's rate to ≥ 0 via Math.max(0, …)
 * - guards dt ≤ 0, null/undefined snapshots, or missing direction → empty
 *
 * @param {{ts:number, inbound?, outbound?}|null|undefined} curSnap
 * @param {{ts:number, inbound?, outbound?}|null|undefined} prevSnap
 * @returns {{ inbound: {tag:string,dl:number,ul:number}[], outbound: {tag:string,dl:number,ul:number}[] }}
 */
export function computeTagRates(curSnap, prevSnap) {
  if (!curSnap || !prevSnap) return { inbound: [], outbound: [] };
  const dt = curSnap.ts - prevSnap.ts;
  if (dt <= 0) return { inbound: [], outbound: [] };
  const result = { inbound: [], outbound: [] };
  for (const dir of ['inbound', 'outbound']) {
    const curDir = curSnap[dir];
    const prevDir = prevSnap[dir];
    if (curDir && prevDir) {
      for (const tag of Object.keys(curDir)) {
        const dl = Math.max(0, ((curDir[tag]?.downlink ?? 0) - (prevDir[tag]?.downlink ?? 0)) / dt);
        const ul = Math.max(0, ((curDir[tag]?.uplink ?? 0) - (prevDir[tag]?.uplink ?? 0)) / dt);
        result[dir].push({ tag, dl, ul });
      }
    }
  }
  return result;
}

/**
 * Format a Unix-epoch-seconds timestamp as a short clock label.
 * Mirrors the component's fmtTimeShort EXACTLY: ru-RU locale, minute:second
 * only (e.g. "05:30"). Kept locale-dependent to preserve original rendering.
 *
 * @param {number} ts seconds since Unix epoch
 * @returns {string}
 */
function fmtTimeLabel(ts) {
  return new Date(ts * 1000).toLocaleTimeString('ru-RU', { minute: '2-digit', second: '2-digit' });
}

/**
 * Compute chart-ready time-series from a history array of snapshots.
 *
 * Mirrors the chartData computed property EXACTLY:
 * - processes the **outbound** direction only (real proxy traffic)
 * - for each tag, only counts intervals where cDL ≥ pDL (counter-reset guard)
 * - skips i→i+1 pairs where dt ≤ 0
 * - aggregator does NOT clamp individual tags (only skips via cDL≥pDL)
 *
 * @param {{ts:number, outbound?}[]|null|undefined} history
 * @returns {{ labels:string[], dl:number[], ul:number[] } | null}
 */
export function computeChartData(history) {
  if (!history || history.length < 2) return null;
  const labels = [];
  const dl = [], ul = [];

  for (let i = 1; i < history.length; i++) {
    const prev = history[i - 1], cur = history[i];
    const dt = cur.ts - prev.ts;
    if (dt <= 0) continue;
    labels.push(fmtTimeLabel(cur.ts));

    let tDL = 0, tUL = 0;
    if (cur.outbound && prev.outbound) {
      for (const tag of Object.keys(cur.outbound)) {
        const cDL = cur.outbound[tag]?.downlink ?? 0, pDL = prev.outbound[tag]?.downlink ?? 0;
        const cUL = cur.outbound[tag]?.uplink ?? 0, pUL = prev.outbound[tag]?.uplink ?? 0;
        if (cDL >= pDL) { tDL += (cDL - pDL) / dt; tUL += (cUL - pUL) / dt; }
      }
    }
    dl.push(tDL); ul.push(tUL);
  }
  return { labels, dl, ul };
}

/**
 * Sum outbound rates across all tags into a single { dl, ul } aggregate.
 * Mirrors the totalRates computed property EXACTLY.
 *
 * @param {{ outbound?: {tag:string,dl:number,ul:number}[] }|null|undefined} tagRates
 * @returns {{ dl: number, ul: number }}
 */
export function totalOutboundRates(tagRates) {
  let dl = 0, ul = 0;
  if (!tagRates || !tagRates.outbound) return { dl, ul };
  for (const r of tagRates.outbound) {
    dl += r.dl;
    ul += r.ul;
  }
  return { dl, ul };
}
