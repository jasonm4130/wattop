---
title: Overview
description: What wattop is, what it shows, and what it needs.
---

**wattop** is a terminal monitor for Apple Silicon Macs that watches your coding agents and your hardware in one pane: every running Claude Code and Codex session, its subagents and workflows, and the CPU, GPU, power, thermals and memory of the machine running them.

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

## How it compares

| Tool | Hardware power & sensors | Claude Code / Codex sessions | Subagent tree | Live TUI | Needs sudo |
|---|:-:|:-:|:-:|:-:|:-:|
| **wattop** | yes | both, live | yes | yes | no |
| [mactop](https://github.com/metaspartan/mactop) | yes | no | no | yes | no |
| [asitop](https://github.com/tlkh/asitop) | yes | no | no | yes | yes |
| [ccusage](https://github.com/ryoppippi/ccusage) | no | both, plus others (reports) | no | no | no |
| [Claude Code Usage Monitor](https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor) | no | Claude, live | no | yes | no |

wattop vendors mactop's IOReport/SMC collector. Comparison checked against each project's README in September 2026. If you only want hardware, mactop is excellent; if you want historical usage reports across many agents, ccusage is. wattop is for watching live agents and the machine running them at once.

## Requirements

Apple Silicon (arm64) with macOS 14 or newer. wattop links Apple's private IOReport framework through CGO, so there is no Linux or Intel build. Hardware support varies by chip and macOS version; run [`wattop doctor`](/docs/doctor/) to see what resolved on yours, and see [limitations](/docs/limitations/).

This is an early-stage project. Token rates are 60-second transcript averages, and dollar figures are API-price estimates, not subscription balances. See [honest labelling](/docs/honest-labelling/).

## Documentation

- [Install](/docs/install/): Homebrew, a release archive, `go install`, or build from source.
- [Usage](/docs/usage/): every flag, every key, token rates, the subagent tree and the JSON stream.
- [Config file](/docs/config/): `config.toml` keys, defaults and precedence.
- [Themes](/docs/themes/): the four themes, hex accents and `NO_COLOR`.
- [Doctor](/docs/doctor/): what `wattop doctor` reports and when to run it.
- [Honest labelling](/docs/honest-labelling/): what each number is, and what it is not.
- [Pricing table](/docs/pricing/): where costs come from and how the table refreshes.
- [Limitations](/docs/limitations/): known gaps, each with its evidence.
- [Privacy](/docs/privacy/): what wattop reads, and its one network request.
- [Troubleshooting / FAQ](/docs/troubleshooting/): `$0.00/hr`, missing sessions, `0 RPM` fans and more.
