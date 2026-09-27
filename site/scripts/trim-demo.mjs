// Trims consecutive `wattop --demo --json` snapshots (NDJSON) down to the
// fields the landing-page dashboard renders, and writes src/data/demo.json.
//
//   pnpm trim-demo path/to/demo-frames.ndjson
//
// The input is synthetic data from `wattop --demo`; nothing here is invented,
// only selected and rounded.
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const src = process.argv[2];
if (!src) {
  console.error('usage: node scripts/trim-demo.mjs <demo-frames.ndjson>');
  process.exit(1);
}
const here = dirname(fileURLToPath(import.meta.url));
const out = resolve(here, '../src/data/demo.json');

const r = (n, d = 1) => (n == null ? null : Math.round(n * 10 ** d) / 10 ** d);

const frames = readFileSync(src, 'utf8')
  .split('\n')
  .filter(Boolean)
  .map((l) => JSON.parse(l));

const childRow = (a) => ({
  k: 'sa',
  type: a.agent_type,
  desc: a.description,
  model: a.model,
  st: a.status,
  tool: a.current_tool || null,
  depth: a.spawn_depth,
  bg: a.background,
  out: r(a.token_rate?.output_per_sec),
  cost: r(a.cost_usd, 2),
  burn: r(a.burn_usd_per_hr, 2),
});

const wfRow = (w) => ({
  k: 'wf',
  id: w.id,
  phase: w.phase,
  st: w.status,
  agents: w.agents,
  running: w.running,
  done: w.done,
  out: r(w.token_rate?.output_per_sec),
  cost: r(w.cost_usd, 2),
  burn: r(w.burn_usd_per_hr, 2),
});

const trimmed = frames.map((f) => {
  const s = f.sys;
  const [e, p] = s.clusters;
  const cores = e.core_count + p.core_count;
  return {
    out: r(f.token_rate.output_per_sec),
    in: r(f.token_rate.input_per_sec),
    cpu: r((e.active_pct * e.core_count + p.active_pct * p.core_count) / cores),
    sys: {
      e: [e.core_count, r(e.active_pct), e.freq_mhz],
      p: [p.core_count, r(p.active_pct), p.freq_mhz],
      gpu: [s.gpu.core_count, r(s.gpu.active_pct), s.gpu.freq_mhz],
      w: {
        sys: r(s.power.system_watts),
        cpu: r(s.power.cpu_watts),
        gpu: r(s.power.gpu_watts),
        ane: r(s.power.ane_watts),
        dram: r(s.power.dram_watts),
      },
      t: [r(s.temps.cpu), r(s.temps.gpu), r(s.temps.soc)],
      fans: s.fans.map((x) => [x.label, x.rpm]),
      thermal: s.thermal_state,
      throttled: s.throttled,
      mem: [s.memory.used_bytes, s.memory.total_bytes, s.memory.swap_used_bytes, s.memory.swap_total_bytes],
      dram: r(s.bandwidth.dram_combined_gbs),
      net: [s.net.in_bytes_per_sec, s.net.out_bytes_per_sec],
      disk: [s.disk.read_bytes_per_sec, s.disk.write_bytes_per_sec],
    },
    total: { cost: r(f.total_cost_usd, 2), burn: r(f.total_burn_usd_per_hr, 2), self: r(f.self_cpu_pct) },
    sessions: f.sessions.map((x) => ({
      agent: x.agent,
      kind: x.kind,
      pid: x.pid,
      cwd: x.cwd,
      model: x.model,
      st: x.status,
      ctx: x.context_max ? r((x.context_used / x.context_max) * 100, 0) : null,
      exact: !!x.context_exact,
      out: r(x.token_rate?.output_per_sec),
      cost: r(x.cost_usd, 2),
      burn: r(x.burn_usd_per_hr, 2),
      tl: (x.tools || []).length,
      cpu: r(x.proc?.cpu_pct),
      gpu: x.proc?.gpu_ms_per_sec ?? null,
      rss: x.proc?.rss_bytes ?? null,
      children: [...(x.subagents || []).map(childRow), ...(x.workflows || []).map(wfRow)],
    })),
  };
});

const meta = {
  source: 'wattop --demo --json (v0.3.0), 25 consecutive snapshots at 1s',
  soc: frames[0].sys.soc_name,
  synthetic: true,
};

mkdirSync(dirname(out), { recursive: true });
const json = JSON.stringify({ meta, frames: trimmed });
writeFileSync(out, json);
console.log(`wrote ${out} (${(json.length / 1024).toFixed(1)} KB, ${trimmed.length} frames)`);
