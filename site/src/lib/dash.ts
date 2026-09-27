// Renders one frame of the wattop dashboard as an HTML string. Shared by the
// Astro page (first frame, server-rendered so the hero works without JS) and
// the client script (every later frame). All values come from the trimmed
// `wattop --demo --json` snapshots in src/data/demo.json (refresh with
// `pnpm demo:refresh`; the mapping lives in scripts/demo-schema.mjs).
//
// Cell text follows the TUI (internal/ui/panel/sessions.go, children.go):
// the static "⠋ Busy" glyph, child rows indented by two spaces per depth
// level rather than drawn with tree lines, a trailing "⇢" on a background
// subagent, and blank child $/HR cells when there is no positive burn.

export type Child = {
  k: 'sa' | 'wf';
  type?: string;
  desc?: string;
  model?: string;
  st: string;
  tool?: string | null;
  depth?: number;
  bg?: boolean;
  id?: string;
  phase?: string;
  agents?: number;
  running?: number;
  done?: number;
  failed?: number;
  partial?: boolean;
  out: number | null;
  cost: number | null;
  burn: number | null;
};

export type Session = {
  agent: string;
  kind: string;
  pid: number | null;
  bind: string;
  cwd: string;
  model: string;
  st: string;
  ctx: number;
  exact: boolean;
  out: number | null;
  cost: number | null;
  partial: boolean;
  burn: number | null;
  tl: number;
  sa: [number, number];
  cpu: number | null;
  gpu: number | null;
  rss: number | null;
  children: Child[];
};

export type Frame = {
  out: number;
  in: number;
  cpu: number;
  sys: {
    e: [number, number, number];
    p: [number, number, number];
    gpu: [number, number, number];
    w: { sys: number; cpu: number; gpu: number; ane: number; dram: number };
    t: [number, number, number];
    fans: [string, number][];
    thermal: number;
    throttled: boolean;
    mem: [number, number, number, number];
    dram: number | null;
    dramEst: boolean;
    net: [number, number];
    disk: [number, number];
  };
  total: { cost: number; burn: number; self: number };
  sessions: Session[];
};

export type Demo = { meta: { source: string; soc: string; synthetic: boolean }; frames: Frame[] };

/** Samples of history each sparkline shows (one per second). */
export const WINDOW = 60;

/**
 * The 25 recorded frames are played forward then backward (0..24..1) so the
 * loop has no jump. `seq(t)` maps a tick to a frame index.
 */
export function seq(t: number, n: number): number {
  const period = 2 * (n - 1);
  const m = ((t % period) + period) % period;
  return m < n ? m : period - m;
}

const esc = (s: string) =>
  s.replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]!);

const f1 = (n: number | null | undefined) => (n == null ? '—' : n.toFixed(1));
const usd = (n: number | null | undefined) => (n == null ? '$—' : `$${n.toFixed(2)}`);

function bytes(n: number, perSec = false): string {
  const u = ['B', 'KB', 'MB', 'GB'];
  let i = 0;
  let v = n;
  while (v >= 1000 && i < u.length - 1) {
    v /= 1000;
    i++;
  }
  return `${v.toFixed(1)} ${u[i]}${perSec ? '/s' : ''}`;
}

function rss(n: number | null): string {
  return n == null ? '—' : `${Math.round(n / 1e6)}M`;
}

const THERMAL = ['Nominal', 'Fair', 'Serious', 'Critical'];
/** sessions.go spinnerGlyph: one static braille frame. */
const SPIN = '⠋';

function trunc(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1) + '…' : s;
}

/** A column sparkline, like ntcharts' block bars. */
function spark(values: number[], max: number, cls: string): string {
  const bars = values
    .map((v) => `<i style="height:${Math.max(2, Math.round((v / (max || 1)) * 100))}%"></i>`)
    .join('');
  return `<div class="spk ${cls}" aria-hidden="true">${bars}</div>`;
}

function gauge(pct: number, cls: string): string {
  const p = Math.max(0, Math.min(100, pct));
  return `<span class="g" aria-hidden="true"><span class="g-f ${cls}" style="width:${p}%"></span></span>`;
}

/** sessions.go ctxGauge: "~" marks Claude's estimate; severity at 70/90%. */
function ctxGauge(pct: number, exact: boolean): string {
  const tone = pct >= 90 ? 'hot' : pct >= 70 ? 'waiting' : 'busy';
  const text = pct > 100 ? '<span class="hot-c">&gt;100%</span>' : `${pct}%`;
  return `<td class="c-ctx"><span class="ctx ${exact ? 'exact' : 'est'}" title="${exact ? 'exact (Codex rate_limits)' : 'estimate (dashed edge)'}"><span class="ctx-m">${exact ? '' : '~'}</span><span class="ctx-g">${gauge(pct, tone)}</span><span class="ctx-n">${text}</span></span></td>`;
}

/** sessions.go statusInfo. */
function status(st: string): string {
  switch (st) {
    case 'busy':
      return `<span class="st busy"><span class="spin" aria-hidden="true">${SPIN}</span> Busy</span>`;
    case 'waiting':
      return '<span class="st waiting">Waiting</span>';
    case 'rate-limited':
      return '<span class="st warn">RateLimited</span>';
    case 'unknown':
      return '<span class="st muted">Unknown</span>';
    case 'stale':
      return '<span class="st muted">Stale</span>';
    default:
      return `<span class="st muted">${esc(st)}</span>`;
  }
}

/** children.go childStatus (prefix "wf " for a workflow row). */
function childStatus(st: string, prefix = ''): string {
  if (st === 'running') return `<span class="st busy">${prefix}● run</span>`;
  if (st === 'idle') return `<span class="st waiting">${prefix}idle</span>`;
  if (st === 'done') return `<span class="st muted">${prefix}done</span>`;
  if (st === 'failed') return `<span class="st hot">${prefix}fail</span>`;
  return `<span class="st muted">${prefix}${esc(st)}</span>`;
}

/** children.go childBurn: blank unless there is a positive burn. */
const childBurn = (n: number | null) => (n == null || n <= 0 ? '' : usd(n));

function sessionRows(s: Session): string {
  const kids = s.children;
  const rows: string[] = [];
  const name = s.cwd.replace(/^~\/code\//, '');
  const bound = s.bind !== 'unknown';
  // pidCell's compact form (the TUI drops "pid " when the column is narrow).
  const pid = bound ? (s.pid == null ? '—' : String(s.pid)) : '—';
  const bgRun = s.kind !== '' && s.kind !== 'interactive';
  const agent = bgRun ? `<span class="muted">${esc(s.agent)}*</span>` : `<span class="ag-${s.agent}">${esc(s.agent)}</span>`;
  const model = esc(trunc(s.model || '—', 16));
  const cost = (s.partial ? '~' : '') + usd(s.cost);
  rows.push(
    `<tr class="s-row">` +
      `<td class="c-st">${status(s.st)}</td>` +
      `<td class="c-pid num">${pid}</td>` +
      `<td class="c-ag">${agent}</td>` +
      `<td class="c-model">${bgRun ? `<span class="muted">${model}</span>` : model}</td>` +
      `<td class="c-cwd"><span class="cwd-full">${esc(s.cwd)}</span><span class="cwd-short">${esc(name)}</span></td>` +
      ctxGauge(s.ctx, s.exact) +
      `<td class="c-out num">${f1(s.out)}</td>` +
      `<td class="c-cost num">${cost}</td>` +
      `<td class="c-burn num">${s.burn == null ? '—' : usd(s.burn)}</td>` +
      `<td class="c-tl num">${s.tl}</td>` +
      `<td class="c-sa num">${s.sa[0]}/${s.sa[1]}</td>` +
      `<td class="c-cpu num">${!bound || s.cpu == null ? '—' : s.cpu.toFixed(1) + '%'}</td>` +
      `<td class="c-gpu num">${bound ? f1(s.gpu) : '—'}</td>` +
      `<td class="c-rss num">${bound ? rss(s.rss) : '—'}</td>` +
      `</tr>`,
  );
  kids.forEach((c) => {
    // childStatusCell: two spaces, plus two per depth level (workflows: 0).
    const indent = `<span class="ind" aria-hidden="true">${'&nbsp;'.repeat(2 + 2 * (c.k === 'wf' ? 0 : c.depth ?? 0))}</span>`;
    let who: string;
    let what: string;
    let st: string;
    if (c.k === 'wf') {
      st = childStatus(c.st, 'wf ');
      who = 'workflow';
      what =
        esc(c.id!.slice(0, 10)) +
        (c.phase ? ` · ${esc(c.phase)}` : '') +
        ` · ${c.running} run ${c.done}/${c.agents} done` +
        (c.failed ? ` ${c.failed} fail` : '');
    } else {
      st = childStatus(c.st);
      // subagentRow: a background spawn keeps its "⇢" and gives up a letter.
      who = c.bg ? esc(trunc(c.type!, 13)) + '⇢' : esc(trunc(c.type!, 14));
      what = (c.tool && c.st === 'running' ? `<span class="tool">▸ ${esc(c.tool)}</span> · ` : '') + esc(c.desc!);
    }
    rows.push(
      `<tr class="c-row">` +
        `<td class="c-st">${indent}${st}</td>` +
        `<td class="c-pid"></td>` +
        `<td class="c-ag muted">${who}</td>` +
        `<td class="c-model muted">${c.model ? esc(trunc(c.model, 16)) : ''}</td>` +
        `<td class="c-cwd c-desc"><span class="desc">${what}</span></td><td class="c-ctx"></td>` +
        `<td class="c-out num">${f1(c.out)}</td>` +
        `<td class="c-cost num">${(c.partial ? '~' : '') + usd(c.cost)}</td>` +
        `<td class="c-burn num">${childBurn(c.burn)}</td>` +
        `<td class="c-tl"></td><td class="c-sa"></td><td class="c-cpu"></td><td class="c-gpu"></td><td class="c-rss"></td>` +
        `</tr>`,
    );
  });
  return rows.join('');
}

function niceMax(v: number): number {
  return Math.ceil(v);
}

/**
 * @param frames all frames
 * @param tick   ever-increasing tick; history is the WINDOW ticks ending here
 */
export function renderDash(frames: Frame[], tick: number): string {
  const n = frames.length;
  const hist: Frame[] = [];
  for (let t = tick - WINDOW + 1; t <= tick; t++) hist.push(frames[seq(t, n)]);
  const f = hist[hist.length - 1];
  const s = f.sys;

  const outV = hist.map((h) => h.out);
  const inV = hist.map((h) => h.in);
  const cpuV = hist.map((h) => h.cpu);
  const gpuV = hist.map((h) => h.sys.gpu[1]);
  const wV = hist.map((h) => h.sys.w.sys);
  const outMax = niceMax(Math.max(...outV));
  const inMax = niceMax(Math.max(...inV));
  const wMax = Math.max(30, Math.ceil(Math.max(...wV) / 10) * 10);

  const [memUsed, memTot, swapUsed, swapTot] = s.mem;
  const memPct = (memUsed / memTot) * 100;
  const thermal = THERMAL[s.thermal] ?? String(s.thermal);

  const throughput = `
  <section class="pnl p-tok">
    <h3 class="pnl-t">Token throughput</h3>
    <div class="spk-h"><span class="out-c">OUT <b>${f.out.toFixed(1)}</b> tok/s</span><span class="rng">0–${outMax}</span></div>
    ${spark(outV, outMax, 'out')}
    <div class="spk-h in-h"><span class="in-c">IN&nbsp; <b>${f.in.toFixed(1)}</b> tok/s</span><span class="rng">0–${inMax}</span></div>
    ${spark(inV, inMax, 'in')}
  </section>`;

  const history = `
  <section class="pnl p-hw">
    <h3 class="pnl-t">Hardware history</h3>
    <div class="spk-h"><span class="cpu-c">CPU % <b>${f.cpu.toFixed(1)}</b></span><span class="rng">0–100</span></div>
    ${spark(cpuV, 100, 'cpu sm')}
    <div class="spk-h"><span class="gpu-c">GPU % <b>${s.gpu[1].toFixed(1)}</b></span><span class="rng">0–100</span></div>
    ${spark(gpuV, 100, 'gpu sm')}
    <div class="spk-h"><span class="w-c">POWER W <b>${s.w.sys.toFixed(1)}</b></span><span class="rng">0–${wMax}</span></div>
    ${spark(wV, wMax, 'w sm')}
  </section>`;

  const compute = `
  <section class="pnl p-cm">
    <h3 class="pnl-t">Compute / memory</h3>
    <dl class="meters">
      <dt>E (${s.e[0]})</dt><dd>${gauge(s.e[1], 'busy')}<span class="num">${s.e[1].toFixed(1)}%</span><span class="num dim">${s.e[2]} MHz</span></dd>
      <dt>P (${s.p[0]})</dt><dd>${gauge(s.p[1], s.p[1] >= 70 ? 'waiting' : 'busy')}<span class="num">${s.p[1].toFixed(1)}%</span><span class="num dim">${s.p[2]} MHz</span></dd>
      <dt>GPU (${s.gpu[0]})</dt><dd>${gauge(s.gpu[1], 'gpu')}<span class="num">${s.gpu[1].toFixed(1)}%</span><span class="num dim">${s.gpu[2]} MHz</span></dd>
      <dt>Memory</dt><dd>${gauge(memPct, 'busy')}<span class="num">${memPct.toFixed(1)}%</span><span></span></dd>
    </dl>
    <p class="kv">Mem  ${(memUsed / 1e9).toFixed(1)}/${(memTot / 1e9).toFixed(1)} GB  Swap ${(swapUsed / 1e9).toFixed(1)}/${(swapTot / 1e9).toFixed(1)} GB</p>
    ${s.dram == null ? '' : `<p class="kv">DRAM Total ${s.dramEst ? '~' : ''}${f1(s.dram)} GB/s${s.dramEst ? ' <span class="dim">(estimate)</span>' : ''}</p>`}
  </section>`;

  const power = `
  <section class="pnl p-pw">
    <h3 class="pnl-t">Power / thermals / I/O</h3>
    <p class="kv"><span class="w-c"><b>${s.w.sys.toFixed(1)} W</b></span> system &nbsp; Thermal <span class="${s.thermal ? 'warn' : 'idle-c'}">${thermal}</span>${s.throttled ? ' <span class="badge-hot">THROTTLED</span>' : ''}</p>
    <p class="kv">CPU ${s.w.cpu.toFixed(1)}W &nbsp;GPU ${s.w.gpu.toFixed(1)}W &nbsp;ANE ${s.w.ane.toFixed(1)}W &nbsp;DRAM ${s.w.dram.toFixed(1)}W</p>
    <p class="kv gap">Temp &nbsp;CPU ${s.t[0].toFixed(1)}°C &nbsp;GPU ${s.t[1].toFixed(1)}°C &nbsp;SOC ${s.t[2].toFixed(1)}°C</p>
    <p class="kv">Fans &nbsp;${s.fans.map(([l, r]) => `${esc(l)} ${r} RPM`).join(' &nbsp;')}</p>
    <p class="kv gap">Net &nbsp;↓ ${bytes(s.net[0], true)} &nbsp;↑ ${bytes(s.net[1], true)}</p>
    <p class="kv">Disk R ${bytes(s.disk[0], true)} &nbsp;W ${bytes(s.disk[1], true)}</p>
  </section>`;

  const table = `
  <section class="pnl p-ss">
    <h3 class="pnl-t">Sessions / ${f.sessions.length}</h3>
    <div class="tbl-wrap">
    <table class="ss">
      <caption class="sr-only">Agent sessions in the synthetic demo, with subagent and workflow rows nested under their parent session.</caption>
      <thead><tr>
        <th class="c-st" scope="col">Status</th><th class="c-pid" scope="col">PID</th><th class="c-ag" scope="col">Agent</th>
        <th class="c-model" scope="col">Model</th><th class="c-cwd" scope="col">CWD</th><th class="c-ctx" scope="col">CTX</th>
        <th class="c-out num" scope="col">OUT/s</th><th class="c-cost num" scope="col">$</th><th class="c-burn num" scope="col">$/HR</th>
        <th class="c-tl num" scope="col">TL</th><th class="c-sa num" scope="col">SA</th><th class="c-cpu num" scope="col">CPU%</th>
        <th class="c-gpu num" scope="col">GPU/s</th><th class="c-rss num" scope="col">RSS</th>
      </tr></thead>
      <tbody>${f.sessions.map((x) => sessionRows(x)).join('')}</tbody>
    </table>
    </div>
  </section>`;

  const foot = `
  <div class="d-foot">
    <p><span>Machine <b class="w-c">${s.w.sys.toFixed(1)}W</b> total</span><span class="sep">|</span><span><b class="hot-c">${usd(f.total.burn)}/hr</b> total</span><span class="sep">|</span><span><b class="cost-c">${usd(f.total.cost)}</b> session total</span><span class="sep">|</span><span class="self">wattop self ${f.total.self.toFixed(1)}% CPU</span></p>
    <p class="dim">Costs are estimates from a pricing table and ignore subscription plans.</p>
  </div>`;

  return `
  <div class="d-row r1">${throughput}${history}</div>
  <p class="d-axis dim" aria-hidden="true">${WINDOW}s ago → now · tok/s: recorded over 60s</p>
  <div class="d-row r2">${compute}${power}</div>
  ${table}
  ${foot}`;
}
