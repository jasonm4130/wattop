---
title: Doctor
description: wattop doctor prints what resolved on this machine, without entering the TUI.
---

```sh
wattop doctor
wattop doctor --ioreport-groups
```

`wattop doctor` is the first thing to run on a new machine or after a macOS upgrade. It prints which SoC channels resolved, how many processes the scanner enumerated and got a CPU baseline for, how many Claude/Codex sessions were discovered and pid-bound, and the pricing table's age and status, all without entering the TUI.

`--ioreport-groups` also enumerates every IOReport group with its channel count.

Hardware support varies by chip and macOS version, so a channel that shows `—` in the dashboard is usually explained here.
