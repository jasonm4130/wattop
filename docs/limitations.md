# Limitations

Plain statements of what wattop does not do well, each with the evidence
behind it. See `docs/adr/2026-09-06-stack.md` for the design decisions these
follow from.

## DRAM bandwidth on this chip is an estimate with no direction

An earlier version of this section said the DRAM byte-counter channels "do
not resolve on this chip" and that an exact `0.0` therefore meant "did not
resolve". Both halves were wrong, and the QA run on 2026-09-06 measured what
is actually true (`docs/qa/2026-09-06-v0.1.md` §2).

What the M5 Max under macOS 27 actually does:

- **No IOReport DRAM byte counter produces data.** Verified over 27
  consecutive samples with every core streaming memory at 16.8 W of DRAM
  power: all three DRAM fields stayed `null` with all three named in
  `sys.missing` — the sampler's "no source" state, which means none of the
  branches in `samplePowerMetrics` that set a source was both reached and
  non-empty.
- **The only figure that ever appears is derived from DRAM power.** mactop
  falls back to calibrating a GB/s-per-watt constant at runtime (four
  threads streaming 256 MB buffers, measured against the Energy Model DRAM
  channel) and then reports `(dram_power - idle_power) x constant`. That is
  an estimate of *total* traffic, so wattop publishes it as
  `dram_combined_gbs` with `dram_estimated: true` and renders it as
  `Total ~9.2 GB/s`. Measured live: 24 of 28 samples resolved under a
  two-thread memory load, tracking DRAM power from 3.5 W to 4.1 W.
- **Read and write are not separately measurable here.** The estimator
  cannot distinguish direction; earlier builds split its one figure in half
  and published the halves as `dram_read_gbs` and `dram_write_gbs`, which is
  why the two were identical to fifteen significant figures on every live
  sample. They are now `null`, and named in `sys.missing`.
- **The calibration is one-shot and fails on a busy machine.** It arms the
  first time bandwidth looks likely (busy CPU or GPU, or elevated DRAM
  power) and takes a 500 ms idle baseline at whatever load the machine is
  under. Trip it while memory is already saturated and the baseline equals
  the stress reading, the derived constant is rejected, and no DRAM
  bandwidth figure appears again for the life of the process. Reproduced:
  starting a full-machine memory load at the same instant as wattop yielded
  0 of 24 resolved samples; a lighter load that left power headroom yielded
  24 of 28. Not fixed.

`dram_read_gbs`, `dram_write_gbs` and `dram_combined_gbs` are `null` only
when no source produced a figure. A source that resolved and counted zero
now publishes `0.0`, because on this hardware idle DRAM traffic really is
approximately zero and dashing it threw away a real reading. Measured live
after the load ends: eight consecutive samples with `dram_combined_gbs:
0.0`, `dram_estimated: true` and DRAM power back at 0.35-0.48 W.

That distinction needs the source flag, and only the live sampler has one.
mactop's headless JSON — the format `internal/collect/replay` reads and the
test corpus is captured in — writes a plain `0` whether a channel counted
zero or never resolved. That path therefore treats a `0` on any bandwidth
key as unresolved: the field is `null`, the key is named in
`sys.missing`, and `Channels()` reports it unresolved. A replay frame that
claimed `DRAM R 0.0 GB/s` off a corpus where no byte counter ever produced
data would be the same error as the dash it replaced, pointing the other
way.

## ANE bandwidth carries no channel-presence signal

`ane_bw_combined_gbs` was `null` on every sample of the QA run and of every
run since. Unlike DRAM, mactop reports ANE bandwidth only as a byte total
with nothing saying whether a channel was behind it, so wattop still cannot
tell an absent ANE channel from one reading exactly zero and reports an
exact `0.0` as unresolved. That is a weaker rule than the DRAM one above and
is applied deliberately, not by oversight: fixing it needs the same
source-tracking through `ioreport.m` that DRAM now has.

Run `doctor` to see which channels resolved on your machine.

## Claude context-fill is an estimate; Codex's is exact

Claude Code's transcript carries no authoritative context-window figure, so
its context-fill gauge is estimated from a token-count ladder (overridable
per session or cwd via `config.toml`'s `context_window_overrides`) and
rendered with a dashed bar edge to mark the estimate. Codex's `rate_limits`
payload reports its context window directly; that gauge is exact and drawn
with a solid edge.

## Per-session GPU percent is derived, not measured

Per-pid GPU ms/sec is a real measurement — mactop's own nanosecond-delta
step, verified in-process (a 27-entry per-pid table, one pid at 20.026
gpu_ms_per_sec over a 1.22 s window). Per-session GPU *percent* is a rescale
of that ms/sec figure so the per-session sum approximates system GPU-busy;
the rescale itself is unvalidated. ms/sec is therefore the load-bearing
column and the one used for anything that matters (it is never the default
sort key candidate the percent might otherwise be); the percent is a
secondary, marked-as-derived number.

## Codex status and pid binding are inferred

Codex CLI has no live IPC and no per-pid session file the way Claude Code
does. Status (busy / waiting / stale) is inferred from rollout file mtime
against a configurable idle threshold, and a rollout is bound to a pid by
the nearest-after-start-time heuristic among candidate `codex` processes —
one rollout per pid, `(pid unknown)` rather than a dropped row when no
candidate matches. Both are best-effort; neither is a promise from Codex
itself.

Codex also leaves one rollout file behind per `exec` run, forever, and the
QA run on 2026-09-06 found fourteen of nineteen rows on launch were dormant
rollouts up to 23 h old — all `(pid unknown)`, all dashes, burying the five
live sessions the tool exists to show. `codex.Source` now scans a 2 h
lookback by default and opens an older rollout only when a `codex` process
exists at all; one that binds to no pid is dropped at the source, so `--json`
agrees with the TUI. A dormant row that survives that (a Claude session gone
stale, say) is hidden from the table and counted in the footer as
`N hidden (a)`, with `a` toggling it back. Nothing is silently dropped from
the screen without being counted.

The lookback is a constant, not yet a config key: `codex.WithLookback`
exists but `cmd/wattop` does not read a `codex_lookback_minutes` from
`config.toml`, so 2 h is what you get.

## Fan RPM can read a stuck 0 for a process's whole lifetime

Observed twice in about sixteen launches during the 2026-09-06 re-QA: both
fans reported `rpm: 0` on every sample for the life of that wattop process,
while `min_rpm` on the same reading was 2317 — a speed below the fan's own
stated minimum, which is not a reading a spinning fan produces. Ten
consecutive launches immediately afterwards, including under memory load and
alongside a second concurrent wattop, all read 2311-2321 / 2494-2508 RPM, so
the fault is intermittent and not load- or contention-triggered as far as
this run could tell. Not root-caused.

Two things follow. The SMC fan read fails in a way that yields zero rather
than an error, and when it does the panel renders `Fan 0 0 RPM` rather than
`Fan 0 —` — the same conflation of "zero" with "absent" that the DRAM
section above exists to avoid, in the one place that still has it.

## A headless `claude -p` run has no transcript, and reports `kind: interactive`

Two separate gaps, both observed on the 2026-09-06 QA run and both still
open at HEAD.

A headless `claude -p` process writes a session file under
`~/.claude/sessions/<pid>.json` but no `<id>.jsonl` transcript — `find`
against a live headless run's id returned a directory and nothing else.
Everything wattop derives from the transcript is therefore genuinely
unavailable for that row: no model, no token split, no cost. The row renders
`—` in the model column and `$—` for cost, and the detail view says
`no transcript on disk: tokens and cost unavailable` rather than leaving the
reader to guess whether wattop failed or the data is absent.

Separately, Claude Code writes `kind: "interactive"` into that session file
for a headless run (`entrypoint: "sdk-cli"` is the field that actually says
so), so wattop's `kind` is `interactive` for a process the plan expected to
read `sdk-cli`. The background-session styling keyed on `Kind !=
"interactive"` therefore never fires for exactly the headless runs it was
meant to mute. Fixing it needs an `Entrypoint` field on `domain.Session`.

## Costs are estimates that ignore subscription plans

Every `$` figure is computed from token counts against a LiteLLM-derived
per-token pricing table (`internal/pricing`, updated by `make pricing`; see
`docs/pricing-update.md`). It has no visibility into Claude Pro/Max or
ChatGPT/Codex subscription entitlements — wattop's cost figure ignores subscription plans entirely — so a session running entirely
inside a subscription's included usage still shows a nonzero estimated
cost. An unresolved model renders `$—`, never `$0.00` — the two mean
different things, and conflating them would hide real drift in the pricing
table behind a number that looks like "free."

## `$/hr` under-reports, and reads `$0.00` more often than you would expect

The burn rate counts only spend wattop watched happen. It baselines a
session the first time it sees it and times every later delta by the newest
usage timestamp in that session's transcript, discarding a delta whose
newest record is already older than the 60 s window. Before that, the first
full read of hours of transcript at launch was counted as spend inside one
~1.3 s poll and extrapolated: the QA run on 2026-09-06 recorded a headline
of **$105,679/hr** decaying over minutes (§4). Re-measured after the fix, a
$41.85 backfill arriving in 1.481 s across two polls reads `$0.00/hr`.

The cost of that is systematic under-reporting, in two known shapes:

- **A fresh launch reads `$0.00/hr` until new spend lands.** Everything on
  disk when wattop starts is history, not rate.
- **A parent session blocked in a long `Task` call reads `$0.00/hr` while
  its subagent spends.** The parent writes no transcript records while
  blocked, so its newest `ToolCall.At` freezes at the spawn instant, while
  the subagent's growing usage keeps folding into the parent's `CostUSD`.
  Once the spawn is more than 60 s old, every such delta is discarded as
  stale. This is the swarm case wattop was built to watch, and `$0.00`
  rather than a dash sits awkwardly beside the honesty rule the rest of the
  tool follows. Fixing it needs a real usage timestamp on `domain.Session`
  and `domain.Subagent`, which the Claude and Codex sources do not carry
  today.

Codex rollouts surface no usage timestamp at all, so they fall back to the
wall clock; baselining on first sighting is their only protection against a
multi-poll backfill.

A nonzero live burn has still not been observed end to end: across two
30-second and one 3-minute `--json` capture, every watched transcript was
static after the launch backfill, so `total_cost` never moved. The positive
path rests on `internal/pricing`'s unit tests ($3.60/hr and $60.00/hr
exactly, against controlled timestamps), not on live data.

## The detail view does not fit a terminal narrower than 103 columns

The **session table** used to be here and is not any more. It narrows:
columns shrink, the context gauge collapses to `~ 55%`, the token triple
collapses to a single figure, surplus rows become a `▼ N more` marker, and
`$`, `$/HR`, `CPU%` and `RSS` survive at every width. Measured on the v0.1
corpus it renders 80 cells at 80x24 (was 153), and a live capture at 80x24
against four real Claude sessions comes back 80 cells with nothing past the
terminal edge (`docs/qa/2026-09-06-v0.1.md`, "Re-QA after hardening"). The
`Machine` footer narrows the same way, dropping whole segments rather than
cutting a figure in half.

The **detail view** (`enter`) was never given that treatment. It reserves
103 cells and does not adapt, so at 80 columns a real terminal cuts it
mid-token — the live capture loses the tail of the token line at
`cache-wri`. The fix is the same shape as the table's: a responsive column
set in `internal/ui/panel/detail.go`.

`internal/e2e` splits the two so the suite cannot report "pass" for a frame
that does not fit. `TestFrameFitsTerminal` asserts the real pass condition —
the frame fits its terminal in both axes — for every size and frame that
meets it, which now includes the session table at 80x24.
`TestDetailViewOverflowsAt80Columns` pins the one that does not (103 cells
against a terminal of 80) with `==`, so it fails both if the overflow grows
and once the layout is fixed. When it is fixed, delete that test and
`overflowAt80`, tick `docs/manual-qa.md` item 12, and delete this section.

## wattop's cost reads roughly 1.8x-2.8x `ccusage` for the same session

Cross-checked on 2026-09-06 against `ccusage session --json` over the same
four live Claude sessions (`docs/manual-qa.md` item 10): wattop $35.80 /
$20.90 / $301.31 against ccusage $12.87 / $11.66 / $113.67. Two causes,
both verified arithmetically rather than guessed:

- **Cache-creation TTL.** This machine's transcripts report cache writes as
  `ephemeral_1h_input_tokens`, not `ephemeral_5m` — 92 records on one
  session, 294,667 tokens, all 1-hour and zero 5-minute. wattop prices
  those at `cache_creation_input_token_cost_above_1hr` ($2.0e-5/token for
  `claude-fable-5-1`); `ccusage` prices all cache creation at the
  5-minute rate ($1.25e-5). On that one session that is a $2.21 difference,
  and wattop is the more accurate of the two.
- **Parent/sidechain attribution.** wattop splits a session's own usage
  from each subagent's and prices them separately, then sums: on session
  `28cd1f81`, $13.06 own + $7.84 subagent = $20.90, exactly the figure
  reported. `ccusage` splits the same transcript by the model on each
  message instead ($6.51 `claude-fable-5-1` + $5.15 `claude-opus-5`), and
  arrives at different token totals for the parent (input 84,323 /
  output 66,935 / cache-read 11,793,625 against wattop's 2,306 / 90,236 /
  10,546,114).

Neither tool knows about subscription plans, so neither figure is what the
account is actually billed. The divergence is recorded here because "our
number differs from the other tool's" is the first question anyone asks,
and the answer is a real difference in method, not a bug in either.

## The binary is not static and not cross-compilable

wattop links against `-lIOReport`, a private Apple framework with no public
header, via CGO. `CGO_ENABLED=1 GOOS=darwin GOARCH=arm64` on an Apple
Silicon host is the only supported build; there is no Linux build and no
cross-compilation from an Intel host. `-lIOReport` is not a public API —
Apple has changed private frameworks across macOS releases before, and a
future macOS release breaking it is a real, unscheduled risk, not a
hypothetical one.

## `ioreport.m` is a standing maintenance liability

The vendored Objective-C IOReport shim (`internal/soc/mactop/ioreport.m`)
is 136 KB — by far the largest single file in this tree, and the one most
likely to need a re-vendor when a macOS release changes IOReport's private
ABI. `make vendor-diff` re-fetches the pinned mactop commit and diffs the
16 verbatim-copied files against it; there is no automated alert beyond
running it.

## Self-CPU shows an unexplained transient over-read on startup

Over a 60-second `--json` run (see `docs/manual-qa.md` item 11), the first
five `Snapshot.self_cpu_pct` samples were `0, 18.4, 5.6, 296.6, 192.7`
before settling to a 4.2%-7.0% steady state that an independent `ps -o
%cpu` reading corroborates. The spike lands on samples 4 and 5, after a
plausible 5.6% reading — not on sample 1, where a genuinely missing
CPU-delta baseline would show up (and which `CPUTracker.Update` reports as
`ok=false`, not a number, per `internal/proc/delta.go`). This is not the
same thing as the documented "no baseline yet" warmup a fresh Codex pid
shows as a dash. The likely candidate is `internal/proc`'s per-pid
CPU-delta arithmetic — either the elapsed wall interval between the
scanner's own early scans, or a stale/reused baseline — producing a
too-large percentage for one or two ticks. Not root-caused or fixed here;
`internal/proc` is out of this task's scope.

## Untested outside this machine

Everything above, and the dashboard generally, has been built and tested
on one machine: Apple Silicon M5 Max. Nothing has been verified on Intel,
on M1 through M4, or on a non-Max/Ultra chip (a plain M-series with no S
cluster, or a different core-count split). The cluster panel iterates
whatever topology `sysctl` reports rather than hardcoding a core count, so
it should degrade gracefully, but "should" is not "has been observed to."
