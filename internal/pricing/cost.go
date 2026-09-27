package pricing

import (
	"regexp"
	"sort"
	"strconv"

	"github.com/jasonm4130/wattop/internal/domain"
)

// tierKeyRe matches a tiered variant of a base cost key, e.g.
// "input_cost_per_token_above_272k_tokens" for base
// "input_cost_per_token", threshold "272". Anything past the k/tokens
// suffix (a "_flex" or "_priority" variant) does not match and is ignored:
// only the plain per-token rate is priced here.
var tierKeyRe = regexp.MustCompile(`^(.+)_above_(\d+)k_tokens$`)

// The base cost keys Cost prices from.
const (
	baseInput         = "input_cost_per_token"
	baseOutput        = "output_cost_per_token"
	baseCacheRead     = "cache_read_input_token_cost"
	baseCacheCreate   = "cache_creation_input_token_cost"
	baseCacheCreate1h = "cache_creation_input_token_cost_above_1hr"
)

var rateBases = [...]string{baseInput, baseOutput, baseCacheRead, baseCacheCreate, baseCacheCreate1h}

type tier struct {
	threshold int64 // tokens; the tier applies to a prompt strictly above it
	rate      float64
}

// baseRates is one base key's flat rate plus whatever *_above_<N>k_tokens
// tiers the entry carries for it, ascending by threshold.
type baseRates struct {
	base  float64
	tiers []tier
}

// rateCard is a model entry's rates parsed once: tierKeyRe runs over the
// entry's keys when the card is built, never per Cost call. Cards are
// cached per resolved model on the Book (see (*Book).cardFor).
type rateCard struct {
	bases map[string]baseRates
}

// parseRateCard reads every base rate and every tiered variant the entry
// itself carries — never a hardcoded tier name. gpt-5.6-terra carries
// *_above_272k_tokens keys; claude-opus-5 carries no *_above_200k_tokens
// key at all, so it always prices at the base rate regardless of prompt
// size.
func parseRateCard(e modelEntry) *rateCard {
	c := &rateCard{bases: make(map[string]baseRates)}
	for k, v := range e {
		rate, ok := asFloat(v)
		if !ok {
			continue
		}
		if m := tierKeyRe.FindStringSubmatch(k); m != nil {
			n, err := strconv.ParseInt(m[2], 10, 64)
			if err != nil {
				continue
			}
			br := c.bases[m[1]]
			br.tiers = append(br.tiers, tier{threshold: n * 1000, rate: rate})
			c.bases[m[1]] = br
			continue
		}
		br := c.bases[k]
		br.base = rate
		c.bases[k] = br
	}
	for k, br := range c.bases {
		sort.Slice(br.tiers, func(i, j int) bool { return br.tiers[i].threshold < br.tiers[j].threshold })
		c.bases[k] = br
	}
	return c
}

// rate returns what the card charges for base at a prompt of promptTokens:
// the highest tier whose threshold the prompt exceeds, else the base rate.
func (c *rateCard) rate(base string, promptTokens int64) float64 {
	br := c.bases[base]
	for i := len(br.tiers) - 1; i >= 0; i-- {
		if promptTokens > br.tiers[i].threshold {
			return br.tiers[i].rate
		}
	}
	return br.base
}

func asFloat(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// Cost prices one usage block:
//
//	input×input_rate + output×output_rate
//	  + cache_read×cache_read_rate
//	  + ephemeral_5m×cache_creation_rate
//	  + ephemeral_1h×cache_creation_above_1hr_rate
//
// Thinking tokens are already inside Output — never added separately, or
// they would be double-counted. promptTokens selects the tier where the
// entry carries *_above_<N>_tokens variants; pass the request's own prompt
// size.
//
// u.CacheRead (Claude) and u.CachedInput (Codex) are mutually exclusive
// sources for the same concept — a cache hit billed at the discounted
// cache-read rate — so both feed the same term. For Codex, CachedInput is
// a subset of Input, not additive (proved arithmetically from a real
// record: total 230377 = input 227924 + output 2453, with cached 210432
// not added in), so it is first subtracted out of the billable input
// before the input rate is applied.
func (b *Book) Cost(model string, u domain.Usage, promptTokens int64) (usd float64, priced bool) {
	r, ok := b.Resolve(model)
	if !ok {
		return 0, false
	}
	return r.cost(u, promptTokens), true
}

func (r Rates) cost(u domain.Usage, promptTokens int64) float64 {
	c := r.card
	inputRate := c.rate(baseInput, promptTokens)
	outputRate := c.rate(baseOutput, promptTokens)
	cacheReadRate := c.rate(baseCacheRead, promptTokens)
	cacheCreateRate := c.rate(baseCacheCreate, promptTokens)
	cacheCreate1hRate := c.rate(baseCacheCreate1h, promptTokens)
	if cacheCreate1hRate == 0 && u.CacheCreate1h > 0 {
		cacheCreate1hRate = cacheCreateRate
	}

	billableInput := u.Input - u.CachedInput
	if billableInput < 0 {
		billableInput = 0
	}
	cacheReadTokens := u.CacheRead + u.CachedInput

	return float64(billableInput)*inputRate +
		float64(u.Output)*outputRate +
		float64(cacheReadTokens)*cacheReadRate +
		float64(u.CacheCreate5m)*cacheCreateRate +
		float64(u.CacheCreate1h)*cacheCreate1hRate
}

// CostLedger prices a usage ledger bucket by bucket, each at its own model
// and at PromptK*1000 prompt tokens — the same tier the request's exact
// prompt size selects (see domain.UsageKey). Each distinct model resolves
// once.
//
// unpricedModels lists, sorted, every model whose buckets carry tokens but
// which the book cannot price (including "" for usage filed before any
// model was known); those buckets are left out of usd. anyPriced reports
// whether at least one bucket priced. A caller with anyPriced false has no
// cost at all and must render "$—"; one with both anyPriced and unpriced
// models has a partial total.
func (b *Book) CostLedger(l domain.UsageLedger) (usd float64, unpricedModels []string, anyPriced bool) {
	type resolved struct {
		r  Rates
		ok bool
	}
	// Summed in key order: floating-point addition is not associative, and
	// map order would make the same ledger's total wobble in its last bits
	// from one cycle to the next, which the burn tracker reads as spend.
	keys := make([]domain.UsageKey, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Model != keys[j].Model {
			return keys[i].Model < keys[j].Model
		}
		return keys[i].PromptK < keys[j].PromptK
	})

	byModel := make(map[string]resolved)
	unpriced := make(map[string]struct{})
	for _, k := range keys {
		u := l[k]
		res, seen := byModel[k.Model]
		if !seen {
			r, ok := b.Resolve(k.Model)
			res = resolved{r: r, ok: ok}
			byModel[k.Model] = res
		}
		if !res.ok {
			if !u.IsZero() {
				unpriced[k.Model] = struct{}{}
			}
			continue
		}
		usd += res.r.cost(u, k.PromptK*1000)
		anyPriced = true
	}
	for m := range unpriced {
		unpricedModels = append(unpricedModels, m)
	}
	sort.Strings(unpricedModels)
	return usd, unpricedModels, anyPriced
}
