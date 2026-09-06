package pricing

import "testing"

func frozenBook(t *testing.T) *Book {
	t.Helper()
	data, err := readTestdata("frozen-table.json")
	if err != nil {
		t.Fatalf("read frozen-table.json: %v", err)
	}
	snap, err := parseSnapshotJSON(data)
	if err != nil {
		t.Fatalf("parse frozen-table.json: %v", err)
	}
	return &Book{snap: snap}
}

func bookFrom(models modelTable) *Book {
	return &Book{snap: snapshot{Models: models}}
}

// TestLoadEmbeddedTable exercises Load() against the real, gzip-embedded
// table.json.gz -- distinct from every other test in this package, which
// pins testdata/frozen-table.json so an upstream `make pricing` run cannot
// turn CI red. This test asserts only shape (entry count, provenance
// present), never a specific price, so it survives regeneration.
func TestLoadEmbeddedTable(t *testing.T) {
	b, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if n := b.Entries(); n < 60 {
		t.Fatalf("Entries() = %d, want >= 60", n)
	}
	url, sha, fetchedAt := b.Source()
	if url == "" || sha == "" || fetchedAt.IsZero() {
		t.Fatalf("Source() = (%q, %q, %v), want all set", url, sha, fetchedAt)
	}
	if url != SourceURL {
		t.Fatalf("Source() url = %q, want %q", url, SourceURL)
	}
}

func TestBracketedSuffixResolves(t *testing.T) {
	b := frozenBook(t)

	r, ok := b.Resolve("claude-opus-5[1m]")
	if !ok {
		t.Fatalf("Resolve(claude-opus-5[1m]) ok = false, want true")
	}
	if r.model != "claude-opus-5" {
		t.Fatalf("resolved model = %q, want claude-opus-5", r.model)
	}
}

func TestUnknownModelIsUnpricedNotZero(t *testing.T) {
	b := frozenBook(t)

	_, ok := b.Resolve("not-a-real-model-xyz")
	if ok {
		t.Fatalf("Resolve(not-a-real-model-xyz) ok = true, want false")
	}

	usd, priced := b.Cost("not-a-real-model-xyz", zeroUsage(), 0)
	if priced {
		t.Fatalf("Cost() priced = true, want false for an unknown model")
	}
	if usd != 0 {
		t.Fatalf("Cost() usd = %v for an unknown model; callers must render $— from priced=false, never trust this 0", usd)
	}
}

func TestResolveExactKey(t *testing.T) {
	b := frozenBook(t)
	r, ok := b.Resolve("gpt-5-codex")
	if !ok || r.model != "gpt-5-codex" {
		t.Fatalf("Resolve(gpt-5-codex) = (%+v, %v), want exact match", r, ok)
	}
}

func TestResolveStripsTrailingDate(t *testing.T) {
	b := bookFrom(modelTable{
		"claude-haiku-4-5": {"input_cost_per_token": 1e-06},
	})
	r, ok := b.Resolve("claude-haiku-4-5-20251001")
	if !ok || r.model != "claude-haiku-4-5" {
		t.Fatalf("Resolve(claude-haiku-4-5-20251001) = (%+v, %v), want claude-haiku-4-5", r, ok)
	}
}

func TestResolveProviderPrefixedForm(t *testing.T) {
	b := bookFrom(modelTable{
		"anthropic.claude-3-opus": {"input_cost_per_token": 1.5e-05},
	})
	r, ok := b.Resolve("claude-3-opus")
	if !ok || r.model != "anthropic.claude-3-opus" {
		t.Fatalf("Resolve(claude-3-opus) = (%+v, %v), want anthropic.-prefixed match", r, ok)
	}
}

func TestResolveLongestPrefixMatch(t *testing.T) {
	b := bookFrom(modelTable{
		"gpt-5.6":       {"input_cost_per_token": 1e-06},
		"gpt-5.6-terra": {"input_cost_per_token": 2e-06},
	})
	r, ok := b.Resolve("gpt-5.6-terra-mini")
	if !ok || r.model != "gpt-5.6-terra" {
		t.Fatalf("Resolve(gpt-5.6-terra-mini) = (%+v, %v), want longest-prefix match gpt-5.6-terra", r, ok)
	}
}
