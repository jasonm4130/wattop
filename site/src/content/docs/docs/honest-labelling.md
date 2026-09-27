---
title: Honest labelling
description: What each number in wattop is, and what it is not.
---

wattop marks every figure that is an estimate, and never shows a zero where it has no data.

- Claude context-fill is an estimate (dashed bar edge); Codex's is exact from `rate_limits` (solid edge).
- Costs are estimates from the [pricing table](/docs/pricing/) and **ignore subscription plans** (Claude Pro/Max, ChatGPT/Codex) entirely.
- An unpriced model renders `$—`, never `$0.00`. Those mean different things.
- Per-session GPU is measured as ms/sec; the derived percent is a rescale and never the default sort key.
- Bandwidth renders `—` only where no source exists; a channel that resolves and reads zero renders `0.0`. On this chip no DRAM byte counter resolves at all, so DRAM shows one power-derived total, marked as an estimate (`Total ~14.3 GB/s`), with no read/write split.
- The temperature row shows the three semantic sensors (`CPU`, `GPU`, `SOC`) and omits any that did not resolve, rather than every raw SMC key the chip exposes.
- `$/hr` counts only spend wattop watched happen. Cost already on disk when it starts is baselined, so a fresh launch reads `$0.00/hr` rather than extrapolating hours of history into a rate.
- Token rates are 60-second transcript averages, not instantaneous model generation speed. Unknown usage is `—`; observed inactivity is `0.0`.

"This chip" is the M5 Max wattop has been tested on. [`wattop doctor`](/docs/doctor/) shows which channels resolve on yours.

:::caution[A nonzero live `$/hr` has not been observed end to end]
From [limitations](/docs/limitations/#hr-under-reports-and-reads-000-more-often-than-you-would-expect): "A nonzero live burn has still not been observed end to end: across two 30-second and one 3-minute `--json` capture, every watched transcript was static after the launch backfill, so `total_cost` never moved. The positive path rests on `internal/pricing`'s unit tests ($3.60/hr and $60.00/hr exactly, against controlled timestamps), not on live data."
:::

See [limitations](/docs/limitations/) for the full list with evidence, and the [manual QA checklist](https://github.com/jasonm4130/wattop/blob/main/docs/manual-qa.md) this release was verified against.

## Self-CPU overhead

Measured over a 75-second `--json` run on an M5 Max watching 12 Claude/Codex sessions with 1,293 subagents: `Snapshot.self_cpu_pct` settles to **mean 5.19% (max 6.94%)** in steady state. The first ~5 samples after startup read 95-304% while wattop catches up on existing transcripts (and, on M5-class chips, calibrates DRAM bandwidth); see [limitations](/docs/limitations/#self-cpu-shows-an-unexplained-transient-over-read-on-startup) and [`docs/manual-qa.md`](https://github.com/jasonm4130/wattop/blob/main/docs/manual-qa.md) item 11. A monitor that distorts what it measures must be accountable for its own overhead; this is that accounting.
