---
title: Privacy
description: wattop runs entirely on your Mac. What it reads, what it writes, and its single network request.
---

wattop runs entirely on your Mac. There is no telemetry, no analytics and no account. Nothing wattop reads leaves the machine; the only network request it ever makes is the pricing-table refresh described below.

## What it reads

**Agent transcripts.** wattop reads Claude Code and Codex transcripts under `~/.claude` and `~/.codex` read-only and never writes to them. Specifically: `~/.claude/sessions`, `~/.claude/projects` and `~/.codex/sessions`.

**The whole process table.** To find agent processes and bind sessions to them, the process scanner enumerates **every process on the machine** each cycle (`sysctl(KERN_PROC_ALL)`), not only the processes sessions end up bound to. For every process owned by your user it reads:

- pid, command name and start time;
- the full command line (argv, via `KERN_PROCARGS2`);
- the current working directory;
- resident memory, CPU time and disk read/write counters;
- per-process GPU time.

Processes owned by other users are enumerated but skipped: macOS only lets an unprivileged process read that detail for its own user's processes, and wattop never runs as root. The kernel call that returns a process's arguments also returns its environment in the same buffer; wattop parses only the arguments and discards the rest. None of this is written to disk or sent anywhere.

**Hardware.** Apple Silicon power, temperature, fan and bandwidth sensors through IOReport and SMC, plus system memory, network and disk counters.

**Configuration.** `$XDG_CONFIG_HOME/wattop/config.toml` (or `~/.config/wattop/config.toml`) if you create one; wattop does not create it. See [Config file](/docs/config/).

`--demo` reads none of the above: it runs on synthetic data, ignores `config.toml` and makes no network request.

## Network

The only network request is a pricing-table refresh: at most one `GET` per 24 hours to [LiteLLM's public price list](https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json) on raw.githubusercontent.com. It is a plain download of a public file: it carries nothing about your machine or sessions beyond what any HTTP request carries (your IP address and Go's default `User-Agent`).

The result is cached in `$XDG_CACHE_HOME/wattop/pricing.json` (default `~/.cache/wattop/pricing.json`). If the request fails, the table embedded in the binary is used, so pricing works offline. See [Pricing table](/docs/pricing/).

## What it writes

Only the pricing cache above. wattop does not write to `~/.claude`, `~/.codex` or `config.toml`, and keeps no logs or history on disk.

## JSON output

:::danger
`--json` output includes working directories, process command lines (argv) and subagent descriptions. Treat it like shell history before you share it.
:::

`--json` includes the bound process (`proc`, with its `argv` and `cwd`) for each session, not the whole process table. See [Usage](/docs/usage/#json-output).

## This website

wattop.app uses [Skopia](https://skopia.dev) for analytics: cookieless, and it collects no personal data. This applies to the website only. The wattop binary has no analytics or telemetry of any kind.
