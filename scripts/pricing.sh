#!/usr/bin/env bash
# Refreshes internal/pricing/table.json.gz from LiteLLM's upstream pricing
# table and regenerates docs/pricing-update.md with the commit/checksum that
# produced it. Run via `make pricing`.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

SOURCE_URL="https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
OUT_GZ="internal/pricing/table.json.gz"
OUT_DOC="docs/pricing-update.md"

if ! command -v jq >/dev/null 2>&1; then
	echo "pricing.sh: jq not found on PATH" >&2
	exit 1
fi

RAW="$(mktemp -d)"
trap 'rm -rf "$RAW"' EXIT
KEYS_FILE="$RAW/keys.txt"

# The ~60 keys that matter: the models actually seen on this machine
# (claude-opus-5, claude-fable-5-1, claude-sonnet-5, claude-haiku-4-5,
# gpt-5.6-terra, gpt-5-codex) plus their current and recent sibling
# releases, so a version bump on either agent still resolves without a
# refresh. Kept here (not a separate data file) so this script is the one
# place that owns the curated set.
cat >"$KEYS_FILE" <<'KEYS'
claude-opus-5
claude-sonnet-5
claude-haiku-4-5
claude-haiku-4-5-20251001
claude-fable-5-1
claude-fable-5
claude-mythos-5
claude-mythos-5-1
claude-mythos-preview
claude-opus-4-8
claude-opus-4-7
claude-opus-4-7-20260416
claude-opus-4-6
claude-opus-4-6-20260205
claude-opus-4-5
claude-opus-4-5-20251101
claude-opus-4-1
claude-opus-4-1-20250805
claude-opus-4-20250514
claude-sonnet-4-6
claude-sonnet-4-5
claude-sonnet-4-5-20250929
claude-sonnet-4-20250514
claude-3-7-sonnet-20250219
claude-3-opus-20240229
claude-3-haiku-20240307
gpt-6-astra
gpt-5.6-terra
gpt-5.6-sol
gpt-5.6-luna
gpt-5.6-cyber
gpt-5.6
gpt-5.5-pro
gpt-5.5
gpt-5.4-pro
gpt-5.4-mini
gpt-5.4-nano
gpt-5.4
gpt-5.3-codex
gpt-5.2-codex
gpt-5.2-pro
gpt-5.2
gpt-5.1-codex-max
gpt-5.1-codex
gpt-5.1-codex-mini
gpt-5.1
gpt-5-codex
gpt-5-pro
gpt-5-mini
gpt-5-nano
gpt-5
gpt-4.1
gpt-4.1-mini
gpt-4.1-nano
gpt-4o
gpt-4o-mini
gpt-4o-2024-08-06
gpt-4-turbo
gpt-4
gpt-3.5-turbo
o1
o1-pro
o3
o3-mini
o3-pro
o4-mini
KEYS

echo "pricing.sh: fetching $SOURCE_URL..." >&2
curl -fsSL "$SOURCE_URL" -o "$RAW/upstream.json"

SHA256="$(shasum -a 256 "$RAW/upstream.json" | cut -d' ' -f1)"
COMMIT="$(curl -fsSL "https://api.github.com/repos/BerriAI/litellm/commits?path=model_prices_and_context_window.json&per_page=1" \
	| jq -r '.[0].sha')"
GENERATED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
TOTAL_ENTRIES="$(jq 'length' "$RAW/upstream.json")"
FILTERED_COUNT="$(wc -l <"$KEYS_FILE" | tr -d ' ')"

echo "pricing.sh: filtering to $FILTERED_COUNT curated keys ($KEYS_FILE)..." >&2
jq -n \
	--slurpfile upstream "$RAW/upstream.json" \
	--rawfile keysraw "$KEYS_FILE" \
	--arg source_url "$SOURCE_URL" \
	--arg source_sha256 "$SHA256" \
	--arg generated_at "$GENERATED_AT" \
	'
	($keysraw | rtrimstr("\n") | split("\n")) as $keys
	| $upstream[0] as $table
	| {
		source_url: $source_url,
		source_sha256: $source_sha256,
		generated_at: $generated_at,
		models: (reduce $keys[] as $k ({}; if $table[$k] then . + {($k): $table[$k]} else . end))
	}
	' \
	--sort-keys \
	>"$RAW/table.json"

MISSING="$(jq -n --slurpfile t "$RAW/table.json" --rawfile keysraw "$KEYS_FILE" \
	'($keysraw | rtrimstr("\n") | split("\n")) as $keys | [$keys[] | select(($t[0].models[.]) == null)]' )"
if [ "$(echo "$MISSING" | jq 'length')" -gt 0 ]; then
	echo "pricing.sh: warning: keys missing upstream: $MISSING" >&2
fi

gzip -n -9 -c "$RAW/table.json" >"$OUT_GZ"
echo "pricing.sh: wrote $OUT_GZ ($(jq '.models | length' "$RAW/table.json") models)" >&2

cat >"$OUT_DOC" <<EOF
# Pricing table update

\`internal/pricing/table.json.gz\` is a filtered, embedded snapshot of
LiteLLM's \`model_prices_and_context_window.json\`. It is what \`pricing.Load()\`
reads at startup, so wattop prices sessions correctly offline and instantly on
first run — no network call is on the startup path.

## Source

- URL: \`$SOURCE_URL\`
- Upstream commit (path \`model_prices_and_context_window.json\`): \`$COMMIT\`
- Fetched: \`$GENERATED_AT\`
- SHA-256 of the fetched upstream JSON: \`sha256:$SHA256\`
- Upstream entry count at fetch time: $TOTAL_ENTRIES
- Filtered to $FILTERED_COUNT keys, curated in the \`KEYS\` heredoc inside
  \`scripts/pricing.sh\` — the models actually seen on this machine
  (\`claude-opus-5\`, \`claude-fable-5-1\`, \`claude-sonnet-5\`,
  \`claude-haiku-4-5\`, \`gpt-5.6-terra\`, \`gpt-5-codex\`) plus their current and
  recent sibling releases, so a version bump on either agent still resolves
  without a refresh.

## Refreshing

\`\`\`
make pricing
\`\`\`

Regenerates \`table.json.gz\` and this file from the live upstream table, then
run \`go test ./internal/pricing/...\` — it still passes because the tests are
pinned to \`internal/pricing/testdata/frozen-table.json\`, a fixed fixture that
does not move when upstream does.

## Verifying the checksum

\`\`\`
curl -fsSL '$SOURCE_URL' | shasum -a 256
\`\`\`

Compare the first field of the output against the \`sha256:\` line above. A
mismatch alone is not a problem — it just means upstream has moved since this
file was last regenerated; run \`make pricing\` to catch up.

## Adding a model upstream has not yet published

Add its bare key (matching LiteLLM's key exactly, e.g. \`claude-opus-6\`) to
the \`KEYS\` heredoc in \`scripts/pricing.sh\`, then re-run \`make pricing\`. If
upstream genuinely has no entry yet, \`make pricing\` logs it as missing and
drops it silently — until upstream publishes a real entry, the model
resolves via \`(*pricing.Book).Resolve\`'s date-strip / prefix / longest-prefix
fallbacks to whichever sibling model is the closest prefix match, or renders
\`\$—\` if none matches. An unresolved model never renders \`\$0.00\`: it renders
\`\$—\` and is counted in the footer's unpriced-model list.
EOF

echo "pricing.sh: wrote $OUT_DOC" >&2
