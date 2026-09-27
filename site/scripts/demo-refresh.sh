#!/bin/sh
# Rebuild wattop, record 25 one-second `--demo --json` frames and trim them
# into src/data/demo.json. Run from site/: `pnpm demo:refresh`.
set -eu
make -C .. build
tmp=$(mktemp -t wattop-demo.XXXXXX)
trap 'rm -f "$tmp"' EXIT
# --json streams one snapshot per interval; take 25 and stop.
../bin/wattop --demo --json --interval 1s | head -n 25 > "$tmp" || true
WATTOP_VERSION=$(../bin/wattop --version | awk '{print $2}' | sed 's/-[0-9]*-g.*//') node scripts/trim-demo.mjs "$tmp"
