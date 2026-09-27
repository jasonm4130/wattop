---
title: Pricing table
description: Where wattop's cost figures come from, how the table refreshes, and what happens offline.
---

Costs are computed from an embedded, filtered snapshot of [LiteLLM's pricing table](https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json) (`internal/pricing/table.json.gz`), refreshed in the background at startup and cached, so pricing works instantly and offline on first run. Each request is priced at its own model and its own long-context tier, so a `/model` switch or one very long prompt never reprices the rest of a session.

Every figure is an API-price estimate. It **ignores subscription plans** (Claude Pro/Max, ChatGPT/Codex) entirely, so a session running inside a plan's included usage still shows a nonzero cost. See [honest labelling](/docs/honest-labelling/).

## The embedded table

The binary carries a filtered snapshot of LiteLLM's `model_prices_and_context_window.json`. wattop reads it at startup, so no network call is on the startup path. It is filtered to the models seen in use (`claude-opus-5`, `claude-fable-5-1`, `claude-sonnet-5`, `claude-haiku-4-5`, `gpt-5.6-terra`, `gpt-5-codex`) plus their current and recent sibling releases, so a version bump on either agent still resolves without a refresh.

The current embedded snapshot:

| Field | Value |
|---|---|
| Upstream commit | `56a61cf016a4fd44de520f5fcd54d6263f915699` |
| Fetched | `2026-09-06T01:29:41Z` |
| SHA-256 of the upstream JSON | `f68d88c12610ea31ab355a1293fde55aeed6fa78a1f4b182c67be47d80b1d202` |
| Upstream entries at fetch time | 3818 |
| Keys kept | 66 |

## Background refresh

At startup, wattop checks its cache file:

```text
$XDG_CACHE_HOME/wattop/pricing.json
```

When `XDG_CACHE_HOME` is unset, that is `~/.cache/wattop/pricing.json`.

- If the cache is less than 24 hours old, wattop uses it and makes no request.
- Otherwise it makes one `GET` of the full upstream table from raw.githubusercontent.com in the background (10-second timeout), writes the result to the cache, and switches to it. The full table resolves strictly more models than the embedded snapshot, never fewer.
- If the request fails, the failure is not fatal and the table embedded in the binary stays in use.

So there is at most one pricing request per 24 hours, shared across every wattop you run. `--demo` never refreshes; it uses the embedded table only.

[`wattop doctor`](/docs/doctor/) prints the table in use: its source URL, SHA-256, entry count, age and status. If the table is more than 30 days old, because refreshes keep failing, the pricing source is reported as degraded in doctor and on the dashboard.

## Unknown models

A model name is matched against the table exactly first, then with a bracketed suffix stripped (`claude-opus-5[1m]` to `claude-opus-5`), then with a trailing `-YYYYMMDD` date stripped, then in its `anthropic.`/`openai.`-prefixed forms, then by longest prefix, so it resolves to the closest sibling model. If nothing matches, the cost renders `$—`, never `$0.00`, and the model is listed in the footer's unpriced-model list (`unpriced_models` in `--json`). A `~` before a cost means it leaves out usage on an unpriced model.

## Verifying the checksum

```sh
U=https://raw.githubusercontent.com/BerriAI/litellm
U=$U/main/model_prices_and_context_window.json
curl -fsSL "$U" | shasum -a 256
```

Compare the first field of the output against the SHA-256 above. A mismatch alone is not a problem: it just means upstream has moved since the embedded snapshot was generated.

## Updating the embedded table

In a source checkout, `make pricing` pulls the latest upstream table and regenerates `internal/pricing/table.json.gz` and [`docs/pricing-update.md`](https://github.com/jasonm4130/wattop/blob/main/docs/pricing-update.md) with the commit and checksum that produced it.

To add a model upstream has not published yet, add its bare key (matching LiteLLM's key exactly, e.g. `claude-opus-6`) to the `KEYS` heredoc in `scripts/pricing.sh`, then re-run `make pricing`. If upstream genuinely has no entry yet, `make pricing` logs it as missing and drops it; until upstream publishes a real entry, the model resolves through the fallbacks above or renders `$—`.
