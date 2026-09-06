#!/usr/bin/env bash
# Re-fetches the pinned mactop commit (see internal/soc/VENDOR.md) into a
# temp dir and diffs the 16 verbatim-copied files against it, so an upstream
# fix or behavior change arrives as a reviewable delta instead of a silent
# fork. metrics_subset.go and cpu_usage_subset.go are partial extractions,
# not file-for-file copies -- see VENDOR.md -- and are intentionally not
# diffed here.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VENDOR_DIR="$REPO_ROOT/internal/soc/mactop"
VENDOR_MD="$REPO_ROOT/internal/soc/VENDOR.md"
UPSTREAM_URL="https://github.com/metaspartan/mactop.git"

if [[ ! -f "$VENDOR_MD" ]]; then
  echo "vendor-diff: $VENDOR_MD not found" >&2
  exit 1
fi

SHA=$(grep -oE '[0-9a-f]{40}' "$VENDOR_MD" | head -1)
if [[ -z "$SHA" ]]; then
  echo "vendor-diff: no 40-char commit SHA found in $VENDOR_MD" >&2
  exit 1
fi

FILES=(
  ioreport.go ioreport.m smc.c smc.h native_stats.go sys_info.go
  detection.go types.go arch_check.go battery.go thunderbolt.go
  thunderbolt_network.go rdma.go profiler.go displayfps.go displayfps.m
)

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

git init -q "$TMP"
git -C "$TMP" remote add origin "$UPSTREAM_URL"
if ! git -C "$TMP" fetch -q --depth 1 origin "$SHA" 2>/dev/null; then
  echo "vendor-diff: could not fetch $SHA from $UPSTREAM_URL (network unavailable?)" >&2
  exit 1
fi
git -C "$TMP" checkout -q FETCH_HEAD

any_diff=0
for f in "${FILES[@]}"; do
  upstream="$TMP/internal/app/$f"
  local_copy="$VENDOR_DIR/$f"
  if [[ ! -f "$upstream" ]]; then
    echo "=== $f: MISSING upstream at $SHA (renamed/removed?) ==="
    any_diff=1
    continue
  fi
  if [[ ! -f "$local_copy" ]]; then
    echo "=== $f: MISSING local copy at $local_copy ==="
    any_diff=1
    continue
  fi
  # Normalize the one universal, documented edit (package clause) before
  # diffing so unrelated noise doesn't drown a real upstream change.
  if ! diff -u \
      <(sed 's/^package app$/package mactop/' "$upstream") \
      "$local_copy" \
      > "$TMP/$f.diff"; then
    echo "=== $f: DIFFERS from upstream @ $SHA ==="
    cat "$TMP/$f.diff"
    any_diff=1
  fi
done

if [[ "$any_diff" -eq 0 ]]; then
  echo "clean: all 16 vendored files match upstream @ $SHA"
  exit 0
fi

echo
echo "vendor-diff: differences found against upstream @ $SHA (see VENDOR.md for documented, expected edits)"
exit 0
