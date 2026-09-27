---
title: Honest labelling
description: What each number in wattop is, and what it is not.
---

- Claude context-fill is an estimate (dashed bar edge); Codex's is exact from `rate_limits` (solid edge).
- Costs are estimates from LiteLLM's pricing table and **ignore subscription plans** (Claude Pro/Max, ChatGPT/Codex) entirely.
- An unpriced model renders `$—`, never `$0.00`. Those mean different things.
- Per-session GPU is measured as ms/sec; the derived percent is a rescale and never the default sort key.
- Bandwidth renders `—` only where no source exists; a channel that resolves and reads zero renders `0.0`. Where no DRAM byte counter resolves, DRAM shows one power-derived total, marked as an estimate (`Total ~14.3 GB/s`), with no read/write split.
- The temperature row shows the three semantic sensors (`CPU`, `GPU`, `SOC`) and omits any that did not resolve.
- `$/hr` counts only spend wattop watched happen. Cost already on disk when it starts is baselined, so a fresh launch reads `$0.00/hr` rather than extrapolating hours of history into a rate.
- Token rates are 60-second transcript averages, not instantaneous generation speed.

## Pricing

Costs come from an embedded, filtered snapshot of LiteLLM's pricing table, refreshed in the background at startup and cached, so pricing works instantly and offline on first run. Each request is priced at its own model and its own long-context tier, so a `/model` switch or one very long prompt never reprices the rest of a session.

## Self-CPU overhead

Measured over a 75-second `--json` run on an M5 Max watching 12 Claude/Codex sessions with 1,293 subagents: `self_cpu_pct` settles to **mean 5.19% (max 6.94%)** in steady state. The first ~5 samples after startup read 95–304% while wattop catches up on existing transcripts. A monitor that distorts what it measures must be accountable for its own overhead; this is that accounting.
