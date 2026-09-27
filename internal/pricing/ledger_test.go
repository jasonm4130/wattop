package pricing

import (
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/jasonm4130/wattop/internal/domain"
)

// regexTieredRate is the pre-cache tier selection, kept here as the
// reference the cached rate card must agree with: scan every key of the
// entry through tierKeyRe on every call.
func regexTieredRate(e modelEntry, base string, promptTokens int64) float64 {
	type tier struct {
		threshold int64
		rate      float64
	}
	var tiers []tier
	for k, v := range e {
		m := tierKeyRe.FindStringSubmatch(k)
		if m == nil || m[1] != base {
			continue
		}
		n, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			continue
		}
		rate, ok := asFloat(v)
		if !ok {
			continue
		}
		tiers = append(tiers, tier{threshold: n * 1000, rate: rate})
	}
	baseRate, _ := asFloat(e[base])
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].threshold < tiers[j].threshold })
	for i := len(tiers) - 1; i >= 0; i-- {
		if promptTokens > tiers[i].threshold {
			return tiers[i].rate
		}
	}
	return baseRate
}

// TestRateCardMatchesRegexScan: the parsed-once card selects exactly the
// rate the per-call regex scan did, for every base and around every
// threshold, including a multi-tier entry.
func TestRateCardMatchesRegexScan(t *testing.T) {
	entries := map[string]modelEntry{}
	for k, e := range frozenBook(t).snap.Models {
		entries[k] = e
	}
	entries["multi-tier"] = modelEntry{
		"input_cost_per_token":                        1e-06,
		"input_cost_per_token_above_128k_tokens":      2e-06,
		"input_cost_per_token_above_272k_tokens":      3e-06,
		"input_cost_per_token_above_272k_tokens_flex": 9e-06,
	}
	prompts := []int64{0, 1, 127_999, 128_000, 128_001, 199_999, 200_000, 200_001, 271_999, 272_000, 272_001, 1_000_000}
	for name, e := range entries {
		card := parseRateCard(e)
		for _, base := range rateBases {
			for _, p := range prompts {
				if got, want := card.rate(base, p), regexTieredRate(e, base, p); got != want {
					t.Errorf("%s %s @%d: card %v, regex scan %v", name, base, p, got, want)
				}
			}
		}
	}
}

// TestRateCardIsCachedPerModel: resolving a model twice reuses one parsed
// card, and swapping in a new table drops it so new rates apply.
func TestRateCardIsCachedPerModel(t *testing.T) {
	b := bookFrom(modelTable{"m": {"input_cost_per_token": 1e-06}})
	r1, _ := b.Resolve("m")
	r2, _ := b.Resolve("m")
	if r1.card == nil || r1.card != r2.card {
		t.Fatalf("cards %p and %p, want one cached card", r1.card, r2.card)
	}

	b.swap(snapshot{Models: modelTable{"m": {"input_cost_per_token": 3e-06}}})
	r3, _ := b.Resolve("m")
	if r3.card == r1.card {
		t.Fatalf("card survived a table swap")
	}
	if got, _ := b.Cost("m", domain.Usage{Input: 1_000_000}, 0); !almostEqual(got, 3, 1e-9) {
		t.Fatalf("Cost after swap = %v, want 3 (the new table's rate)", got)
	}
}

// TestCostLedgerPricesEachBucketAtItsOwnTier: a ledger with a short and a
// long request prices each at its own tier, and the threshold boundary is
// exact (272000 tokens is not above 272k; 272001 is).
func TestCostLedgerPricesEachBucketAtItsOwnTier(t *testing.T) {
	b := frozenBook(t)
	const model = "gpt-5.6-terra"
	short := domain.Usage{Input: 100_000, Output: 1_000}
	long := domain.Usage{Input: 300_000, Output: 1_000}
	edge := domain.Usage{Input: 272_000}
	over := domain.Usage{Input: 272_001}

	l := domain.UsageLedger{}
	l.Add(model, 100_000, short)
	l.Add(model, 300_000, long)
	l.Add(model, 272_000, edge)
	l.Add(model, 272_001, over)

	got, unpriced, anyPriced := b.CostLedger(l)
	if !anyPriced || len(unpriced) != 0 {
		t.Fatalf("anyPriced=%v unpriced=%v, want priced with none unpriced", anyPriced, unpriced)
	}
	cost := func(u domain.Usage, p int64) float64 {
		c, _ := b.Cost(model, u, p)
		return c
	}
	want := cost(short, 100_000) + cost(long, 300_000) + cost(edge, 272_000) + cost(over, 272_001)
	if !almostEqual(got, want, 1e-12) {
		t.Fatalf("CostLedger = %.9f, want %.9f", got, want)
	}
	if lifetime := cost(short.Plus(long).Plus(edge).Plus(over), 300_000); almostEqual(got, lifetime, 1e-9) {
		t.Fatalf("CostLedger priced the lifetime at the long tier (%.6f)", lifetime)
	}
	// The boundary itself: 272000 prices at base, 272001 at the long tier.
	if e, o := cost(domain.Usage{Input: 1_000_000}, 272_000), cost(domain.Usage{Input: 1_000_000}, 273_000); almostEqual(e, o, 1e-12) {
		t.Fatalf("272000 and 273000 priced alike; the test does not exercise the boundary")
	}
	edgeOnly := domain.UsageLedger{}
	edgeOnly.Add(model, 272_000, domain.Usage{Input: 1_000_000})
	if got, _, _ := b.CostLedger(edgeOnly); !almostEqual(got, cost(domain.Usage{Input: 1_000_000}, 272_000), 1e-12) {
		t.Fatalf("272000-token bucket priced %.9f, want the base-tier %.9f", got, cost(domain.Usage{Input: 1_000_000}, 272_000))
	}
	overOnly := domain.UsageLedger{}
	overOnly.Add(model, 272_001, domain.Usage{Input: 1_000_000})
	if got, _, _ := b.CostLedger(overOnly); !almostEqual(got, cost(domain.Usage{Input: 1_000_000}, 272_001), 1e-12) {
		t.Fatalf("272001-token bucket priced %.9f, want the long-tier %.9f", got, cost(domain.Usage{Input: 1_000_000}, 272_001))
	}
}

// TestCostLedgerReportsUnpricedModels: a bucket whose model does not
// resolve is left out of the total and named; an unresolved bucket with no
// tokens names nothing.
func TestCostLedgerReportsUnpricedModels(t *testing.T) {
	b := frozenBook(t)
	l := domain.UsageLedger{}
	l.Add("claude-opus-5", 1_000, domain.Usage{Input: 1_000})
	l.Add("mystery-model", 1_000, domain.Usage{Input: 1_000})
	l[domain.UsageKey{Model: "empty-model"}] = domain.Usage{}

	got, unpriced, anyPriced := b.CostLedger(l)
	want, _ := b.Cost("claude-opus-5", domain.Usage{Input: 1_000}, 1_000)
	if !anyPriced || !almostEqual(got, want, 1e-12) {
		t.Fatalf("CostLedger = (%v, anyPriced=%v), want %v priced", got, anyPriced, want)
	}
	if !reflect.DeepEqual(unpriced, []string{"mystery-model"}) {
		t.Fatalf("unpriced = %v, want [mystery-model]", unpriced)
	}

	none := domain.UsageLedger{}
	none.Add("mystery-model", 1_000, domain.Usage{Input: 1_000})
	if _, unpriced, anyPriced := b.CostLedger(none); anyPriced || len(unpriced) != 1 {
		t.Fatalf("all-unpriced ledger: anyPriced=%v unpriced=%v, want false and one model", anyPriced, unpriced)
	}
}

func BenchmarkCost(b *testing.B) {
	book, err := Load()
	if err != nil {
		b.Fatal(err)
	}
	u := domain.Usage{Input: 1_000, Output: 500, CacheRead: 100_000, CacheCreate5m: 2_000}
	b.ReportAllocs()
	for b.Loop() {
		book.Cost("gpt-5.6-terra", u, 150_000)
	}
}

func BenchmarkCostLedger(b *testing.B) {
	book, err := Load()
	if err != nil {
		b.Fatal(err)
	}
	l := domain.UsageLedger{}
	for p := int64(1_000); p <= 400_000; p += 1_000 {
		l.Add("gpt-5.6-terra", p, domain.Usage{Input: 100, Output: 50, CachedInput: 50})
	}
	b.ReportAllocs()
	for b.Loop() {
		book.CostLedger(l)
	}
}
