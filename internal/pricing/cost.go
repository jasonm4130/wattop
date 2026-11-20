package pricing

import (
	"regexp"
	"sort"
	"strconv"

	"github.com/jasonm4130/wattop/internal/domain"
)

var tierKeyRe = regexp.MustCompile(`^(.+)_above_(\d+)k_tokens$`)

const (
	baseInput         = "input_cost_per_token"
	baseOutput        = "output_cost_per_token"
	baseCacheRead     = "cache_read_input_token_cost"
	baseCacheCreate   = "cache_creation_input_token_cost"
	baseCacheCreate1h = "cache_creation_input_token_cost_above_1hr"
)

var rateBases = [...]string{baseInput, baseOutput, baseCacheRead, baseCacheCreate, baseCacheCreate1h}

type tier struct {
	aboveTokens int64
	rate        float64
}

type baseRates struct {
	base  float64
	tiers []tier
}

type rateCard struct {
	bases map[string]baseRates
}

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
			br.tiers = append(br.tiers, tier{aboveTokens: n * 1000, rate: rate})
			c.bases[m[1]] = br
			continue
		}
		br := c.bases[k]
		br.base = rate
		c.bases[k] = br
	}
	for k, br := range c.bases {
		sort.Slice(br.tiers, func(i, j int) bool { return br.tiers[i].aboveTokens < br.tiers[j].aboveTokens })
		c.bases[k] = br
	}
	return c
}

func (c *rateCard) rate(base string, promptTokens int64) float64 {
	br := c.bases[base]
	for i := len(br.tiers) - 1; i >= 0; i-- {
		if promptTokens > br.tiers[i].aboveTokens {
			return br.tiers[i].rate
		}
	}
	return br.base
}

func asFloat(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// Thinking tokens are already inside Output, so they are never added
// separately. For Codex, CachedInput is a subset of Input, not additive
// (total_tokens = input_tokens + output_tokens, with cached_input_tokens not
// added in), so it is subtracted out of the billable input.
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

func (b *Book) CostLedger(l domain.UsageLedger) (usd float64, unpricedModels []string, anyPriced bool) {
	type resolved struct {
		r  Rates
		ok bool
	}
	byModel := make(map[string]resolved)
	unpriced := make(map[string]struct{})
	for _, k := range keysInSummationOrder(l) {
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

func keysInSummationOrder(l domain.UsageLedger) []domain.UsageKey {
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
	return keys
}
