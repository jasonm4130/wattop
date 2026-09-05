# Landscape: mactop + AI-agent session dashboard

Research date 2026-09-06. Scope: a terminal dashboard merging Apple-Silicon system stats (mactop-style) with live Claude Code + Codex CLI session state, on this machine, no sudo.

## Verdict

Gap not filled. Two mature, non-overlapping ecosystems exist and nothing bridges them in a terminal:

- **Apple-Silicon system monitors** (mactop, macmon, btop, asitop) — deep CPU/GPU/ANE/power/thermal data, zero knowledge of AI agent sessions.
- **"htop for AI agents" TUIs** (abtop, agtop, aitop, agent-deck, and a dozen similarly-named clones) — read Claude Code / Codex transcripts for tokens, cost, context %, tool calls; system stats, where present at all, are generic `ps`-derived CPU%/mem, never GPU/ANE/power/thermal.

Two refuters (independent researchers in the prior workflow, referred to in the dossier as reviewing mactop's docs) both concluded from README/pkg.go.dev reads that mactop has no `--pid` flag and flagged the brief's claim as wrong. Running `mactop --help` on the installed v2.1.5 shows `--pid <pid>  Monitor a specific process by PID` — the brief was right, the doc-reading was wrong. This doesn't change the verdict (a fleet dashboard needs the `processes[]` array from headless JSON, not `--pid`, anyway) but it is the one point both refuters got wrong and it's worth not re-litigating.

Closest single product to the exact ask, **claude-stats** (1pitaph), genuinely spans both halves (system stats + Claude Code/Codex session data) but fails on medium: it's a native macOS menubar app with a Warp-powered embedded terminal for running builds, not a terminal stats TUI. If a menubar app were acceptable this is the one to try; it isn't, per the brief (terminal dashboard, v1 macOS arm64).

## Closest existing tools

| Name | Stack | Covers | Lacks | URL |
|---|---|---|---|---|
| mactop v2 | Go, gotui, CGO | Full Apple-Silicon depth: CPU E/P/S-core, GPU, ANE, power, DRAM bandwidth, temps, fans, battery, net, disk; `--headless --format json`, `--prometheus`, `--pid` | No agent/session awareness at all | https://github.com/metaspartan/mactop |
| agtop (ldegio) | Node.js ≥18 | Claude Code + Codex tokens/cost/context%/tool-calls/subagents; host CPU% and per-session CPU/mem sparklines (closest to a "system half" among agent tools) | System half is `ps`/`lsof` process CPU/mem only — no GPU/ANE/power/thermal/per-core E/P; no host-wide view; no Prometheus | https://github.com/ldegio/agtop |
| abtop (graykode) | Rust, ratatui | Claude Code + Codex + OpenCode: token tracking, context% bars, rate limits, git status, subagents; host CPU/MEM/load-avg panel | Host stats generic (no GPU/ANE/power/thermal); no cost/burn-rate at all | https://github.com/graykode/abtop |
| claude-stats | Swift (native macOS) | Only surveyed tool with both halves: system monitor (CPU/mem/disk/net/power/thermal/ports) alongside Claude Code + Codex session/cost data | Wrong medium — menubar/Notch Island GUI with an embedded Warp terminal for builds, not a stats TUI; Gemini/Kimi/MiniMax parsers are "future work" | https://github.com/1pitaph/claude-stats |
| mactop + abtop (two tmux panes) | Go + Rust | Everything above, side by side, today, zero new code | No correlation between a GPU/ANE power spike and which agent session caused it; no shared refresh clock; no unified burn-rate-next-to-watts view | both above |
| CodeBurn (getagentseal) | TS/Node core + Swift macOS app | Broadest agent coverage (41 tools incl. Claude Code, Codex, Cursor, Copilot, Gemini); tokens, cost, cache metrics, tool-invocation frequency; best public per-agent format docs (`docs/providers/*.md`) | Historical/aggregated only, no live in-progress state; zero system stats; **name collision — this is a real shipping tool, do not reuse the name** | https://github.com/getagentseal/codeburn |

## Local data sources on this machine

No cost/`costUSD` field exists anywhere in either agent's on-disk records — every candidate build must compute $ from `(model, usage tokens)` against a maintained pricing table (LiteLLM's table or models.dev), same approach ccusage and agtop already take.

**Claude Code** (v2.1.261 installed):

- `~/.claude/projects/<sanitized-cwd>/<session-uuid>.jsonl` — main transcript, newline-delimited JSON, heterogeneous via `type` (assistant/user/system/…). Key fields: `message.model` (can be an alias, e.g. `claude-fable-5-1`), `message.usage.{input_tokens,output_tokens,cache_creation_input_tokens,cache_read_input_tokens,output_tokens_details.thinking_tokens}`, `message.content[]` blocks of `type:tool_use` (`name`, `id`) matched to `tool_result` blocks in the following user record, plus `timestamp`, `cwd`, `sessionId`, `gitBranch`, `version`, `effort`, `parentUuid`/`uuid`, `isSidechain`. No cost field. Files can be multi-MB and grow live (observed mtime ~9s old on an active session) — tail incrementally, never re-read whole.
- `<session-uuid>/subagents/agent-<hash>.jsonl` + sibling `agent-<hash>.meta.json` — per-subagent transcript and metadata (`agentType`, `description`, `toolUseId` linking back to the parent's `tool_use` block, `spawnDepth`, `model`). Subagent jsonl records carry the *resolved* concrete model id even where the parent/meta shows an alias like `opus`.
- `~/.claude/sessions/<pid>.json` — one file per live/recent process, keyed by OS pid: `{pid, sessionId, cwd, startedAt, procStart, version, kind, entrypoint(cli|sdk-cli), status(busy|waiting), statusUpdatedAt, updatedAt, name, bridgeSessionId}`. Cheapest live-status poll — updates near-instantly on busy/waiting transitions, no need to tail the transcript just to answer "is this session working right now." Sibling `<pid>.<hash>.key` files are messaging-socket auth secrets — never read them.
- `~/.claude/history.jsonl` — flat cross-project prompt history (`display`, `timestamp`, `project`, `sessionId`) for a "recent activity" feed.
- `~/.claude/stats-cache.json` — precomputed daily aggregates (`dailyActivity[]`, `dailyModelTokens[]`); observed 12 days stale at probe time — historical-trend use only, never "today's" numbers.
- Process→session join: `ps -axo pid,ppid,etime,command | grep -Ei 'claude|codex'` finds candidate pids; `cat ~/.claude/sessions/<pid>.json` gives `sessionId`/`cwd`/`status` directly (no need to derive the sanitized-cwd directory name yourself).

**Codex CLI** (0.151.0 installed, 0.153.2 latest):

- `~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<uuid>.jsonl` — the practical read source despite Codex having migrated most state to internal SQLite (`state_5.sqlite` etc. — not worth reverse-engineering). Record types: `session_meta` (`session_id`, `cwd`, `cli_version`), `turn_context` (`model`, `effort`, `sandbox_policy`, `cwd`) — **read model here per-turn, never from `config.toml`'s default**, since `-m` overrides per-invocation (confirmed live: a running `codex exec -m gpt-5.6-terra` process). `event_msg` with `payload.type` in `{task_started, task_complete, token_count, thread_settings_applied}`. The `token_count` event's `info.total_token_usage`/`info.last_token_usage` gives `{input_tokens, cached_input_tokens, cache_write_input_tokens, output_tokens, reasoning_output_tokens, total_tokens}` plus `info.model_context_window` (e.g. 258400) and `rate_limits.{primary,secondary}.{used_percent, window_minutes, resets_at}` — this is Codex's direct equivalent of Claude Code's context-fill %, no derivation needed. No cost field, no per-pid session-status file — liveness must come from `ps` + file mtime (`find … -mmin -N`) joined against `turn_context.payload.cwd`.

**mactop** (v2.1.5 installed, verified no-sudo):

- `mactop --headless --format json --count 1` — one-shot, exit 0, no sudo prompt. Returns one JSON object: `soc_metrics.{cpu_power,gpu_power,ane_power,dram_power,e_cluster_active,p_cluster_active,soc_temp,...}`, `memory.{total,used,swap_*}`, `cpu_usage`, `core_usages[]`, `processes[].{pid,command,cpu_percent,gpu_ms_per_sec,memory_percent,rss_kb}` — the `processes[]` array is the correct pid-join key for a multi-session fleet view (better than `--pid`, which only tracks one process at a time).
- `mactop --headless --prometheus :PORT --count 0` (backgrounded) — verified serving standard Prometheus text (`mactop_*` gauges) within ~1.5s, no sudo. Must always pair `--prometheus` with `--headless`: without it, a backgrounded/no-tty launch dies on `open /dev/tty: device not configured`.
- No cost field anywhere in mactop output either — it's a pure hardware feed, orthogonal to the pricing-table need above.

## Common metrics people measure

Tokens in/out/cache (create + read), cache-hit ratio, estimated $ cost per model, burn rate (tokens/min or $/hr), context-window fill %, active/waiting/idle session state, tool-call and subagent/delegation counts, 5-hour rolling session-block tracking (Claude-specific), daily/monthly cost forecasts — on the agent side; CPU (per-core, E/P/S cluster on Apple Silicon), GPU/ANE utilization and power, memory + swap, network throughput, disk I/O, temperature/thermal state, battery, and per-process CPU/mem — on the system side.

## UI conventions

Gauge/meter bars for instantaneous CPU/GPU/memory (mactop, macmon, btop); braille-density line graphs degrading to block/ASCII on limited terminals (btop) as the default "nice" time-series rendering; sparklines in compact/menubar views; zoomable/scrollable history charts rather than snapshot-only (zenith, bottom, macmon). Context-window fill is conventionally a percentage bar with color-coded warnings (abtop, Claude-Code-Usage-Monitor) — same visual idiom as a battery/disk-usage bar. Live per-session state is typically a status word/badge (working/waiting/idle/error) with a spinner on the active line, not a numeric gauge. Most tools default to a 1–2s poll or fs-watcher refresh. Rust+ratatui is the dominant stack for the newer agent-monitor TUIs; Node/TS for the older/broader-coverage ones; Go+Bubble Tea appears once (agent-deck) and matches mactop's own Go choice. Named color-theme switching (macmon's 6 themes; the separate `ratatui-themes` crate with Dracula/Nord/Catppuccin/Gruvbox/Tokyo Night) is common in the Rust ecosystem; mactop itself uses raw hex/named-color flags, no curated preset pack. Nearly every agent-monitor explicitly brands itself against htop/btop by name (abtop, aitop, agtop, agenttop, tokentop, agent-htop, claudetop) even sharing no code with the real thing.

## Reuse candidates

- `mactop --headless --prometheus :PORT --count 0` as a long-lived background system-metrics feed (Prometheus text, cheap to scrape repeatedly), or `--headless --format json --count 1` for a simple one-shot polling script. Either avoids reimplementing IOReport — do not attempt to call the Apple IOReport/SMC APIs directly.
- `~/.claude/sessions/<pid>.json` as the primary Claude Code liveness signal, over tailing the transcript, for busy/waiting status.
- `codeburn`'s `docs/providers/*.md` as the most complete public reference for per-agent on-disk formats (paths and record shapes across 40+ agents) — read rather than re-deriving parsers from scratch.
- A LiteLLM pricing table or models.dev feed as the mandatory cost/burn-rate source, since neither agent transcript carries a cost field.
- macmon (Rust, `serve` subcommand with `/json` and `/metrics`, embeddable as a library) as an alternative system feed to shelling out to mactop, if that's preferred.
- abtop (Rust/ratatui, matches installed Rust 1.95) or agtop (Node, matches Node 24) as a starting base to extend with an Apple-Silicon collector, rather than writing the agent-session parsing layer from scratch; agent-deck (Go/Bubble Tea) matches the installed Go 1.27 toolchain and mactop's own stack choice.

## Naming note

`codeburn` is taken — github.com/getagentseal/codeburn is a real, actively shipping tool (`npx codeburn`, codeburn.app, 41-agent coverage). Do not name this project codeburn or any close variant of that phrase.
