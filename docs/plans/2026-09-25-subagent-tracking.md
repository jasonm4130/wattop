# Subagent, workflow and Codex child tracking

wattop v0.1 shows direct Claude `Agent` spawns only. Jason asked on September
25, 2026 for full child tracking before v0.2.0.

## Evidence (local corpus, 2026-09-25)

- `Walk` skips subdirectories, so `subagents/workflows/wf_<id>/` is never read:
  103 direct `agent-*.meta.json` against 2,071 under workflows (95% invisible).
- Workflow agents' meta.json has no `toolUseId`. Each `wf_*` dir holds a
  `journal.jsonl` of `launched`, `started {agentId,label?,phase?}`,
  `result {agentId,result}` and `failed {agentId}` records. Journal `agentId`
  equals the `agent-<id>` filename stem (4,147/4,147). About half of `started`
  records carry no label/phase; fall back to meta `name`, then `description`.
- Nested spawns (`spawnDepth` 2) sit flat in `subagents/` with
  `parentAgentId` equal to the parent's stem (15/15). Their `tool_result` is in
  the parent agent's transcript, not the session's.
- Background spawns (`requestShape: "background"`) get a `tool_result` 1.4 s
  after launch ("Async agent launched successfully"), so v0.1 marks them done at
  launch. Completion arrives later in the parent transcript as a
  `queue-operation` record, `operation: "enqueue"`, whose `content` string holds
  `<task-notification><task-id>ID</task-id><tool-use-id>T</tool-use-id>
  <status>completed</status>…`.
- `stop_reason` is not a completion signal: most finished workflow agents end
  on a `tool_use` (structured output).
- Child and parent transcripts share no assistant `message.id` (0/2,043), so
  summing child usage onto the parent does not double-count.
- Codex: 348 of the last 400 rollouts are children, with `parent_thread_id` at
  the top of `session_meta.payload`: `source.subagent.thread_spawn`
  `{parent_thread_id, depth, agent_path, agent_nickname, agent_role}` or
  `source.subagent.other: "guardian"`. Children share the parent's
  `session_id` (216/348 equal), so keying on `session_id` collides; `payload.id`
  is the unique thread id. Child token totals start at zero, not the parent's.
- The `$0.00/hr` parent (docs/limitations.md) comes from timing burn by
  `ToolCall.At`, which freezes while a parent blocks on a child.

## Design

Domain (`internal/domain/types.go`): `Session.Subagents` stays flat with
`ParentID`/`WorkflowID`; `Session.Workflows` summarises each run;
`Session.LastUsageAt` is the newest usage timestamp in the session or any child.

Status is `running | idle | done | failed`. Done/failed only from a definitive
signal, always correlated to that child: workflow journal `result`/`failed`
whose `agentId` is the child's stem; a background task-notification whose
`<tool-use-id>` equals the child's meta `toolUseId` (`<status>completed` is
done, anything else failed); otherwise the child's `toolUseId` answered by a
`tool_result` in the session transcript or any child transcript (ignored for
background children, whose immediate ack is not completion). An unfinished
child is `running` while it has a `tool_use` awaiting its `tool_result`, or
while its newest record or observed growth is within 2 minutes; otherwise
`idle`. An unfinished parent with a running descendant is `running`. `Live` is
`Status == running`.

Claude `Subagent.ID` is the `agent-<id>` filename stem (never meta `hash`,
which differs), so `ParentID` (meta `parentAgentId`) and journal `agentId`
resolve against it.

Claude source reads children incrementally (per-file tail state that survives
a parent reset), re-reads a child only when its size or inode changed (a
definitive end never freezes accounting, so usage flushed after the journal
`result` is still counted), and decodes
the journal into a minimal struct that never retains `result` bodies.

Codex source keys sessions by thread id, excludes children from pid binding,
and folds each child into its root session's `Subagents`; a child whose root is
not visible stays a top-level session. No thread spans more than one rollout
file in the local corpus (840 threads); if two files ever share a thread id in
one poll, the later keeps a filename-suffixed id so they stay separate rows
rather than being summed.

Reducer: prices each subagent, gives each its own burn rate timed by
`LastActivityAt`, sums workflow cost/burn/rate from its agents,
sets `CostPartial` on a session or workflow whose total omits an unpriced child
(rendered with a `~` prefix and listed in UnpricedModels) and
`Snapshot.TotalCostPartial` when the total does, prices each child at its own
`ContextUsed` (last-request prompt size) so long-context tiers apply, and times
session burn by `max(ToolCall.At, LastUsageAt)`. Totals and history rings keep
summing sessions only, since session cost already includes children.

UI: the table shows each session's non-workflow subagents in tree order
(indented by depth) plus one collapsed row per workflow; the SA column reads
running/total. The detail panel shows workflows with their agents and the full
tree with status, phase, current tool, elapsed, cost and $/hr.

## Acceptance

`go vet`, `go test ./...` and `make build` pass. Against the live corpus,
`wattop --json` for a session with a finished workflow lists its agents and a
workflow total whose cost matches an independent sum over the agent jsonls;
a background agent reads running until its notification lands; a Codex child
appears under its parent rather than as a sibling. A running workflow in this
session shows live agents while it runs. Release v0.2.0 through the tap and
confirm `wattop --version` locally.
