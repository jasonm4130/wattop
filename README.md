# wattop

A terminal dashboard that puts Apple Silicon SoC telemetry and live AI
coding-agent sessions on one refresh clock, joined by pid.

## Scope

macOS, Apple Silicon (arm64) only. Built with CGO against IOReport and SMC,
so it does not build or run on Linux or Intel Macs.

## Build

```
make build
```

Produces `bin/wattop`. Requires Go 1.27 and Xcode command line tools (for
CGO).

## Attribution

_Filled in as part of release (Task 14)._

## Limitations

_Filled in as part of release (Task 14)._
