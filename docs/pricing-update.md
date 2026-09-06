# Pricing table update

`internal/pricing/table.json.gz` is a filtered, embedded snapshot of
LiteLLM's `model_prices_and_context_window.json`. It is what `pricing.Load()`
reads at startup, so wattop prices sessions correctly offline and instantly on
first run — no network call is on the startup path.

## Source

- URL: `https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json`
- Upstream commit (path `model_prices_and_context_window.json`): `56a61cf016a4fd44de520f5fcd54d6263f915699`
- Fetched: `2026-09-06T01:29:41Z`
- SHA-256 of the fetched upstream JSON: `sha256:f68d88c12610ea31ab355a1293fde55aeed6fa78a1f4b182c67be47d80b1d202`
- Upstream entry count at fetch time: 3818
- Filtered to 66 keys, curated in the `KEYS` heredoc inside
  `scripts/pricing.sh` — the models actually seen on this machine
  (`claude-opus-5`, `claude-fable-5-1`, `claude-sonnet-5`,
  `claude-haiku-4-5`, `gpt-5.6-terra`, `gpt-5-codex`) plus their current and
  recent sibling releases, so a version bump on either agent still resolves
  without a refresh.

## Refreshing

```
make pricing
```

Regenerates `table.json.gz` and this file from the live upstream table, then
run `go test ./internal/pricing/...` — it still passes because the tests are
pinned to `internal/pricing/testdata/frozen-table.json`, a fixed fixture that
does not move when upstream does.

## Verifying the checksum

```
curl -fsSL 'https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json' | shasum -a 256
```

Compare the first field of the output against the `sha256:` line above. A
mismatch alone is not a problem — it just means upstream has moved since this
file was last regenerated; run `make pricing` to catch up.

## Adding a model upstream has not yet published

Add its bare key (matching LiteLLM's key exactly, e.g. `claude-opus-6`) to
the `KEYS` heredoc in `scripts/pricing.sh`, then re-run `make pricing`. If
upstream genuinely has no entry yet, `make pricing` logs it as missing and
drops it silently — until upstream publishes a real entry, the model
resolves via `(*pricing.Book).Resolve`'s date-strip / prefix / longest-prefix
fallbacks to whichever sibling model is the closest prefix match, or renders
`$—` if none matches. An unresolved model never renders `$0.00`: it renders
`$—` and is counted in the footer's unpriced-model list.
