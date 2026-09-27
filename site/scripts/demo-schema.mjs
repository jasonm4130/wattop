// The one place that maps a raw `wattop --demo --json` Snapshot onto the
// trimmed frame src/lib/dash.ts renders. scripts/trim-demo.mjs runs it, and
// src/lib/demo-schema.test.ts checks both ends: SOURCE_FIELDS against the Go
// JSON tags in internal/domain/*.go, and the committed src/data/demo.json
// against the fields dash.ts reads.
//
// Row rules mirror internal/ui/panel/children.go (ChildRows): subagents that
// belong to a workflow get no row of their own, depth counts parent_id hops,
// and workflow rows follow the plain subagents.

/**
 * Every raw Snapshot field trimFrame reads, as a JSON path ("[]" marks an
 * array element). Keys of Go maps (sys.temps.cpu etc.) are listed after a
 * "{}" segment: they are map keys, not struct tags. Keep this list in step
 * with trimFrame below; the test fails if a name here stops being a JSON tag.
 */
export const SOURCE_FIELDS = [
  'token_rate.output_per_sec',
  'token_rate.input_per_sec',
  'total_cost_usd',
  'total_burn_usd_per_hr',
  'self_cpu_pct',
  'sys.soc_name',
  'sys.clusters[].core_count',
  'sys.clusters[].active_pct',
  'sys.clusters[].freq_mhz',
  'sys.gpu.core_count',
  'sys.gpu.active_pct',
  'sys.gpu.freq_mhz',
  'sys.power.system_watts',
  'sys.power.cpu_watts',
  'sys.power.gpu_watts',
  'sys.power.ane_watts',
  'sys.power.dram_watts',
  'sys.temps{}cpu',
  'sys.temps{}gpu',
  'sys.temps{}soc',
  'sys.fans[].label',
  'sys.fans[].rpm',
  'sys.thermal_state',
  'sys.throttled',
  'sys.memory.used_bytes',
  'sys.memory.total_bytes',
  'sys.memory.swap_used_bytes',
  'sys.memory.swap_total_bytes',
  'sys.bandwidth.dram_combined_gbs',
  'sys.bandwidth.dram_estimated',
  'sys.net.in_bytes_per_sec',
  'sys.net.out_bytes_per_sec',
  'sys.disk.read_bytes_per_sec',
  'sys.disk.write_bytes_per_sec',
  'sessions[].agent',
  'sessions[].kind',
  'sessions[].pid',
  'sessions[].bind_conf',
  'sessions[].cwd',
  'sessions[].model',
  'sessions[].status',
  'sessions[].priced',
  'sessions[].cost_partial',
  'sessions[].context_used',
  'sessions[].context_max',
  'sessions[].context_exact',
  'sessions[].token_rate.output_per_sec',
  'sessions[].cost_usd',
  'sessions[].burn_usd_per_hr',
  'sessions[].tools',
  'sessions[].proc.cpu_pct',
  'sessions[].proc.gpu_ms_per_sec',
  'sessions[].proc.rss_bytes',
  'sessions[].subagents[].id',
  'sessions[].subagents[].parent_id',
  'sessions[].subagents[].workflow_id',
  'sessions[].subagents[].agent_type',
  'sessions[].subagents[].description',
  'sessions[].subagents[].model',
  'sessions[].subagents[].status',
  'sessions[].subagents[].live',
  'sessions[].subagents[].current_tool',
  'sessions[].subagents[].background',
  'sessions[].subagents[].token_rate.output_per_sec',
  'sessions[].subagents[].cost_usd',
  'sessions[].subagents[].burn_usd_per_hr',
  'sessions[].workflows[].id',
  'sessions[].workflows[].phase',
  'sessions[].workflows[].status',
  'sessions[].workflows[].agents',
  'sessions[].workflows[].running',
  'sessions[].workflows[].done',
  'sessions[].workflows[].failed',
  'sessions[].workflows[].token_rate.output_per_sec',
  'sessions[].workflows[].cost_usd',
  'sessions[].workflows[].cost_partial',
  'sessions[].workflows[].burn_usd_per_hr',
];

const r = (n, d = 1) => (n == null ? null : Math.round(n * 10 ** d) / 10 ** d);

/** childStatus's fallback: an empty status reads from `live`. */
const childSt = (status, live) => status || (live ? 'running' : 'done');

/** subagentDepth: parent_id hops to a root within the session's subagents. */
function depthOf(subs, i) {
  const byID = new Map(subs.map((s, j) => [s.id, j]));
  let depth = 0;
  for (let hops = 0; hops < subs.length; hops++) {
    const p = subs[i].parent_id;
    if (!p) break;
    const j = byID.get(p);
    if (j === undefined || j === i) break;
    depth++;
    i = j;
  }
  return depth;
}

function children(x) {
  const subs = x.subagents || [];
  const rows = [];
  subs.forEach((a, i) => {
    if (a.workflow_id) return;
    rows.push({
      k: 'sa',
      type: a.agent_type,
      desc: a.description,
      model: a.model,
      st: childSt(a.status, a.live),
      tool: a.current_tool || null,
      depth: depthOf(subs, i),
      bg: !!a.background,
      out: r(a.token_rate?.output_per_sec),
      cost: r(a.cost_usd, 2),
      burn: r(a.burn_usd_per_hr, 2),
    });
  });
  for (const w of x.workflows || []) {
    rows.push({
      k: 'wf',
      id: w.id,
      phase: w.phase || '',
      st: childSt(w.status, false),
      agents: w.agents,
      running: w.running,
      done: w.done,
      failed: w.failed || 0,
      out: r(w.token_rate?.output_per_sec),
      cost: r(w.cost_usd, 2),
      partial: !!w.cost_partial,
      burn: r(w.burn_usd_per_hr, 2),
    });
  }
  return rows;
}

export function trimFrame(f) {
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
      dramEst: !!s.bandwidth.dram_estimated,
      net: [s.net.in_bytes_per_sec, s.net.out_bytes_per_sec],
      disk: [s.disk.read_bytes_per_sec, s.disk.write_bytes_per_sec],
    },
    total: { cost: r(f.total_cost_usd, 2), burn: r(f.total_burn_usd_per_hr, 2), self: r(f.self_cpu_pct) },
    sessions: f.sessions.map((x) => ({
      agent: x.agent,
      kind: x.kind,
      pid: x.pid,
      bind: x.bind_conf,
      cwd: x.cwd,
      model: x.model,
      st: x.status,
      ctx: x.context_max > 0 ? r((x.context_used / x.context_max) * 100, 0) : 0,
      exact: !!x.context_exact,
      out: r(x.token_rate?.output_per_sec),
      cost: x.priced ? r(x.cost_usd, 2) : null,
      partial: !!x.cost_partial,
      burn: r(x.burn_usd_per_hr, 2),
      tl: (x.tools || []).length,
      // SA column: running/total over every subagent, workflow agents included.
      sa: [(x.subagents || []).filter((a) => childSt(a.status, a.live) === 'running').length, (x.subagents || []).length],
      cpu: r(x.proc?.cpu_pct),
      gpu: x.proc?.gpu_ms_per_sec ?? null,
      rss: x.proc?.rss_bytes ?? null,
      children: children(x),
    })),
  };
}
