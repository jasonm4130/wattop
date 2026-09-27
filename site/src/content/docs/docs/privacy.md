---
title: Privacy
description: wattop runs entirely on your Mac. What it reads, what it writes, and its single network request.
---

wattop runs entirely on your Mac. There is no telemetry and no account.

## What it reads

Claude Code and Codex transcripts under `~/.claude` and `~/.codex`, **read-only**. wattop never writes to them. It also reads process information (CPU, GPU time, memory) for the processes those sessions are bound to, and Apple Silicon sensors through IOReport and SMC.

`--demo` reads no local data at all.

## Network

The only network request is a pricing-table refresh: at most one `GET` per 24 hours to [LiteLLM's public price list](https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json) on raw.githubusercontent.com.

The result is cached in `$XDG_CACHE_HOME/wattop/pricing.json` (default `~/.cache`). If the request fails, the table embedded in the binary is used, so pricing works offline on first run.

## What it writes

The pricing cache above. Configuration is read from `$XDG_CONFIG_HOME/wattop/config.toml` (or `~/.config/wattop/config.toml`) if you create one; wattop does not create it.

## JSON output

:::caution
`--json` output includes session working directories, process command lines and subagent descriptions. Treat it like your shell history before sharing it.
:::
