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
