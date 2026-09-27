---
title: Config file
description: wattop's optional config.toml and the defaults it sets.
---

wattop reads one optional file:

```text
$XDG_CONFIG_HOME/wattop/config.toml
```

When `XDG_CONFIG_HOME` is unset, that is `~/.config/wattop/config.toml`. The format is [TOML](https://toml.io/). wattop never creates or writes this file.

A missing file is not an error: every field just keeps its default. A file that exists but cannot be read or parsed prints a one-line `wattop: config: ...` message to stderr, and wattop carries on with the defaults. `--demo` ignores the file entirely, so a demo looks the same on any machine; `wattop doctor` does read it.

## Keys

Every key is optional. A zero value means "not set", so the default applies.

| Key | Type | Default | Meaning |
|---|---|---|---|
| `theme` | string | `wattop-dark` | Default theme: one of the four [theme names](/docs/themes/) (case-insensitive), or a bare hex accent such as `58a6ff`. |
| `interval_ms` | integer | `1000` | SoC sample interval in milliseconds; the config spelling of `--interval`. Clamped to 500-5000 with a warning. |
| `burn_hot_usd_per_hr` | float | `5.0` | The `$/hr` at or above which the footer's machine total and a session's detail view render as hot. |
| `codex_stale_minutes` | integer | `5` | Rollout-file age, in minutes, past which a Codex session with no other signal is considered stale. |
| `context_window_overrides` | table of string to integer | none | Maps a Claude session id or a cwd to a context window size in tokens, overriding the context-fill estimate for that session. |

`context_window_overrides` replaces the estimate's denominator. Without an override, Claude's context window is estimated as the smallest of 200,000 or 1,000,000 tokens that is at least the session's largest prompt so far. A session id match wins over a cwd match. Codex sessions do not need it: their context window is reported exactly.

## Example

```toml
theme = "nord"
interval_ms = 2000
burn_hot_usd_per_hr = 10.0
codex_stale_minutes = 15

[context_window_overrides]
"/Users/me/code/api-server" = 1000000
```

## Precedence

Flags and environment variables override the file. Highest first:

| Setting | Order |
|---|---|
| Theme | `--theme`, then `$WATTOP_THEME`, then `theme`, then `wattop-dark` |
| Interval | `--interval`, then `interval_ms`, then 1s |
| Colour | `--no-color` or a non-empty `NO_COLOR` disables styling; there is no config key |

`burn_hot_usd_per_hr`, `codex_stale_minutes` and `context_window_overrides` have no flag or environment variable; the file is the only way to set them.

There is no config key for the Codex discovery lookback: it is fixed at 2 h. See [limitations](/docs/limitations/#codex-status-and-pid-binding-are-inferred).
