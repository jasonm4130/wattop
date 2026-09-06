# wattop test corpus

Everything under this directory is committed test data. Every consumer
resolves this directory with `fixture.CorpusDir()` (see
`internal/fixture/corpus.go`), never a relative literal, so the files here
are reachable from any package directory in the module.

Captured on: Apple M5 Max, macOS (darwin/arm64), 2026-09-06.
Software versions on the capture machine: mactop v2.1.5, Claude Code
2.1.261, Codex CLI 0.151.0.

Every file here has been passed through `cmd/wattop-scrub`
(`internal/fixture.Scrub`), which rewrites the recording machine's home
path (`/Users/<the recording account>` → `/home/u`) and account name,
replaces human prose with
length-preserving filler, and rewrites UUIDs/session ids to a deterministic
hash so cross-record references (a `parentUuid` pointing at another
record's `uuid`, a `toolUseId` matching a subagent's `meta.json`) still
line up. `go test ./internal/fixture/... -run TestNoRealPathsInCommittedFixtures`
enforces that no real path or username leaked through.

## Provenance

The `soc/` files are a genuine capture from this machine, taken with mactop
and then scrubbed (mactop's headless JSON does not carry any of this
machine's identifying strings itself, but the capture is piped through the
same tool for the same reason everything else is: the pipeline is the thing
under test, not just this one dataset).

The `agent/` files are **hand-authored to the exact field shapes and key
names observed in this machine's own real Claude Code session files**
(`~/.claude/sessions/<pid>.json`) **and real Claude/Codex transcripts**
(`~/.claude/projects/**/*.jsonl`, `~/.codex/sessions/**/*.jsonl`) — the
schemas, field names, and representative values below (`usage` shapes,
`rate_limits` shapes, `cache_creation` shapes) were read directly off real
records on this machine before being written into these fixtures. The
prose, ids, and scenario framing are synthetic: several of the required
scenarios (a specific rejected-quota record landing on a coincidentally
useful pid, three concurrent subagents with one still running, a
mid-write-truncated line) don't reliably exist in this user's history on
demand, and literal excerpts of real conversations are not something to
commit to a public test corpus regardless. Each fixture was then still
run through `cmd/wattop-scrub` for the same redaction guarantee as
everything else, and the scrub tool's own tests
(`internal/fixture/scrub_test.go`) are what actually enforce the "no real
path or username" property — not a claim made here.

## `soc/`

- **`mactop-ndjson.jsonl`** — `mactop --headless --format json --count 0 --interval 1000`,
  captured live under a 45 s timeout (34 lines landed). One JSON object per
  line. This is the framing `internal/soc` should expect from a long-running
  headless mactop process.
- **`mactop-array.json`** — `mactop --headless --format json --count 1`.
  Emits a JSON **array** (`[{...}]`), not NDJSON — a decoder written only
  against `mactop-ndjson.jsonl` will break on this file. mactop even splits
  the array's `[`, the object, and the `]` across separate physical lines,
  which is why `cmd/wattop-scrub` treats every `.json` file as one JSON
  document read whole, never line-by-line.
- **`mactop-degraded.jsonl`** — a copy of `mactop-ndjson.jsonl` with the
  following keys deleted from every record's `soc_metrics` object:
  `ane_active`, `dram_read_bw_gbs`, `dram_write_bw_gbs`,
  `dram_bw_combined_gbs`; and the top-level `fans` array removed entirely.
  This is the fixture for the missing-channel / version-drift path:
  `SysSample.Missing` (Task 4) must name `ane_active`,
  `dram_read_bw_gbs`/`dram_write_bw_gbs`/`dram_bw_combined_gbs`, and `fans`
  when decoding this file, and every other field must still decode
  normally.

## `agent/claude/`

- **`busy-interactive.jsonl`** — an interactive session transcript whose
  last turn is an assistant `tool_use` with no matching `tool_result`
  yet: the busy signal a transcript-only reader can see without the
  separate `~/.claude/sessions/<pid>.json` status file.
- **`waiting.jsonl`** — a transcript whose last turn is a completed
  assistant text reply: the turn finished and the session is idle, waiting
  on the next user message.
- **`sessions/missing-status.json`** — the session-state file
  (`~/.claude/sessions/<pid>.json` shape) for pid `99571`, with **no
  `status` field at all**. This must render "unknown", never default to
  idle. (`jq -e '.status == null'` passes because the key is absent, which
  decodes to a JSON `null` under `jq`'s `.status` lookup.)
- **`subagent-tree.jsonl`** plus **`subagents/agent-1.jsonl`**,
  **`subagents/agent-2.jsonl`**, **`subagents/agent-3.jsonl`** and their
  `.meta.json` siblings — a parent turn that fans out three concurrent
  `Task` tool calls. `agent-1` and `agent-2` have matching `tool_result`s
  in the parent transcript and finished normally; `agent-3`'s `tool_use`
  has no matching `tool_result` in the parent, and `agent-3.jsonl` ends
  mid-tool-call — it is still running (`agent-3.meta.json` has
  `"live":true`). The three `toolUseId`s in the parent's `tool_use` blocks
  match the `toolUseId` in each subagent's `meta.json` exactly (both were
  scrubbed from the same original id, and `Scrub`'s id rewrite is a
  deterministic hash of the input, so independent redaction of the parent
  and each child still agrees).
- **`headless-child.json`** — a session-state file for a `claude -p` run:
  `"kind":"sdk-cli"` and `"entrypoint":"sdk-cli"`, unlike an interactive
  session's `"kind":"interactive"`/`"entrypoint":"cli"`.
- **`rate-limited.jsonl`** — a `quotaLimits` record with
  `{"status":"rejected","rateLimitType":"five_hour","resetsAt":...,"overageDisabledReason":...}`.
- **`truncated-final-line.jsonl`** — two well-formed lines followed by a
  deliberately partial JSON object with no closing braces or trailing
  newline: what a byte-offset tailer actually sees mid-write. `Scrub`
  itself rejects this line (`TestScrubRejectsTruncatedJSON`); the file is
  otherwise unscrubbed at that final line because there's nothing valid to
  parse.
- **`cache-1h.jsonl`** — an assistant record whose
  `message.usage.cache_creation` has `ephemeral_1h_input_tokens > 0` and
  `ephemeral_5m_input_tokens == 0` (`jq -e` in the acceptance check
  confirms this). This shape genuinely occurs on this machine and is the
  case the plan's 37.5%-undercount correction is about: pricing a 1-hour
  cache write at the 5-minute rate.

## `agent/codex/`

- **`full-turn.jsonl`** — one complete turn:
  `session_meta` → `turn_context` → `task_started` (`event_msg`) →
  `token_count` (`event_msg`, carrying both `info.total_token_usage` and
  the top-level `rate_limits` block) → `task_complete` (`event_msg`, with
  `last_agent_message`).
- **`rate-limits.jsonl`** — a `turn_context` and `token_count` pair with
  `rate_limits.primary.used_percent` and `.secondary.used_percent` both
  populated and elevated (92.5% / 61.0%), for the rate-limit gauge path.
- **`model-override.jsonl`** — `turn_context.payload.model` is
  `"gpt-5.6-terra-mini"`, different from `"gpt-5.6-terra"` used in
  `full-turn.jsonl` and `rate-limits.jsonl` — proving model must be read
  per turn from `turn_context`, never assumed constant for a rollout or
  taken from `~/.codex/config.toml`'s default.

## Regenerating

`scripts/fixtures.sh` re-captures the `soc/` files (it re-runs mactop and
re-scrubs; it does not touch `agent/`, which is hand-maintained — see
`internal/fixture/scrub.go` for the redaction rules and
`cmd/wattop-scrub/main.go` for the CLI). Requires this machine (mactop,
passwordless, at `/opt/homebrew/bin/mactop`); the tests over the committed
output run anywhere, including Linux CI.
