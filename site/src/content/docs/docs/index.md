---
title: Overview
description: What wattop is, what it shows, and what it needs.
---

**wattop** is a terminal monitor for Apple Silicon Macs that watches your coding agents and your hardware in one pane.

```sh
brew install --cask jasonm4130/wattop/wattop
wattop
```

## What it shows

- **Claude Code and Codex, live.** Every running session with its status, output tokens/s, context fill, estimated cost and `$/hr`, bound to its process's CPU, GPU and memory.
- **Subagents and workflows as a tree.** Claude `Agent` spawns (nested and background), workflow runs with phase and progress, and Codex child threads, each with its own rate and cost.
- **Apple Silicon hardware beside it.** CPU/GPU/ANE/DRAM power, E- and P-cluster load, GPU, temperatures, fans, thermal pressure, memory and swap, network and disk, with two-minute history graphs.
- **No sudo, no runtime dependencies.** Reads IOReport and SMC directly instead of wrapping `powermetrics` (which needs root); one binary, macOS system frameworks only.
- **Scriptable.** `--json` streams one snapshot per interval as NDJSON; `--once` prints one and exits.

## Requirements

Apple Silicon (arm64) with macOS 14 or newer. wattop links Apple's private IOReport framework through CGO, so there is no Linux or Intel build. Hardware support varies by chip and macOS version; run [`wattop doctor`](/docs/doctor/) to see what resolved on yours.

This is an early-stage project. Token rates are 60-second transcript averages, and dollar figures are API-price estimates, not subscription balances. See [honest labelling](/docs/honest-labelling/).

## Next

- [Install](/docs/install/): Homebrew, `go install`, or build from source.
- [Usage](/docs/usage/): flags, keybindings and the JSON stream.
- [Privacy](/docs/privacy/): what wattop reads, and its one network request.
