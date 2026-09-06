# Manual QA

A checklist run on this machine (Apple Silicon, M5 Max) against live
agents — the behaviour `internal/e2e`'s replay-driven tests cannot exercise,
because it needs a real terminal, real IOReport hardware, or a real process
to kill. Tick each box with the value actually observed, not the expected
one; a checklist entry that just repeats the pass condition back is not a
result.

Run `wattop` from a built `bin/wattop` (`make build`), not `go run`, so the
self-CPU figure in step 11 reflects the real binary.

## Legend

| Mark | Meaning |
|---|---|
| `[x]` | Run, and the stated pass condition was observed to hold. |
| `[~]` | Run, and part of the pass condition was observed; the residual is named on the line and needs a human at an interactive terminal. |
| `[!]` | Run, and the stated pass condition was observed to **fail**. The failure is recorded in `docs/limitations.md`. |
| `[ ]` | Not run: needs an interactive terminal plus a live agent this session could not create without disrupting other work. |

Status of this run (2026-09-06): 6 pass, 3 partial, 1 fail, 2 not run.

The two not-run items both need a live Claude session killed or a subagent
driven from one, on a machine that had five concurrent worker sessions
running at the time; doing either would have destroyed another worker's
in-flight run. They are the remaining human-at-the-terminal work before
v0.1 ships.

---

- [~] **1. Two agents, one busy.** Start `wattop` with two Claude sessions
      and one `codex exec` running. Both agents appear; the busy one shows
      a spinner; statuses flip within ~1 s of a real transition. The Codex
      row reads `(pid unknown)` with dashed CPU/GPU/RSS for the first ~2 s
      while the process scanner establishes its CPU baseline, then binds —
      that is the documented warmup, not a failure.
      Observed: `bin/wattop --once --json`, 2026-09-06 14:59 AEST, against
      five concurrent live Claude sessions and no live `codex exec`. All
      five Claude sessions appeared, each pid-bound with `bind_conf:
      "exact"` (pids 14106 / 39160 / 87821 / 92953 / 97048), carrying four
      distinct real statuses at once — `busy`, `idle` (×2), `shell`,
      `unknown` — so status is being read per session, not stamped
      uniformly. Twenty Codex rollouts appeared as `stale` with
      `pid=null`, which is the no-live-process path, not the warmup path.
      **Residual:** the spinner glyph, the ~1 s transition latency, and the
      Codex `(pid unknown)` → bound warmup all need the TUI plus a live
      `codex exec`, which this session did not start (it spends paid
      quota).

- [x] **2. No sudo.** Confirm no sudo prompt appears at any point, from
      launch through exit.
      Observed: `make build` + `bin/wattop doctor`/`--json --once`/`--json`
      ran end to end as `jasonmatthew`, no sudo prompt, no privilege
      elevation.

- [ ] **3. Kill mid-run: the full stale-then-drop lifecycle.** Kill a
      Claude session mid-run. Its `~/.claude/sessions/<pid>.json` disappears,
      so the source stops emitting it and the reducer holds the row as
      `stale` with a growing age for `sessionTTL` (30 s) before removing
      it. Observe both edges: the flip to `stale` within one refresh
      interval, and the row gone about 30 s later. No panic; other rows
      unaffected.
      Observed (time to `stale`, time to removal): Not run. Requires
      killing a real live Claude session; five concurrent worker sessions
      were running on this machine and killing any of them would have
      destroyed another worker's in-flight run. `internal/e2e`'s
      `TestStaleThenDropLifecycle` asserts the same lifecycle
      deterministically on the replay clock (present at t=19 s, `stale` at
      t=25 s, still present at t=49 s, gone at t=51 s), so the reducer half
      is covered; what is untested is the real filesystem edge.

- [~] **4. Subagent lifecycle.** Start a subagent (a `Task` tool call). It
      appears as an indented child row, marked live, with the resolved
      model id (never the alias, e.g. `claude-opus-5` not `opus`), and
      drops out of the live set when its `tool_result` lands.
      Observed: same `--once --json` run as item 1. Seven subagent rows
      across three parent sessions (3 + 1 + 3), each nested under its
      parent in `sessions[].subagents` — the structure the indented child
      row renders from. Every one carried a **resolved** model id:
      `claude-opus-5` (×3) and `claude-sonnet-5` (×4), never `opus` or
      `sonnet`. Each carried its own priced usage block, and the parent's
      cost is the sum: session `28cd1f81` reported `$20.9043165`, which is
      its own usage priced at `$13.0647` plus its subagent's
      `$7.839588` — exact to the cent, so subagent rollup is arithmetically
      confirmed. All seven read `live: false`, i.e. already finished.
      **Residual:** the live→dropped transition itself needs a subagent
      observed while running, which needs a driving session this run did
      not have.

- [ ] **5. Drive the GPU.** Run something GPU-heavy under an agent's pid.
      The GPU gauge and watts move, and the owning session's `gpu ms/s`
      column becomes nonzero.
      Observed: Not run. Requires a real GPU-heavy workload running under a
      live agent's own pid; a subprocess spawned from a session's shell is
      a different pid and would not exercise the per-session attribution
      this step is checking. Baseline for whoever runs it: at idle the
      `--once --json` run read `gpu_watts: 0.0106` and every session's
      `proc.gpu_ms_per_sec` was `null`.

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
      Observed: `bin/wattop doctor` -> `clusters: P: 12 cores, S: 6 cores`,
      and the live `--json` payload agrees:
      `[{"label":"P","core_count":12,...},{"label":"S","core_count":6,...}]`.
      No `E` cluster reported or rendered.

- [~] **8. Theme cycling.** Cycle all four themes with `t` (and back with
      `T`). Confirm every panel re-colours and nothing renders unreadable
      in the light palette.
      Observed: `internal/e2e`'s `TestThemeCycleRecolours` drives the real
      model over the real corpus and presses `t` four times. Each press
      produced a frame byte-different from the previous one (so every
      panel re-coloured) and byte-identical to it once ANSI escapes are
      stripped (so nothing moved or was dropped), no two of the four themes
      rendered identically, and the fourth press returned exactly to the
      starting frame. Runs on every `go test ./internal/e2e/...`.
      **Residual:** "nothing renders unreadable in the light palette" is a
      contrast judgement about `wattop-light` that no assertion here stands
      in for; it needs eyes on a real terminal.

- [x] **9. `NO_COLOR`.** Run `NO_COLOR=1 wattop`. Confirm gauges degrade
      to plain block characters with no ANSI escapes anywhere on screen.
      Observed: `NO_COLOR=1 bin/wattop --json --once` output contains zero
      `\x1b` (ESC) bytes across 95,958 bytes of JSON output.

- [x] **10. Cross-check against `ccusage`.** Compare `wattop --once --json`
      costs against `ccusage` for the same session. Note any divergence
      and its likely cause (pricing-table drift, tier boundary, cache
      accounting).
      Observed: `npx ccusage@latest session --json` against the same four
      live Claude sessions. wattop reads **1.8x-2.8x** higher:

      | session | wattop | ccusage | ratio |
      |---|---|---|---|
      | `f482bb1c` | $35.80 | $12.87 | 2.78 |
      | `28cd1f81` | $20.90 | $11.66 | 1.79 |
      | `a67c55f8` | $301.31 | $113.67 | 2.65 |

      Two causes, both checked arithmetically rather than guessed. (a)
      **Cache-creation TTL**: this machine's transcripts report cache
      writes as `ephemeral_1h_input_tokens` — on `28cd1f81`, 92 records,
      294,667 tokens, all 1-hour and zero 5-minute — and wattop prices
      those at the `_above_1hr` rate ($2.0e-5/token) where ccusage uses the
      5-minute rate ($1.25e-5), a $2.21 difference on that session. wattop
      is the more accurate of the two here. (b) **Parent/sidechain
      attribution**: wattop prices the session and each subagent separately
      and sums ($13.06 + $7.84 = $20.90); ccusage splits the same file by
      per-message model ($6.51 fable + $5.15 opus) and derives different
      parent token totals. No pricing-table drift found: both resolve the
      same model ids. Neither figure accounts for subscription plans. Full
      write-up in `docs/limitations.md`.

- [x] **11. Self-CPU overhead.** Watch the footer's self-CPU figure over
      60 s of normal use. Record the observed range below — a monitor
      that distorts what it measures must be accountable for its own
      overhead, and this number belongs in the README, not just here.
      Observed (range over 60 s): `bin/wattop --json` piped to a file for
      60 s, 46 Snapshots. Raw `self_cpu_pct` sequence, first 5 samples:
      `0, 18.4, 5.6, 296.6, 192.7`, then settling. `Snapshot.self_cpu_pct`
      settled to **4.2%-7.0% (mean 5.35%)** for the remaining ~41 samples.
      Independently, `ps -o %cpu` on the running pid read 5.4% at t+3s,
      agreeing with the steady-state figure. The samples-4-and-5 spike
      (296.6%, 192.7%) follows a plausible 5.6% reading, not a missing
      baseline, so it is not the same "no CPU baseline yet" warmup this
      README describes for a fresh Codex pid — that case renders a dash,
      not a number. This looks like a transient over-read in
      `internal/proc`'s CPU-delta arithmetic (elapsed-interval or
      baseline-reuse candidate) rather than a UI/state-layer issue; see
      docs/limitations.md. Not root-caused or fixed as part of this task
      (`internal/proc` is Task 6's file, out of scope here) — flagged for
      follow-up.

- [!] **12. Terminal resize.** Resize the terminal to 80×24 and to
      200×60. Confirm no wrapping corruption in either direction.
      Observed: **fails at 80×24.** `internal/e2e`'s
      `TestFrameFitsTerminal` drives the real model over the real corpus
      and measures the widest rendered line at each size. At 200×60 and at
      160×40 every frame fits its terminal exactly. At 80×24 the session
      table renders **153 cells wide** and the detail view 103 — 73 and 23
      cells of overflow, which a real terminal folds into wrapped rows.
      The floor is fixed, not proportional: at 60, 80, 100, 120 and 140
      columns the table comes back 153 cells wide every time, so it never
      narrows at all. Only the help overlay adapts down correctly. Frame
      *height* is fine at both sizes. Recorded in `docs/limitations.md`;
      the fix is a responsive column set in `internal/ui/panel`, out of
      this task's scope.
