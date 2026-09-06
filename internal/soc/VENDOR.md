# Vendored: mactop collector layer

Source: https://github.com/metaspartan/mactop (module `github.com/metaspartan/mactop/v2`), MIT, Copyright (c) 2024-2026 Carsen Klock.

Pinned commit: 220df7efeba860f50939b027afc03ee330f209bf (2026-08-xx, "Brew formula update for mactop version v2.1.5").

`make vendor-diff` (`scripts/vendor-diff.sh`) re-fetches this exact SHA into a temp dir and diffs the 16 verbatim files against it, so an upstream fix or a silent behavior change arrives as a reviewable delta rather than a silent fork.

## Why a copy, not an import

Every mactop collector lives under `internal/app` of its module — Go's internal-package rule makes that unreachable from outside `github.com/metaspartan/mactop`. Copying into `internal/soc/mactop` (a normally named subpackage, deliberately not a directory named `vendor`, which `./...` and `go list` would skip) is the only route.

## The 16 verbatim copies

Package clause changed from `app` to `mactop` in every file; no other change except where named below. `vendor-diff.sh` diffs exactly these 16 files byte-for-byte against upstream (after normalizing the package clause).

Seven of the sixteen (`arch_check.go`, `detection.go`, `types.go`, `thunderbolt.go`, `thunderbolt_network.go`, `rdma.go`, `profiler.go`) plus the authored `metrics_subset.go` do **not** themselves `import "C"`, so they needed an explicit `//go:build darwin && arm64 && cgo` line added (see "CGO_ENABLED=0 gate" below) — the other nine of the sixteen (`ioreport.go`, `ioreport.m`, `smc.c`, `smc.h`, `native_stats.go`, `sys_info.go`, `battery.go`, `displayfps.go`, `displayfps.m`) plus the authored `cpu_usage_subset.go` already `import "C"` (or are ignored outright when cgo is off, for the `.c`/`.m`/`.h` files) and needed no tag.

| File | Modified? |
|---|---|
| `ioreport.go` | no |
| `ioreport.m` | no (136 KB — see "Standing maintenance liability" below) |
| `smc.c` | no |
| `smc.h` | no |
| `native_stats.go` | no |
| `sys_info.go` | **6 `i18n.T("...")` calls inlined to their upstream English string literals** (see below); otherwise no |
| `detection.go` | build tag added (see above); otherwise no |
| `types.go` | build tag added; **`CPUCoreWidget` struct + its 5 methods (`NewCPUCoreWidget`, `UpdateUsage`, `calculateLayout`, `drawCore`, `Draw`) deleted**, along with the now-unused `github.com/metaspartan/gotui/v5` and `image` imports — this is the one gotui-coupled type in the vendored set; `FormatCoreSummary` (no gotui dependency) kept |
| `arch_check.go` | build tag added; otherwise no |
| `battery.go` | **4 `i18n.T("...")` calls inlined**; otherwise no |
| `thunderbolt.go` | build tag added; otherwise no |
| `thunderbolt_network.go` | build tag added; otherwise no |
| `rdma.go` | build tag added; otherwise no |
| `profiler.go` | build tag added; otherwise no |
| `displayfps.go` | no |
| `displayfps.m` | no |

### i18n inlining

Upstream's `internal/i18n` package is not vendored (it is a full localization system with 19 locale files — far outside this task's scope, and mactop's own English strings are all this collector layer needs). The 10 call sites (6 in `sys_info.go`, 4 in `battery.go`) are replaced with the literal English string from `internal/i18n/locales/active.en.toml` at the pinned commit:

- `Info_BatteryCharging` → `"charging"`, `Info_BatteryAC` → `"AC"`, `Info_BatteryDischarging` → `"on battery"`, `Info_Battery` → `"Battery"` (battery.go)
- `Metrics_ThermalNominal` → `"Nominal"`, `Metrics_ThermalModerate` → `"Moderate"`, `Metrics_ThermalHeavy` → `"Heavy"`, `Metrics_ThermalTrapping` → `"Trapping"`, `Metrics_ThermalSleeping` → `"Sleeping"`, `Metrics_ThermalUnknown` → `"Unknown"` (sys_info.go)

The now-unused `"github.com/metaspartan/mactop/v2/internal/i18n"` import was removed from both files.

### CGO_ENABLED=0 gate — an added build tag not anticipated by the plan text

The plan's Spec states the `.c`/`.m`/`.h` files "are simply ignored when CGO is off and need no filename constraint" — verified true (a scratch probe: a bare `.c` file alongside a `package mactop` `.go` file builds clean under `CGO_ENABLED=0`). It does **not** hold for the `.go` files that don't themselves `import "C"`: Go's build system only excludes a file from a `CGO_ENABLED=0` build when that file imports `"C"` directly, so a pure-Go file in this package (e.g. `types.go`, which references `FanInfo`/`TempSensor` — types declared in the cgo file `ioreport.go`) is **not** automatically excluded and fails to compile once cgo-only symbols it references disappear.

Fix: every copied `.go` file that does not itself `import "C"` (`arch_check.go`, `detection.go`, `profiler.go`, `rdma.go`, `thunderbolt.go`, `thunderbolt_network.go`, `types.go`, plus the authored `metrics_subset.go`) got an explicit `//go:build darwin && arm64 && cgo` line added as its first line. This is the mechanism `doc.go`'s inverse constraint depends on to actually gate the whole directory — without it, `CGO_ENABLED=0 go build ./internal/soc/mactop/` fails on undefined symbols rather than falling through to `doc.go`. Verified: `CGO_ENABLED=0 go build ./internal/soc/...` exits 0 with this fix, fails without it.

### `types.go` — the one gotui-coupled type

`CPUCoreWidget` (and its constructor and 4 draw/layout methods) is the terminal-rendering widget for the classic per-core bar view; it directly imports `github.com/metaspartan/gotui/v5` for `*ui.Block`, `ui.Buffer`, `ui.Color`, `ui.Style`. It has no collector role — deleting it is what makes `types.go` gotui-free while keeping the plain data structs (`CPUMetrics`, `SystemInfo`, `GPUMetrics`, `MemoryMetrics`, `NetDiskMetrics`, `ProcessMetrics`, `EventThrottler`) and `FormatCoreSummary`.

## `metrics_subset.go` — partial extraction, not a verbatim copy

Not one of the 16 files above and **not** treated as file-for-file by `vendor-diff.sh` — it is a hand-assembled subset of upstream `metrics.go`, which itself is not vendored wholesale (it has Prometheus-exporter and dispatch-goroutine code with no collector role). Function bodies are copied verbatim from the pinned commit; only the file's own header comment and import list are authored.

Copied, unmodified function bodies:
- The nine the plan names: `normalizeSocMetricsPower`, `cpuMetricsFromSoc`, `gpuMetricsFromSoc`, `aneUtilizationPercent`, `calculateCoreAveragesForSystem`, `coreTypeForIndex`, `getMemoryMetrics`, `getNetDiskMetrics`, `GetCPUPercentages`.
- Two small pure helpers the plan's list omitted but the above nine call internally: `averageCPUUsage` (used by `GetCPUPercentages`'s caller composition, see `adapter.go`) and `averageCoreRange` (used by `calculateCoreAveragesForSystem`). Both are pure, zero-UI, and were left out of the plan's enumeration by oversight rather than by design — they are not reachable without dragging in `metrics.go` in full.

Also required: two package-level `const`s from upstream `metrics.go` that live between the copied functions — `aneMaxPowerW` and `aneBWRefFloorGBs` — copied verbatim alongside `aneUtilizationPercent`, which is the only place they are used.

## `cpu_usage_subset.go` — a second partial extraction, from `processes.go`

Upstream `processes.go` is explicitly excluded by the plan ("mostly kill-modals and row formatting", left to Task 6). One function in it does not fit that description: `GetCPUUsage` is a pure `mach_host_self`/`host_processor_info` syscall wrapper with zero gotui coupling, and it is a hard dependency of the plan-named `GetCPUPercentages` (in `metrics_subset.go`), which cannot compile without it. Extracted verbatim (function body unchanged) into its own file, along with the two package-level vars it shares with `GetCPUPercentages` (`lastCPUTimes`, `firstRun`) and the minimal cgo preamble (`mach_host.h`, `processor_info.h`, `mach_init.h`, the `vm_deallocate` extern) it needs — a strict subset of `processes.go`'s much larger preamble (which also pulls in `sysctl.h`, `pwd.h`, `libproc.h` for the parts that are not vendored).

## `adapter.go` and `shim.go` — authored, not vendored

`internal/soc/mactop/adapter.go` (`package mactop`, `//go:build darwin && arm64 && cgo`) is the only file that can name the copied files' unexported symbols, since they are unexported *in this package*. It supplies:

- The five thin lifecycle wrappers the plan names: `Init()`, `Sample(ms int)`, `Cleanup()`, `SOCInfo()`, `ThermalState()`, over `initSocMetrics`/`sampleSocMetrics`/`cleanupSocMetrics`/`getSOCInfo`/`getSocThermalState`.
- `GPUProcessStats() ([]GPUProcEntry, error)` over mactop's `GetGPUProcessStats()` (found in `native_stats.go`, one of the 16 copies, not in the excluded `processes.go` — confirmed by grep against the pinned commit).
- Package-level state the copies reference that has no other home in this package, ported verbatim from upstream `globals.go`: the ANE-estimate atomics `maxANEBWSeenBits`, `aneBWModeLatched`, `aneResidencyLatched` (the plan names only `aneBWModeLatched`; the other two are also required by `aneUtilizationPercent`'s body and were an omission in the plan's enumeration, the same class of gap as the two helpers in `metrics_subset.go`), `stderrLogger`, and the net/disk delta state `lastNetStats`/`lastDiskStats`/`lastNetDiskTime`/`netDiskMutex`.
- **`SampleAll(ms int) Composite`** — a sixth exported call beyond the plan's named five, and the one place the nine `metrics_subset.go` functions actually get invoked (they are unexported, so nothing outside `package mactop` can call them directly). Composition order is modeled on upstream `headless.go`'s `collectHeadlessData` (not vendored — it is output-shaping for the JSON/Prometheus surface — but its call order is the correct one to replicate): `sampleSocMetrics` → `normalizeSocMetricsPower` → one `GetCPUPercentages()` reading → `calculateCoreAveragesForSystem` / `cpuMetricsFromSoc` / `gpuMetricsFromSoc` / `aneUtilizationPercent` → `getMemoryMetrics` / `getNetDiskMetrics`. Without this, `shim.go`/`sampler_darwin.go` would have no route to the nine reused functions at all — this is a spec gap the "compiling the closure" step surfaced, not a design choice made in preference to the plan.
- `CoreTypeForIndex(index int, info SystemInfo) string`, a thin export of `coreTypeForIndex`, so `sampler_darwin.go` can label clusters per the plan's instruction to iterate the reported topology rather than hardcode E/P.

`internal/soc/shim.go` (`package soc`, same build tag) re-exports the five lifecycle wrappers, `SampleAll`, `CoreTypeForIndex` and `GPUProcessStats` under `package soc` names, plus type aliases (`SocMetrics`, `SystemInfo`, `CPUMetrics`, `GPUMetrics`, `Composite`) so `sampler_darwin.go` never needs its own import of `internal/soc/mactop`. `soc.GPUProcEntry` is a duplicate of `mactop.GPUProcEntry`, not an alias — per the plan, this keeps `soc.GPUProcessStats`'s signature free of any `mactop` type so Task 6's `internal/proc` imports `internal/soc` only.

## Standing maintenance liability

`ioreport.m` is 136 KB of vendored Objective-C — by far the largest single file in this tree and the one most likely to need a re-vendor when a macOS release changes IOReport's private ABI. `make vendor-diff` is the mechanism for catching that; there is no automated alert beyond running it.

## Hardware-specific channel behavior (M5 Max)

DRAM and ANE bandwidth (`DRAMReadBW`, `DRAMWriteBW`, `ANEBWCombined` on `SocMetrics`/`CPUMetrics`) read exactly `0.0` GB/s on this chip under sustained load at every sample count tried while writing the plan — mactop's own macOS-27 counter-zeroing-detection latch requires `cpuPower==0 && dramPower==0`, and both are nonzero here, so this is not that known failure mode; the byte-counter channels simply do not resolve on this hardware. `sampler_darwin.go` treats an exact `0.0` on these three fields as "did not resolve" (nil pointer, name appended to `SysSample.Missing` and `false` in `Channels()`), per the plan's explicit statement that these two are expected nil on this hardware. `ECoreCount` is 0 and `e_cluster_active` is permanently `0.0` on this chip; the sampler skips the E cluster entirely rather than emitting a `CoreCount: 0` entry.

## Not vendored

- `internal/app/processes.go` — per the plan, kill-modals and row formatting coupled to gotui; Task 6 re-implements the ~40 useful lines with a hand-written CGO scanner. `GetCPUUsage` is the one exception (see `cpu_usage_subset.go` above).
- `internal/i18n/*` — see i18n inlining above.
- Everything else under `internal/app` (`app.go`, `globals.go` beyond the state named above, `cli.go`, `colors.go`, `config.go`, `events*.go`, `headless.go`, `layout.go`, `menubar.go`/`menubar.m`, `overlay.go`/`overlay.m`, `theme.go`, `catppuccin.go`, `utils.go`, `info.go`) — gotui rendering, CLI, config, Prometheus/headless output shaping, none of it collector logic.
