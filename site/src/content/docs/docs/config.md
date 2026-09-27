---
title: Config file
description: wattop's optional config.toml and the defaults it sets.
---

`$XDG_CONFIG_HOME/wattop/config.toml` (or `~/.config/wattop/config.toml`) sets defaults that flags and environment variables override. A missing file is not an error; every field keeps its default.

| Key | Sets |
|---|---|
| `theme` | Default theme name. |
| `interval_ms` | SoC sample interval in milliseconds (the config spelling of `--interval`). |
| `burn_hot_usd_per_hr` | The `$/hr` at or above which the footer's machine total and a session's detail view render as hot. Default $5/hr. |
| `codex_stale_minutes` | Rollout-file age past which a Codex session with no other signal is considered stale. |
| `context_window_overrides` | A Claude session id or cwd mapped to a token count, overriding the context-fill estimate for that session. |

Precedence for the theme is `--theme`, then `$WATTOP_THEME`, then `config.toml`, then `wattop-dark`. For the interval it is `--interval`, then `config.toml`, then 1s.
