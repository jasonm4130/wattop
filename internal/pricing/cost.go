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

// tieredRate returns the rate an entry charges for base, selecting among
// whatever *_above_<N>k_tokens variants the entry itself carries — never a
// hardcoded tier name. gpt-5.6-terra carries *_above_272k_tokens keys;
// claude-opus-5 carries no *_above_200k_tokens key at all, so it always
// falls through to the base rate regardless of promptTokens.
func tieredRate(e modelEntry, base string, promptTokens int64) float64 {
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
	if len(tiers) == 0 {
		return baseRate
	}

	sort.Slice(tiers, func(i, j int) bool { return tiers[i].threshold < tiers[j].threshold })
	for i := len(tiers) - 1; i >= 0; i-- {
		if promptTokens > tiers[i].threshold {
			return tiers[i].rate
		}
	}
	return baseRate
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
	e := r.entry

	inputRate := tieredRate(e, "input_cost_per_token", promptTokens)
	outputRate := tieredRate(e, "output_cost_per_token", promptTokens)
	cacheReadRate := tieredRate(e, "cache_read_input_token_cost", promptTokens)
	cacheCreateRate := tieredRate(e, "cache_creation_input_token_cost", promptTokens)
	cacheCreate1hRate := tieredRate(e, "cache_creation_input_token_cost_above_1hr", promptTokens)
	if cacheCreate1hRate == 0 && u.CacheCreate1h > 0 {
		cacheCreate1hRate = cacheCreateRate
	}

	billableInput := u.Input - u.CachedInput
	if billableInput < 0 {
		billableInput = 0
	}
	cacheReadTokens := u.CacheRead + u.CachedInput

	usd = float64(billableInput)*inputRate +
		float64(u.Output)*outputRate +
		float64(cacheReadTokens)*cacheReadRate +
		float64(u.CacheCreate5m)*cacheCreateRate +
		float64(u.CacheCreate1h)*cacheCreate1hRate

	return usd, true
}
