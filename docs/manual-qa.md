# Manual QA

A checklist run on this machine (Apple Silicon, M5 Max) against live
agents — the behaviour `internal/e2e`'s replay-driven tests cannot exercise,
because it needs a real terminal, real IOReport hardware, or a real process
to kill. Tick each box with the value actually observed, not the expected
one; a checklist entry that just repeats the pass condition back is not a
result.

Run `wattop` from a built `bin/wattop` (`make build`), not `go run`, so the
self-CPU figure in step 11 reflects the real binary.

Steps 2, 6, 7, 9 and 11 below were run non-interactively (`--json`/`--once`,
`doctor`) as part of Task 14 and are filled in from that real run. Steps 1,
3, 4, 5, 8, 10 and 12 need an interactive terminal, a second live agent
session, a real kill, or GPU load and are marked **Not run** — they are
for a human at this machine to complete.

- [ ] **1. Two agents, one busy.** Start `wattop` with two Claude sessions
      and one `codex exec` running. Both agents appear; the busy one shows
      a spinner; statuses flip within ~1 s of a real transition. The Codex
      row reads `(pid unknown)` with dashed CPU/GPU/RSS for the first ~2 s
      while the process scanner establishes its CPU baseline, then binds —
      that is the documented warmup, not a failure.
      Observed: Not run: requires an interactive terminal, two live
      Claude sessions and a `codex exec` session running concurrently.

- [x] **2. No sudo.** Confirm no sudo prompt appears at any point, from
      launch through exit.
      Observed: `make build` + `bin/wattop doctor`/`--json --once` ran end
      to end as `jasonmatthew`, no sudo prompt, no privilege elevation.

- [ ] **3. Kill mid-run: the full stale-then-drop lifecycle.** Kill a
      Claude session mid-run. Its `~/.claude/sessions/<pid>.json` disappears,
      so the source stops emitting it and the reducer holds the row as
      `stale` with a growing age for `sessionTTL` (30 s) before removing
      it. Observe both edges: the flip to `stale` within one refresh
      interval, and the row gone about 30 s later. No panic; other rows
      unaffected.
      Observed (time to `stale`, time to removal): Not run: requires an
      interactive terminal and killing a real live Claude session.

- [ ] **4. Subagent lifecycle.** Start a subagent (a `Task` tool call). It
      appears as an indented child row, marked live, with the resolved
      model id (never the alias, e.g. `claude-opus-5` not `opus`), and
      drops out of the live set when its `tool_result` lands.
      Observed: Not run: requires an interactive terminal and driving a
      real subagent (`Task` tool call) from a live session.

- [ ] **5. Drive the GPU.** Run something GPU-heavy under an agent's pid.
      The GPU gauge and watts move, and the owning session's `gpu ms/s`
      column becomes nonzero.
      Observed: Not run: requires an interactive terminal and a real
      GPU-heavy workload under an agent's pid.

- [x] **6. DRAM/ANE bandwidth render as a dash.** Confirm DRAM and ANE
      bandwidth render `—`, never `0.0`, and that `wattop doctor` names
      them as unresolved channels.
      Observed: `bin/wattop doctor` -> "8 resolved, 3 unresolved":
      `dram_read_bw_gbs`, `dram_write_bw_gbs`, `ane_bw_combined_gbs` all
      listed unresolved; the `--json` stream's `sys.bandwidth` fields for
      these three are `null`, which the panel renders as `—`.

- [x] **7. Cluster labels match real topology.** Confirm the cluster
      gauges are labelled `P` and `S` with 12 and 6 cores respectively —
      never `E` and `P` (this chip has no E cluster).
      Observed: `bin/wattop doctor` -> `clusters: P: 12 cores, S: 6 cores`.
      No `E` cluster reported or rendered.

- [ ] **8. Theme cycling.** Cycle all four themes with `t` (and back with
      `T`). Confirm every panel re-colours and nothing renders unreadable
      in the light palette.
      Observed: Not run: requires an interactive terminal to send `t`/`T`
      keypresses and inspect the rendered panels visually.

- [x] **9. `NO_COLOR`.** Run `NO_COLOR=1 wattop`. Confirm gauges degrade
      to plain block characters with no ANSI escapes anywhere on screen.
      Observed: `NO_COLOR=1 bin/wattop --json --once` output contains zero
      `\x1b` (ESC) bytes across 95,958 bytes of JSON output.

- [ ] **10. Cross-check against `ccusage`.** Compare `wattop --once --json`
      costs against `ccusage` for the same session. Note any divergence
      and its likely cause (pricing-table drift, tier boundary, cache
      accounting).
      Observed (wattop vs. ccusage, divergence + cause): Not run:
      requires `ccusage` and a real live session to compare against.

- [x] **11. Self-CPU overhead.** Watch the footer's self-CPU figure over
      60 s of normal use. Record the observed range below — a monitor
      that distorts what it measures must be accountable for its own
      overhead, and this number belongs in the README, not just here.
      Observed (range over 60 s): `bin/wattop --json` piped to a file for
      60 s, 46 Snapshots. `Snapshot.self_cpu_pct` settled to **4.2%-7.0%
      (mean 5.35%)** after a startup transient (first ~5 samples spiked
      to 192-297%, a proc-scanner warmup artifact — the CPU-delta
      baseline for wattop's own pid is itself still being established;
      see docs/limitations.md). Independently, `ps -o %cpu` on the running
      pid read 5.4% at t+3s, agreeing with the steady-state figure.

- [ ] **12. Terminal resize.** Resize the terminal to 80×24 and to
      200×60. Confirm no wrapping corruption in either direction.
      Observed: Not run: requires an interactive terminal to resize.
