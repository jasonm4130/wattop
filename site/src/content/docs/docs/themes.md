---
title: Themes
description: wattop's four themes and bare-hex accents.
---

Four named themes, selected by `--theme` / `WATTOP_THEME` or cycled in-app with <kbd>t</kbd> / <kbd>T</kbd>:

- `wattop-dark` (default)
- `wattop-light`
- `nord`
- `catppuccin-mocha`

Every widget reads a semantic role (`idle`, `busy`, `waiting`, `warn`, `hot`, …), never a raw palette colour, so a theme swap re-colours the whole dashboard consistently.

`--theme <hex>` (for example `--theme 58a6ff`) layers a bare accent colour onto `wattop-dark` instead of naming a theme.

This site uses the `wattop-dark` and `wattop-light` palettes; the toggle in the header switches between them.
