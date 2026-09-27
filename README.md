# wattop

[![CI](https://github.com/jasonm4130/wattop/actions/workflows/ci.yml/badge.svg)](https://github.com/jasonm4130/wattop/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/jasonm4130/wattop)](https://github.com/jasonm4130/wattop/releases/latest)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**A terminal monitor for Apple Silicon Macs that watches your coding agents and your hardware in one pane.**

**Docs: [wattop.app](https://wattop.app/docs/)**

![wattop showing hardware and token throughput graphs with Claude and Codex sessions](docs/assets/wattop.gif)

*Synthetic demonstration data from `wattop --demo` (re-record with `make demo`);
rates and costs are illustrative. [Still image](docs/assets/wattop.png).*

```sh
brew install --cask jasonm4130/wattop/wattop
wattop
```

- **Claude Code and Codex, live.** Every running session with its status, output tokens/s, context fill, estimated cost and `$/hr`, bound to its process's CPU, GPU and memory.
- **Subagents and workflows as a tree.** Claude `Agent` spawns (nested and background), workflow runs with phase and progress, and Codex child threads, each with its own rate and cost.
- **Apple Silicon hardware beside it.** CPU/GPU/ANE/DRAM power, E- and P-cluster load, GPU, temperatures, fans, thermal pressure, memory and swap, network and disk, with two-minute history graphs.
- **No sudo, no runtime dependencies.** Reads IOReport and SMC directly instead of wrapping `powermetrics` (which needs root); one binary, macOS system frameworks only.
- **Scriptable.** `--json` streams one snapshot per interval as NDJSON; `--once` prints one and exits.

## How it compares

| | Hardware power & sensors | Claude Code / Codex sessions | Subagent tree | Live TUI | Needs sudo |
|---|:-:|:-:|:-:|:-:|:-:|
| **wattop** | yes | both, live | yes | yes | no |
| [mactop](https://github.com/metaspartan/mactop) | yes | no | no | yes | no |
| [asitop](https://github.com/tlkh/asitop) | yes | no | no | yes | yes |
| [ccusage](https://github.com/ryoppippi/ccusage) | no | both, plus others (reports) | no | no | no |
| [Claude Code Usage Monitor](https://github.com/Maciek-roboblog/Claude-Code-Usage-Monitor) | no | Claude, live | no | yes | no |

wattop vendors mactop's IOReport/SMC collector (see [Attribution](#attribution)).
Comparison checked against each project's README in September 2026. If you
only want hardware, mactop is excellent; if you want historical usage reports
across many agents, ccusage is. wattop is for watching live agents and the
machine running them at once.

## Privacy

wattop runs entirely on your Mac, and nothing it reads leaves the machine.
It reads Claude Code and Codex transcripts under `~/.claude` and `~/.codex`
read-only and never writes to them. To find agent processes, its process
scanner enumerates the whole process table and reads the command line,
working directory, CPU, memory and GPU time of every process you own, not
only the ones sessions bind to. There is no telemetry and no account. The
only network request is a pricing-table refresh: at most one `GET` per 24
hours to
[LiteLLM's public price list](https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json)
on raw.githubusercontent.com, cached in `$XDG_CACHE_HOME/wattop/pricing.json` (default `~/.cache`). If it
fails, the table embedded in the binary is used. `--json` output includes
session working directories, process command lines and subagent
descriptions; treat it like your shell history before sharing it. Details:
[wattop.app/docs/privacy](https://wattop.app/docs/privacy/).

## Requirements

Apple Silicon (arm64) with macOS 14 or newer. wattop links Apple's private
IOReport framework through CGO, so there is no Linux or Intel build. Hardware
support varies by chip and macOS version; run `wattop doctor` to see what
resolved on yours, and see [limitations](https://wattop.app/docs/limitations/).

This is an early-stage project. Token rates are 60-second transcript averages,
and dollar figures are API-price estimates, not subscription balances.

## Install

```sh
brew install --cask jasonm4130/wattop/wattop
```

Update with `brew update && brew upgrade --cask wattop`.
The [official tap](https://github.com/jasonm4130/homebrew-wattop) tracks stable
releases. You can also download the arm64 archive from
[GitHub Releases](https://github.com/jasonm4130/wattop/releases/latest).
Release archives include checksums and GitHub build provenance; they are not
Apple-notarized. The cask removes quarantine from its installed `wattop` binary
to allow it to launch. See [verification instructions](docs/releasing.md#verify-a-download).

With Go 1.27 and the Xcode command line tools:

```sh
go install github.com/jasonm4130/wattop/cmd/wattop@latest
```

### Build from source

```sh
git clone https://github.com/jasonm4130/wattop.git
cd wattop
make build
./bin/wattop
```

The build enables CGO for IOReport and SMC. The examples below assume `wattop`
is on your `PATH`; otherwise use `./bin/wattop`. Tagged releases are built by
GitHub Actions with a macOS 14 deployment target.

## Documentation

Full docs live at **[wattop.app/docs](https://wattop.app/docs/)**.

```sh
wattop                   # interactive TUI
wattop --demo            # synthetic data, reads nothing local
wattop doctor            # what resolved on this machine
wattop --json --once | jq
wattop --help            # every flag
```

- [Install](https://wattop.app/docs/install/): Homebrew, release archive, `go install`, source
- [Usage](https://wattop.app/docs/usage/): flags, keybindings, token rates, subagent tree, `--json`
- [Config file](https://wattop.app/docs/config/): `config.toml` keys, defaults and precedence
- [Themes](https://wattop.app/docs/themes/): four themes, hex accents, `NO_COLOR`
- [Doctor](https://wattop.app/docs/doctor/): what `wattop doctor` reports and when to run it
- [Honest labelling](https://wattop.app/docs/honest-labelling/): what each number is and is not, plus self-CPU overhead
- [Pricing table](https://wattop.app/docs/pricing/): where costs come from and how the table refreshes
- [Limitations](https://wattop.app/docs/limitations/): known gaps, each with its evidence
- [Privacy](https://wattop.app/docs/privacy/): what wattop reads and its one network request
- [Troubleshooting / FAQ](https://wattop.app/docs/troubleshooting/): `$0.00/hr`, missing sessions, `0 RPM` fans and more

## Attribution

wattop vendors and depends on the following MIT-licensed projects — details in [`NOTICE`](NOTICE) and the vendored
[mactop license](internal/soc/mactop/LICENSE):

- **[mactop](https://github.com/metaspartan/mactop)** — Copyright ©
  2024-2026 Carsen Klock. `internal/soc/mactop` is a vendored copy of its
  IOReport/SMC collector layer, pinned to a specific upstream commit (see
  `internal/soc/VENDOR.md`), because that code lives under mactop's own
  `internal/app` and cannot be imported.
- **[ntcharts](https://github.com/NimbleMarkets/ntcharts)** — Copyright ©
  2024-2026 Neomantra Corp. Used as an ordinary dependency for the braille
  sparkline helper.

Also depends on the Charm libraries (Bubble Tea, Lip Gloss, Bubbles),
`BurntSushi/toml`, and `golang.org/x/term`.

## Contributing

Start with [CONTRIBUTING.md](CONTRIBUTING.md) for builds, tests, and pull requests.
[Report a bug](https://github.com/jasonm4130/wattop/issues/new/choose),
[report a vulnerability privately](SECURITY.md), or read the
[code of conduct](CODE_OF_CONDUCT.md).

## License

MIT — see [`LICENSE`](LICENSE).
