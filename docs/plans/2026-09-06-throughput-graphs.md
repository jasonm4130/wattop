# Balanced history and token throughput

Make wattop show recent AI throughput beside hardware activity, with session
rates underneath. Jason selected the balanced layout on September 6, 2026.

## Evidence and metric definitions

[mactop](https://github.com/metaspartan/mactop) groups CPU, GPU, memory, power,
and bandwidth histories. [btop](https://github.com/aristocratos/btop) offers
braille and block graphs alongside process lists. Use that hierarchy: history
first, instantaneous readings second, individual sessions below.

[Claude monitoring](https://code.claude.com/docs/en/monitoring-usage) documents
token counters updated after API requests and request timing through telemetry.
[Codex telemetry](https://learn.chatgpt.com/docs/config-file/config-advanced#observability-and-telemetry)
documents request and stream events, including completion token counts.
Both support opt-in OpenTelemetry export. A local transcript reader can show
recorded throughput without installing a telemetry service or changing agent
configuration; it cannot establish instantaneous model generation speed.

Show input and output tokens/sec as 60-second averages of timestamped usage,
including idle time. Input includes cache reads and writes; output already
includes reasoning where the provider reports it that way. Never add reasoning
tokens to output again. Expire old samples by event time, so loading hours of
transcript history does not turn into a launch spike. Missing usage is a dash.

Local inspection found repeated Claude message IDs with identical usage on
thinking, text, and tool rows (one request repeated 577 output tokens three
times). Deduplicate request usage before accumulating totals or graph samples,
including child transcripts. Codex supplies cumulative usage, so use successive
counter deltas and ignore repeated counters.

## Acceptance

Render labelled token, CPU, GPU, and power histories on a common sampling axis.
Add per-session output tokens/sec and retain the cumulative token count in
session detail. Fit the dashboard at 80, 120, and 160 columns, preserve pause,
and respect all themes and no-colour mode. Verify duplicate usage, counter
resets, old backfill, expiry to zero, child accounting, and missing data.
Run Go tests, vet, build, and the live binary against local collectors.
