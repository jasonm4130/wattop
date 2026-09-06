package pricing

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/jasonm4130/wattop/internal/domain"
)

func readTestdata(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join("testdata", name))
}

func zeroUsage() domain.Usage {
	return domain.Usage{}
}

func almostEqual(a, b, eps float64) bool {
	return math.Abs(a-b) <= eps
}

// TestCache1hTierFires prices a claude-opus-5 block with ephemeral_1h
// nonzero and ephemeral_5m zero, asserting the 1h cache-write rate fires
// and the total is exact to the cent:
//
//	1,000,000 input   × $5/M  = $5.00
//	  100,000 output  × $25/M = $2.50
//	1,000,000 cache_read × $0.50/M = $0.50
//	1,000,000 cache_create_1h × $10/M = $10.00
//	                              total = $18.00
func TestCache1hTierFires(t *testing.T) {
	b := frozenBook(t)

	u := domain.Usage{
		Input:         1_000_000,
		Output:        100_000,
		Thinking:      20_000, // already inside Output; must not add separately
		CacheRead:     1_000_000,
		CacheCreate1h: 1_000_000,
	}

	usd, priced := b.Cost("claude-opus-5", u, 0)
	if !priced {
		t.Fatalf("Cost() priced = false, want true")
	}
	want := 18.00
	if !almostEqual(usd, want, 0.005) {
		t.Fatalf("Cost() = %.6f, want %.2f exact to the cent", usd, want)
	}
}

// TestCache1hVsCache5mDifference prices an isolated cache-write-only block
// (every other field zero, so the comparison isolates the cache-write line
// rather than a mixed total) once at the 1h rate and once at the 5m rate,
// asserting the difference is 37.5% of the 1h line -- the corrected figure
// from the Decision section, not the 60% a naive per-line ratio against the
// 5m figure would produce.
func TestCache1hVsCache5mDifference(t *testing.T) {
	b := frozenBook(t)

	const tokens = 1_000_000

	cost1h, priced := b.Cost("claude-opus-5", domain.Usage{CacheCreate1h: tokens}, 0)
	if !priced {
		t.Fatalf("Cost() priced = false for the 1h block")
	}
	cost5m, priced := b.Cost("claude-opus-5", domain.Usage{CacheCreate5m: tokens}, 0)
	if !priced {
		t.Fatalf("Cost() priced = false for the 5m block")
	}

	if cost5m >= cost1h {
		t.Fatalf("cost5m (%.6f) >= cost1h (%.6f), want the 1h rate to be the pricier one", cost5m, cost1h)
	}

	ratio := (cost1h - cost5m) / cost1h
	if !almostEqual(ratio, 0.375, 1e-9) {
		t.Fatalf("(cost1h-cost5m)/cost1h = %.6f, want 0.375 (37.5%%)", ratio)
	}
}

// TestCodexCachedInputIsSubtracted prices a real Codex record (total 230377
// = input 227924 + output 2453, cached 210432 not added in) and asserts
// cached_input_tokens is subtracted from the billable input rather than
// double-counted on top of it -- getting this backwards roughly doubles the
// figure.
func TestCodexCachedInputIsSubtracted(t *testing.T) {
	b := frozenBook(t)

	u := domain.Usage{
		Input:       227_924,
		Output:      2_453,
		CachedInput: 210_432,
	}

	usd, priced := b.Cost("gpt-5-codex", u, 0)
	if !priced {
		t.Fatalf("Cost() priced = false, want true")
	}

	const (
		inputRate     = 1.25e-06
		outputRate    = 1e-05
		cacheReadRate = 1.25e-07
	)
	billableInput := float64(u.Input - u.CachedInput)
	want := billableInput*inputRate + float64(u.Output)*outputRate + float64(u.CachedInput)*cacheReadRate

	if !almostEqual(usd, want, 1e-9) {
		t.Fatalf("Cost() = %.9f, want %.9f (billable_input = input - cached_input)", usd, want)
	}

	// Getting the subtraction backwards (pricing the full, un-subtracted
	// input) would come out close to double this figure -- assert we are
	// nowhere near that to catch the regression the spec calls out.
	doubledIfWrong := float64(u.Input)*inputRate + float64(u.Output)*outputRate + float64(u.CachedInput)*cacheReadRate
	if usd >= doubledIfWrong*0.9 {
		t.Fatalf("Cost() = %.9f is too close to the un-subtracted figure %.9f", usd, doubledIfWrong)
	}
}

// TestTierSelectedFromPresentKeys asserts tier selection is driven by which
// *_above_<N>_tokens keys an entry carries, never a hardcoded tier name:
// gpt-5.6-terra genuinely carries *_above_272k_tokens rates, so a request
// above that threshold prices at double the base input rate; claude-opus-5
// carries no *_above_200k_tokens key at all, so an implementer told "apply
// the 200k Anthropic tier" would build logic for a field that does not
// exist -- the rate must stay flat regardless of prompt size.
func TestTierSelectedFromPresentKeys(t *testing.T) {
	b := frozenBook(t)

	t.Run("gpt-5.6-terra above 272k selects the higher tier", func(t *testing.T) {
		below, priced := b.Cost("gpt-5.6-terra", domain.Usage{Input: 1_000_000}, 100_000)
		if !priced {
			t.Fatalf("Cost() priced = false")
		}
		above, priced := b.Cost("gpt-5.6-terra", domain.Usage{Input: 1_000_000}, 300_000)
		if !priced {
			t.Fatalf("Cost() priced = false")
		}
		if !almostEqual(above, below*2, 1e-9) {
			t.Fatalf("above-threshold cost = %.6f, below = %.6f, want exactly double (272k tier is 2x base)", above, below)
		}
	})

	t.Run("claude-opus-5 has no 200k tier so the rate never moves", func(t *testing.T) {
		low, priced := b.Cost("claude-opus-5", domain.Usage{Input: 1_000_000}, 0)
		if !priced {
			t.Fatalf("Cost() priced = false")
		}
		high, priced := b.Cost("claude-opus-5", domain.Usage{Input: 1_000_000}, 500_000)
		if !priced {
			t.Fatalf("Cost() priced = false")
		}
		if low != high {
			t.Fatalf("cost at promptTokens=0 (%.6f) != cost at promptTokens=500000 (%.6f); claude-opus-5 has no *_above_200k_tokens key so the rate must not change", low, high)
		}
	})
}
