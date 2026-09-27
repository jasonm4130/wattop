---
title: Usage
description: wattop's flags, keybindings, token-rate definitions, subagent tree and JSON output.
---

```sh
wattop                  # interactive TUI
wattop --theme nord     # or WATTOP_THEME=nord
wattop --interval 2s    # SoC sample interval, 500ms-5s
wattop --json           # NDJSON, one Snapshot per interval
wattop --json --once    # exactly one Snapshot, then exit
wattop --no-color       # or NO_COLOR=1
wattop --demo           # synthetic data; reads no local data
wattop doctor           # what resolved on this chip
wattop doctor --ioreport-groups
```

## Flags

This is the full flag surface; `wattop --help` (or `-h`) prints the same list.

| Flag | Meaning |
|---|---|
| `--demo` | Synthetic data for screenshots. A synthetic machine and synthetic Claude and Codex sessions; reads no local data and ignores `config.toml`. |
| `--interval <duration>` | SoC sample interval, 500ms-5s (default: `config.toml`, then 1s). This is the whole refresh period. A value outside the range is clamped with a warning, never rejected. |
| `--json` | Print one Snapshot per interval as NDJSON and never enter the alt screen. |
| `--no-color` | Disable all ANSI styling. Gauges render as plain blocks. Setting `NO_COLOR` to any non-empty value does the same. |
| `--once` | With `--json`, print exactly one Snapshot and exit. Without `--json` it has no effect. |
| `--theme <name\|hex>` | Theme name or bare hex accent (default: `$WATTOP_THEME`, then `config.toml`, then `wattop-dark`). An unknown name falls back to `wattop-dark` with a warning. See [Themes](/docs/themes/). |
| `--version` | Print the version and exit. |

Go's flag parser accepts both `-json` and `--json`. An unknown flag prints the error and the usage to stderr and exits with status 2.

### `wattop doctor`

`wattop doctor` prints which SoC channels, agent sources, pricing and theme resolve on this machine, then exits. It takes one flag of its own:

| Flag | Meaning |
|---|---|
| `--ioreport-groups` | Enumerate IOReport groups with channel counts. |

See [Doctor](/docs/doctor/) for what each section means.

## Keybindings

| Key | Action |
|---|---|
| <kbd>↑</kbd>/<kbd>k</kbd>, <kbd>↓</kbd>/<kbd>j</kbd> | Move selection |
| <kbd>enter</kbd> | Toggle the session detail view |
| <kbd>t</kbd> / <kbd>T</kbd> | Cycle theme forward / back |
| <kbd>s</kbd> | Cycle sort (status → cost → burn → cpu) |
| <kbd>f</kbd> | Toggle subagent rows (hides every session's subagent and workflow rows) |
| <kbd>a</kbd> | Show dormant sessions. Stale rows bound to no live process are hidden by default, and the footer says how many (`N hidden (a)`) |
| <kbd>p</kbd> | Pause the display (collection keeps running); the footer shows `[PAUSED]` |
| <kbd>g</kbd> | Toggle history graphs / full hardware meters |
| <kbd>?</kbd> | Toggle the help overlay |
| <kbd>q</kbd> / <kbd>ctrl+c</kbd> | Quit |

Under every sort, a live session always sorts above a dormant one; the chosen metric orders rows within that. GPU is never a sort key (see [honest labelling](/docs/honest-labelling/)). The history graphs need a terminal of at least 80x24; below that wattop shows the hardware meters.

## Token rates

The default view pairs two-minute input/output token graphs with CPU, GPU, and power history. `OUT/s` in each session row is output tokens recorded over the last 60 seconds, divided by 60, including idle time. Input throughput includes cache reads and writes; output includes reported reasoning tokens. These are **transcript activity rates, not instantaneous model generation speed**.

Rates work for Claude, its subagents, and Codex without telemetry setup. Unknown usage is `—`; observed inactivity is `0.0`. Press <kbd>g</kbd> for the full hardware meters and <kbd>enter</kbd> for cumulative token counts and the selected session's rates.

## Subagents and workflows

Subagents are tracked as a tree under their session: Claude `Agent` spawns (including nested and background ones), Claude workflow runs, and Codex spawned and guardian threads. Each child shows its status (`● run`, `idle`, `done`, `fail`), the tool it is waiting on, output rate, cost and its own `$/hr`.

A workflow collapses to one row with its phase and running/done counts; <kbd>enter</kbd> opens the full tree, and the `SA` column reads running/total. A `~` before a cost means it leaves out usage, the session's own or a child's, on a model the pricing table does not know.

Subagent status is inferred from transcripts; see [limitations](/docs/limitations/#subagent-status-is-inferred-from-transcripts).

## Terminal width

**The dashboard fits at 80 columns and above; the detail view needs 103 columns.** The table narrows: columns shrink, the context gauge collapses to `~ 55%`, output rates use compact figures, surplus rows become a `▼ N more` marker, and `$`, `$/HR`, `CPU%` and `RSS` survive at these widths. Measured at 80x24 on live sessions it renders exactly 80 cells with nothing past the terminal edge.

The **detail view** (<kbd>enter</kbd>) has not been narrowed and still reserves 103 cells, so it is cut off below that. See [limitations](/docs/limitations/#the-detail-view-does-not-fit-a-terminal-narrower-than-103-columns).

## JSON output

`--json` prints one Snapshot per interval as newline-delimited JSON (one compact object per line) and never enters the alt screen, so it pipes cleanly. `--json --once` prints exactly one and exits; it takes one full `--interval` before it prints, because the first hardware sample is a real power delta rather than an instantaneous zero.

```sh
wattop --json --once \
  | jq '.sessions[]
        | {agent, status, model,
           cost_usd, burn_usd_per_hr}'
```

The top-level shape, trimmed (from `wattop --demo --json --once | jq`):

```json
{
  "token_rate": {
    "input_per_sec": 10609,
    "output_per_sec": 166.7,
    "cache_read_per_sec": 9830.9
  },
  "at": "2026-09-28T07:46:41.90279+10:00",
  "sys": {
    "soc_name": "Apple M4 Pro",
    "missing": ["dram_read_bw_gbs",
                "dram_write_bw_gbs"]
  },
  "sessions": [
    {
      "agent": "claude",
      "status": "busy",
      "model": "claude-opus-5",
      "cost_usd": 7.62,
      "burn_usd_per_hr": 9.49,
      "context_used": 351900,
      "context_max": 1000000,
      "context_exact": false,
      "pid": 48213,
      "cwd": "~/code/api-server"
    }
  ],
  "total_cost_usd": 19.83,
  "total_cost_partial": false,
  "total_burn_usd_per_hr": 26.25,
  "unpriced_models": [],
  "degraded": [],
  "self_cpu_pct": 1.9
}
```

- `sys` carries `soc_name`, `clusters`, `gpu`, `power`, `bandwidth`, `temps`, `fans`, `thermal_state`, `throttled`, `memory`, `net`, `disk` and `missing`, the list of channels that did not resolve.
- Each entry in `sessions` also carries `id`, `bind_conf`, `name`, `cmdline`, `kind`, `status_since`, `usage`, `priced`, `cost_partial`, `tools`, `tool_counts`, `subagents`, `workflows`, `last_usage_at`, `rate_limits` and its bound `proc` (pid, `argv`, `cwd`, RSS, CPU, disk and GPU figures).
- Measured fields wattop has no source for are generally `null` rather than `0`, and named in `sys.missing` (fan `rpm` is the exception; see [limitations](/docs/limitations/#fan-rpm-can-read-a-stuck-0-for-a-processs-whole-lifetime)).
- `--json` lists every session, including the dormant ones the TUI hides by default, and every workflow agent in full.

:::danger
`--json` output includes working directories, process command lines (argv) and subagent descriptions. Treat it like shell history before you share it.
:::
