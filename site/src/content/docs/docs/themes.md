---
title: Themes
description: wattop's four themes and bare-hex accents.
---

wattop ships four named themes, embedded in the binary:

- `wattop-dark` (default)
- `wattop-light`
- `nord`
- `catppuccin-mocha`

Every widget reads a semantic role (`idle`, `busy`, `waiting`, `warn`, `hot`, …), never a raw palette colour, so a theme swap re-colours the whole dashboard consistently.

## Choosing a theme

```sh
wattop --theme nord
WATTOP_THEME=catppuccin-mocha wattop
```

Theme names are case-insensitive. The first of these that is set wins:

1. `--theme`
2. `$WATTOP_THEME`
3. `theme` in [`config.toml`](/docs/config/)
4. `wattop-dark`

An unknown name is never fatal: wattop prints a one-line warning to stderr and falls back to `wattop-dark`.

## Cycling in the app

Press <kbd>t</kbd> to cycle forward and <kbd>T</kbd> to cycle back through the four themes, in alphabetical order. The footer shows the active one as `theme:<name>`. The choice lasts for the current run only; it is not written back to `config.toml`.

## Hex accent

```sh
wattop --theme 58a6ff
wattop --theme '#58a6ff'
```

`--theme <hex>` layers a bare accent colour onto `wattop-dark` instead of naming a theme. It takes six hex digits, with or without a leading `#` (quote the `#` form in your shell). The same value works in `WATTOP_THEME` and in `config.toml`'s `theme` key.

## No colour

```sh
wattop --no-color
NO_COLOR=1 wattop
```

`--no-color`, or `NO_COLOR` set to any non-empty value, disables all ANSI styling. Gauges render as plain blocks.

## This site

This site uses the `wattop-dark` and `wattop-light` palettes; the toggle in the header switches between them.
