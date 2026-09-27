---
title: Usage
description: wattop's flags, keybindings, token-rate definitions, subagent tree and JSON output.
---

```sh
wattop                    # interactive TUI
wattop --theme nord       # or WATTOP_THEME=nord
wattop --interval 2s      # SoC sample interval, 500ms-5s
wattop --json             # one Snapshot per interval as NDJSON, no alt screen
wattop --json --once      # exactly one Snapshot, then exit
wattop --no-color         # or NO_COLOR=1: no ANSI styling, gauges as blocks
wattop --demo             # synthetic machine and sessions for screenshots; reads no local data
wattop doctor             # what actually resolved on this chip
wattop doctor --ioreport-groups  # + every IOReport group and its channel count
```

## Flags

| Flag | Meaning |
|---|---|
| `--demo` | Synthetic data for screenshots. Reads no local data. |
| `--interval <duration>` | SoC sample interval, 500ms–5s (default: `config.toml`, then 1s). |
| `--json` | Print one Snapshot per interval as NDJSON and never enter the alt screen. |
| `--no-color` | Disable all ANSI styling. |
| `--once` | With `--json`, print exactly one Snapshot and exit. |
| `--theme <name\|hex>` | Theme name or bare hex accent (default: `$WATTOP_THEME`, then `config.toml`, then `wattop-dark`). |
| `--version` | Print the version and exit. |

`wattop doctor` prints which SoC channels, agent sources, pricing and theme resolve on this machine. `--ioreport-groups` adds every IOReport group with its channel count.

## Keybindings

| Key | Action |
|---|---|
| <kbd>↑</kbd>/<kbd>k</kbd>, <kbd>↓</kbd>/<kbd>j</kbd> | Move selection |
| <kbd>enter</kbd> | Toggle the session detail view |
| <kbd>t</kbd> / <kbd>T</kbd> | Cycle theme forward / back |
| <kbd>s</kbd> | Cycle sort (status → cost → burn → cpu) |
| <kbd>f</kbd> | Toggle subagent rows |
| <kbd>a</kbd> | Show dormant sessions (stale rows bound to no live process are hidden by default, and the footer says how many) |
| <kbd>p</kbd> | Pause the display (collection keeps running) |
| <kbd>g</kbd> | Toggle history graphs / full hardware meters |
| <kbd>?</kbd> | Toggle the help overlay |
| <kbd>q</kbd> / <kbd>ctrl+c</kbd> | Quit |

## Token rates

The default view pairs two-minute input/output token graphs with CPU, GPU, and power history. `OUT/s` in each session row is output tokens recorded over the last 60 seconds, divided by 60, including idle time. Input throughput includes cache reads and writes; output includes reported reasoning tokens. These are **transcript activity rates, not instantaneous model generation speed**.

Rates work for Claude, its subagents, and Codex without telemetry setup. Unknown usage is `—`; observed inactivity is `0.0`. Press <kbd>g</kbd> for the full hardware meters and <kbd>enter</kbd> for cumulative token counts and the selected session's rates.

## Subagents and workflows

Subagents are tracked as a tree under their session: Claude `Agent` spawns (including nested and background ones), Claude workflow runs, and Codex spawned and guardian threads. Each child shows its status (`● run`, `idle`, `done`, `fail`), the tool it is waiting on, output rate, cost and its own `$/hr`.

A workflow collapses to one row with its phase and running/done counts; <kbd>enter</kbd> opens the full tree, and the `SA` column reads running/total. A `~` before a cost means it leaves out usage, the session's own or a child's, on a model the pricing table does not know.

## Terminal width

The dashboard fits at 80 columns and above; the detail view needs 103 columns. The table narrows: columns shrink, the context gauge collapses to `~ 55%`, output rates use compact figures, surplus rows become a `▼ N more` marker, and `$`, `$/HR`, `CPU%` and `RSS` survive at these widths. The detail view (<kbd>enter</kbd>) has not been narrowed yet and is cut off below 103 columns.

## JSON output

`--json` prints one Snapshot per interval as newline-delimited JSON and never enters the alt screen, so it pipes cleanly:

```sh
wattop --json --once | jq '.sessions[] | {agent, status, model, cost_usd, burn_usd_per_hr}'
```

A Snapshot carries `token_rate`, `sys` (clusters, GPU, power, bandwidth, temps, fans, thermal state, memory, net, disk and a `missing` list of unresolved channels), `sessions` (each with `usage`, `cost_usd`, `burn_usd_per_hr`, `context_used`/`context_max`, `subagents`, `workflows` and its bound `proc`), `total_cost_usd`, `total_burn_usd_per_hr` and `self_cpu_pct`.

:::caution
`--json` output includes session working directories, process command lines and subagent descriptions. Treat it like your shell history before sharing it.
:::
