#!/bin/sh
set -eu
make -C .. build
tmp=$(mktemp -t wattop-demo.XXXXXX)
trap 'rm -f "$tmp"' EXIT
../bin/wattop --demo --json --interval 1s | head -n 25 > "$tmp" || true
WATTOP_VERSION=$(../bin/wattop --version | awk '{print $2}' | sed 's/-[0-9]*-g.*//') node scripts/trim-demo.mjs "$tmp"
