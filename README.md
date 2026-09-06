# wattop

SoC watts and AI coding-agent dollars, read on one refresh clock and joined
by pid.

A terminal dashboard for Apple Silicon: the same per-cluster power,
thermal and GPU numbers `mactop` shows, next to a live table of your Claude
Code and Codex CLI sessions — status, tokens, cost, burn rate — with each
session's CPU/GPU/RSS sampled at the exact same instant as the SoC read, so
"is this Claude session the thing spiking the GPU right now" has an actual
answer instead of two panels you have to eyeball together.

## Scope

macOS, Apple Silicon (arm64) only. Built with CGO against IOReport and SMC,
so it links `-lIOReport`, a private Apple framework — it is **not
cross-compilable** and there is no Linux or Intel build. No external
runtime: no Node, no Python, no subprocess, no Homebrew dependency at
runtime — macOS system frameworks only, one static-enough binary.

## Install

Via the Homebrew tap (once `jasonm4130/homebrew-wattop` exists and a release
has been published to it — see `.goreleaser.yml`'s `homebrew_casks` pipe):

```
brew install --cask jasonm4130/wattop/wattop
```

It ships as a cask rather than a formula because the release archive is a
pre-built binary, not a from-source build — the cask's `postflight` clears
the Gatekeeper quarantine flag the download picks up, which a formula would
not.

## Build from source

Requires Go 1.27 and Xcode command line tools (for CGO) on Apple Silicon.

```
make build
```

produces `bin/wattop`. The Makefile already sets `CGO_ENABLED=1
GOOS=darwin GOARCH=arm64` — `CGO_ENABLED=1` is not optional, since the SoC
panel is IOReport/SMC data reached through vendored Objective-C (see
`internal/soc/VENDOR.md`).

## Usage

```
wattop                    # interactive TUI
wattop --theme nord       # or WATTOP_THEME=nord
wattop --interval 2s      # SoC sample interval, 500ms-5s
wattop --json             # one Snapshot per interval as NDJSON, no alt screen
wattop --json --once      # exactly one Snapshot, then exit
wattop --no-color         # or NO_COLOR=1: no ANSI styling, gauges as blocks
wattop doctor             # what actually resolved on this chip
wattop doctor --ioreport-groups  # + every IOReport group and its channel count
```

**Terminal width: the session table needs 153 columns.** Below that it does
not narrow — it renders at its full width and the terminal wraps it. Size
the window wide before launching; see
[`docs/limitations.md`](docs/limitations.md).

`wattop doctor` is the first thing to run on a new machine or after a
macOS upgrade: it prints which SoC channels resolved, how many processes
the scanner enumerated and got a CPU baseline for, how many Claude/Codex
sessions were discovered and pid-bound, and the pricing table's age and
status — all without ever entering the TUI.

### Keybindings

| Key | Action |
|---|---|
| `↑`/`k`, `↓`/`j` | Move selection |
| `enter` | Toggle the session detail view |
| `t` / `T` | Cycle theme forward / back |
| `s` | Cycle sort (status → cost → burn → cpu) |
| `f` | Toggle subagent rows |
| `p` | Pause the display (collection keeps running) |
| `?` | Toggle the help overlay |
| `q` / `ctrl+c` | Quit |

### Themes

Four named themes, selected by `--theme`/`WATTOP_THEME` or cycled in-app
with `t`/`T`: `wattop-dark` (default), `wattop-light`, `nord`,
`catppuccin-mocha`. Every widget reads a semantic role (`idle`/`busy`/
`waiting`/`warn`/`hot`/…), never a raw palette color, so a theme swap
re-colours the whole dashboard consistently. `--theme <hex>` (e.g.
`--theme 58a6ff`) layers a bare accent color onto `wattop-dark` instead of
naming a file.

### Config file

`$XDG_CONFIG_HOME/wattop/config.toml` (or `~/.config/wattop/config.toml`)
sets defaults that flags and environment variables override: `theme`,
`interval_ms`, `burn_hot_usd_per_hr`, `codex_stale_minutes`, and
`context_window_overrides` (a Claude session id or cwd to a token count,
overriding the context-fill estimate for that session). A missing file is
not an error — every field just keeps its default.

## Pricing table

Costs are computed from an embedded, filtered snapshot of LiteLLM's pricing
table (`internal/pricing/table.json.gz`), refreshed in the background at
startup and cached, so pricing works instantly and offline on first run.
Run `make pricing` to pull the latest upstream table and regenerate
`docs/pricing-update.md` with the commit and checksum that produced it —
see that file for how to verify the checksum and add a model upstream
hasn't published yet.

## Honest labelling

- Claude context-fill is an estimate (dashed bar edge); Codex's is exact
  from `rate_limits` (solid edge).
- Costs are estimates from the pricing table above and **ignore
  subscription plans** (Claude Pro/Max, ChatGPT/Codex) entirely.
- An unpriced model renders `$—`, never `$0.00` — those mean different
  things.
- Per-session GPU is measured as ms/sec; the derived percent is a rescale
  and never the default sort key.
- DRAM and ANE bandwidth render `—` where the IOReport channel doesn't
  resolve, never `0.0`.

See [`docs/limitations.md`](docs/limitations.md) for the full list with
evidence, and [`docs/manual-qa.md`](docs/manual-qa.md) for the checklist
this release was verified against.

## Self-CPU overhead

Measured over a 60-second `--json` run on the M5 Max this was built on:
`Snapshot.self_cpu_pct` settles to **4.2%-7.0% (mean 5.35%)** in steady
state, corroborated independently by `ps -o %cpu` on the running process
(5.4%). The first ~5 samples after startup show a transient over-read of
190-300% before settling — not a sustained cost, but also not yet fully
explained; see [`docs/limitations.md`](docs/limitations.md) and
[`docs/manual-qa.md`](docs/manual-qa.md) item 11 for the raw sample
sequence. A monitor that distorts what it measures must be accountable for
its own overhead; this is that accounting.

## Attribution

wattop vendors and depends on the following MIT-licensed projects — full
text and details in [`NOTICE`](NOTICE):

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

## License

MIT — see [`LICENSE`](LICENSE).
