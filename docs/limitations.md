# Limitations

Plain statements of what wattop does not do well, each with the evidence
behind it. See `docs/adr/2026-09-06-stack.md` for the design decisions these
follow from.

## DRAM and ANE bandwidth read zero on this hardware

`dram_read_bw_gbs`, `dram_write_bw_gbs` and `ane_bw_combined_gbs` are exact
`0.0` on this M5 Max under sustained load, at every sample count tried
while building this tool — including in mactop itself, the reference
implementation. This is not mactop's known counter-zeroing latch (that
requires `cpuPower==0 && dramPower==0`, and both are nonzero here); the
byte-counter channels simply do not resolve on this chip. wattop treats an
exact `0.0` on these fields as "did not resolve" and shows a dash, never
`0.0`. Run `doctor` to see exactly which IOReport channels resolved on your
machine.

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

## Costs are estimates that ignore subscription plans

Every `$` figure is computed from token counts against a LiteLLM-derived
per-token pricing table (`internal/pricing`, updated by `make pricing`; see
`docs/pricing-update.md`). It has no visibility into Claude Pro/Max or
ChatGPT/Codex subscription entitlements — wattop's cost figure ignores subscription plans entirely — so a session running entirely
inside a subscription's included usage still shows a nonzero estimated
cost. An unresolved model renders `$—`, never `$0.00` — the two mean
different things, and conflating them would hide real drift in the pricing
table behind a number that looks like "free."

## The session table does not fit a terminal narrower than ~150 columns

The session table does not narrow. `SessionsRender`'s format string
reserves 150 cells across its fourteen columns, and measured at 60, 80,
100, 120 and 140 columns it comes back 153 cells wide every time on the
v0.1 corpus — so anything under that wraps and the frame is corrupted. The
detail view has the same problem with a 103-column floor; only the help
overlay adapts down correctly.

150 is a floor, not a ceiling: `fmt` pads a short field but never truncates
a long one, and only `status`, `model` and `cwd` are truncated explicitly
before formatting. A burn rate wider than `%-8s` (`$276.68/hr` is ten
cells) or a cache figure wider than `%-18s` (`formatTokens` caps at no
digits, so a 10.5M-token session renders `10546.1k`) pushes the row wider
still. 153 is what this corpus produces, not a constant of the layout.

This is a session-table layout defect in `internal/ui/panel`, not a
terminal problem, and it is the observed result of `docs/manual-qa.md`
item 12 rather than a theoretical one. Two `internal/e2e` tests split it
so the suite cannot report "pass" for a frame that does not fit:
`TestFrameFitsTerminal` asserts the real pass condition — the frame fits
its terminal in both axes — for every size and frame that meets it, and
`TestSessionTableOverflowsAt80Columns` pins the two that do not (table
153 cells, detail 103, against a terminal of 80) with `==`, so the
exception fails both if the overflow grows and once the layout is fixed.

Not fixed in v0.1: the fix is a responsive column set in
`internal/ui/panel/sessions.go` (drop or shrink columns as width shrinks,
and truncate every field rather than three of them), which is a Task 12
change rather than a release-task one. When it lands, delete
`TestSessionTableOverflowsAt80Columns` — the 80x24 cases are already
enumerated in `frameCases` and fall back into `TestFrameFitsTerminal` —
tick manual-QA item 12, and delete this section.

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
