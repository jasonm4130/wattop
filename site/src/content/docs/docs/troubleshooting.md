---
title: Troubleshooting / FAQ
description: Answers to the questions wattop users hit first, each linked to the full explanation.
---

Start with [`wattop doctor`](/docs/doctor/): it shows which hardware channels, agent sources and pricing table resolved on your machine, without entering the TUI.

## Why is `$/hr` `$0.00`?

`$/hr` counts only spend wattop watched happen. Everything already on disk when wattop starts is baselined as history, so **a fresh launch reads `$0.00/hr` until new spend lands**, even if your sessions have spent a lot today. The session's total cost (`$`) still includes that history.

A nonzero live `$/hr` has also not yet been observed end to end on real sessions; the positive path is covered by unit tests. See [limitations](/docs/limitations/#hr-under-reports-and-reads-000-more-often-than-you-would-expect).

## Why is a cost `$—` instead of a number?

`$—` means wattop could not price it, which is not the same as `$0.00`. Either the model is not in the [pricing table](/docs/pricing/#unknown-models) (it is then listed in the footer's unpriced-model list), or the session has no transcript at all: a headless `claude -p` run writes a session file but no transcript, so it has no model, tokens or cost. See [limitations](/docs/limitations/#a-headless-claude--p-run-has-no-transcript-and-reports-kind-interactive).

A `~` before a cost means the figure leaves out usage on an unpriced model.

## Why does wattop's cost differ from ccusage, or from my bill?

wattop's cost read roughly 1.8x-2.8x `ccusage` for the same sessions when cross-checked. Two causes: wattop prices 1-hour cache writes at the 1-hour rate where `ccusage` uses the 5-minute rate, and the two tools attribute usage between a session and its subagents differently. See [limitations](/docs/limitations/#wattops-cost-reads-roughly-18x-28x-ccusage-for-the-same-session).

Neither figure is your bill. wattop's costs are API-price estimates and ignore subscription plans (Claude Pro/Max, ChatGPT/Codex) entirely. See [honest labelling](/docs/honest-labelling/).

## A session is missing

- **It is dormant.** Stale rows bound to no live process are hidden by default; the footer shows `N hidden (a)`. Press <kbd>a</kbd> to show them. `--json` always includes them.
- **It is a Codex rollout that binds to no pid.** Codex leaves a rollout file behind per `exec` run. wattop scans a 2 h lookback, opens an older rollout only when a `codex` process exists, and drops a rollout that binds to no pid at the source, in both the TUI and `--json`.
- **It is a Codex session that was already idle for more than 24 h when wattop started.** The discovery window is 24 h and is not widened.
- **It is a resumed Codex session filed in an older date folder.** It can take up to 30 s to appear.

`wattop doctor`'s `sessions:` section shows how many sessions each source discovered and bound to a pid. See [limitations](/docs/limitations/#codex-status-and-pid-binding-are-inferred).

## A Codex row shows `(pid unknown)` or dashes for CPU, GPU and memory

For the first couple of seconds, a fresh Codex process has no CPU baseline yet, because CPU figures are deltas between scans; it then binds. A row that stays `(pid unknown)` matched no candidate `codex` process: pid binding for Codex is a best-effort heuristic, since Codex has no per-pid session file. See [limitations](/docs/limitations/#codex-status-and-pid-binding-are-inferred).

## Fans show 0 RPM

An intermittent SMC fault can make both fans read `0 RPM` for the whole life of one wattop process, while the same reading reports a minimum speed above zero. It was seen twice in about sixteen launches and is not root-caused; later launches read normally, so restart wattop. See [limitations](/docs/limitations/#fan-rpm-can-read-a-stuck-0-for-a-processs-whole-lifetime).

## A hardware field shows `—`

`—` means no source exists for that field on your machine; a channel that resolves and reads zero shows `0.0`. Run `wattop doctor` and check its `channels:` list. Hardware support varies by chip and macOS version, and wattop has only been tested on an M5 Max. See [limitations](/docs/limitations/#untested-outside-this-machine).

## DRAM bandwidth shows one `Total ~` figure with no read/write split

On the M5 Max no IOReport DRAM byte counter produces data, so wattop shows a total estimated from DRAM power and marks it with `~`. Read and write cannot be separated. If wattop starts while memory is already saturated, the estimate's calibration fails and no DRAM bandwidth appears for that run; the first figure after calibration also overshoots. See [limitations](/docs/limitations/#dram-bandwidth-on-this-chip-is-an-estimate-with-no-direction).

## Claude's context fill looks wrong

Claude's transcript carries no context-window size, so wattop estimates it (dashed bar edge) as 200,000 or 1,000,000 tokens. If you know the real window, set it per session id or cwd with `context_window_overrides` in [`config.toml`](/docs/config/). Codex's context fill is exact.

## The detail view is cut off

The dashboard fits at 80 columns and above, but the detail view (<kbd>enter</kbd>) needs **103 columns** and is cut off below that. Widen the terminal. See [limitations](/docs/limitations/#the-detail-view-does-not-fit-a-terminal-narrower-than-103-columns).

## I see no graphs, only meters

The history graphs need a terminal of at least 80x24; below that wattop shows the full hardware meters. <kbd>g</kbd> toggles between the two. See [Usage](/docs/usage/#keybindings).

## Gatekeeper blocks the binary

wattop is not Apple-notarized. The Homebrew cask removes the quarantine attribute from the `wattop` binary it installs (`xattr -dr com.apple.quarantine`), so a cask install launches normally. A binary extracted from a release archive you downloaded in a browser can carry the quarantine attribute, and macOS then blocks it. [Verify the download](/docs/install/#release-archive) first, then remove the attribute from that one file:

```sh
xattr -d com.apple.quarantine ./wattop
```

Or install with Homebrew, `go install`, or build from source. See [Install](/docs/install/).

## `wattop: soc unavailable (...); running with the session half only`

The hardware sampler failed to start, so wattop shows sessions without the hardware panels. wattop needs Apple Silicon (arm64) and macOS 14 or newer, and depends on Apple's private IOReport framework, which a macOS release could change. Run `wattop doctor` and see [limitations](/docs/limitations/#the-binary-is-not-static-and-not-cross-compilable).

## Pricing shows as degraded

The pricing table is refreshed at most once per 24 hours. If it is more than 30 days old because refreshes keep failing (offline, or raw.githubusercontent.com unreachable), wattop reports the pricing source as degraded, and `wattop doctor` shows `status: degraded (...)` with the reason. Costs still use the table wattop has. See [Pricing table](/docs/pricing/#background-refresh).

## wattop itself uses a lot of CPU right after launch

The first ~5 samples after startup read 95-304% while wattop catches up on existing transcripts; it then settles to about 5% (mean 5.19%, max 6.94%, watching 12 sessions and 1,293 subagents). See [self-CPU overhead](/docs/honest-labelling/#self-cpu-overhead).
