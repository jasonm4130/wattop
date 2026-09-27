---
title: Limitations
description: What wattop does not do well yet, in brief. The full list with evidence lives in the repo.
---

The full list, with the measurements behind each item, is in [`docs/limitations.md`](https://github.com/jasonm4130/wattop/blob/main/docs/limitations.md). In brief:

- **DRAM bandwidth is an estimate with no direction** on chips where no IOReport byte counter resolves: one power-derived total, no read/write split. The first figure after calibration overshoots.
- **ANE bandwidth** carries no channel-presence signal, so an exact `0.0` is reported as unresolved.
- **Claude context-fill is an estimate**; Codex's is exact.
- **Per-session GPU percent is derived**, not measured; ms/sec is the real figure.
- **Codex status and pid binding are inferred** from rollout files. A session already idle for more than 24 h when wattop starts is not found.
- **Fan RPM can read a stuck 0** for a process's whole lifetime (intermittent, not root-caused).
- **Headless `claude -p` runs** have no transcript, so no model, tokens or cost.
- **Costs ignore subscription plans**, and `$/hr` under-reports at launch by design.
- **The detail view needs 103 columns**; the main dashboard fits at 80.
- **Costs differ from `ccusage`** for the same session because of cache-write TTL pricing and parent/subagent attribution.
- **No static or cross-compiled binary**: the private IOReport framework is linked through CGO, and a future macOS release could break it.
- **Self-CPU over-reads for a few samples on startup.**
- **Tested on one machine** (M5 Max) so far.
- **Subagent status is inferred** from transcripts; a child that dies silently reads `idle`.
