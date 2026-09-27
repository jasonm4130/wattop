---
title: Doctor
description: wattop doctor prints what resolved on this machine, without entering the TUI.
---

```sh
wattop doctor
wattop doctor --ioreport-groups
```

`wattop doctor` is the first thing to run on a new machine or after a macOS upgrade. It samples one interval from the real collectors, prints a plain-text report of what resolved, and exits, all without entering the TUI. It reads `config.toml` for the interval and theme, as the dashboard does.

Run it when:

- you have just installed wattop, to see which hardware channels your chip exposes;
- you have upgraded macOS, since wattop depends on Apple's private IOReport framework;
- a dashboard field shows `—` and you want to know whether it resolved;
- a session you expect is missing, or shows no CPU/GPU/RSS;
- costs look wrong and you want the pricing table's age and status.

## Example

From an M5 Max:

```text
wattop doctor

soc:
  name: Apple M5 Max
  thermal_state: 0
  clusters:
    P: 12 cores
    S: 6 cores
channels:
  9 resolved, 4 unresolved
  ane_bw_combined_gbs: unresolved
  ane_power: resolved
  cpu_power: resolved
  cpu_temp: resolved
  dram_bw_combined_gbs: unresolved
  dram_power: resolved
  dram_read_bw_gbs: unresolved
  dram_write_bw_gbs: unresolved
  fans: resolved
  gpu_power: resolved
  gpu_temp: resolved
  soc_temp: resolved
  system_power: resolved

proc:
  rows: 582
  rows with a cpu percentage: 141
  gpu table rows: 20

sessions:
  claude: 5 discovered, 5 bound to a pid
  codex: 0 discovered, 0 bound to a pid
  total: 5 discovered, 5 bound to a pid

pricing:
  source: https://raw.githubusercontent.com/BerriAI/
    litellm/main/model_prices_and_context_window.json
  sha256: fbb16319dc9b1e1ced94cd532ee7a8cb8edbdb22e3
    11b12514872633067f7537
  entries: 4392
  age: 12h27m1s
  status: ok

theme: wattop-dark
```

The `source` and `sha256` values print on one line each; they are wrapped here to fit the page.

## Sections

**`soc`**: the chip name, the macOS thermal state as a number, and each CPU cluster with its core count. If the hardware sample fails, this says `sample failed:` and the error.

**`channels`**: how many of wattop's own hardware fields resolved on this machine, then each one by name as `resolved` or `unresolved`. These are wattop's fields, not IOReport group members. A field that is unresolved here renders `—` in the dashboard and `null` in `--json`. On the M5 Max above, no DRAM or ANE bandwidth counter resolves; see [limitations](/docs/limitations/#dram-bandwidth-on-this-chip-is-an-estimate-with-no-direction).

**`proc`**: the process scanner, run twice with one sample interval between, because CPU and GPU figures are deltas and a process's first scan only sets its baseline. `rows` is how many processes the scanner read (processes you own; other users' processes are skipped). `rows with a cpu percentage` counts those with nonzero CPU over the interval, and `gpu table rows` those with a per-process GPU figure.

**`sessions`**: for each agent source (`claude`, `codex`), how many sessions it discovered and how many are bound to a pid, then the total. A session that is discovered but not bound shows no CPU, GPU or memory.

**`pricing`**: the pricing table's upstream URL, the SHA-256 of the upstream JSON it was built from, how many models it prices, its age, and a status. `status: ok` means the table is non-empty and less than 30 days old; otherwise it reads `status: degraded (...)` with the reason, for example `pricing table is 42 days old; refresh is failing`. The dashboard uses the same check. See [Pricing table](/docs/pricing/).

**`theme`**: the theme that resolved from `$WATTOP_THEME`, `config.toml` or the default.

## `--ioreport-groups`

Adds an `ioreport-groups:` section after `channels`: every IOReport channel group this machine publishes, each with its channel count, then a total. This is Apple's own grouping read straight off the machine, which is what makes a renamed or moved counter findable after a macOS upgrade.

The last line says how the list was produced:

- `listing: wildcard channel copy (every group this machine publishes)`, or
- `listing: named-group probe (IOReport's wildcard channel copy returned nothing)`. On this fallback the set of group names is fixed, so a group Apple renamed would be missing rather than listed under its new name.
