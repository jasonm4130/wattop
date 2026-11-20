package pricing

import (
	"regexp"
	"strings"
)

type Rates struct {
	model string
	entry modelEntry
	card  *rateCard
}

var (
	bracketSuffixRe = regexp.MustCompile(`\[[^\]]*\]$`)
	dateSuffixRe    = regexp.MustCompile(`-\d{8}$`)
)

func (b *Book) Resolve(model string) (Rates, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	models := b.snap.Models

	if e, ok := models[model]; ok {
		return b.rates(model, e), true
	}

	stripped := bracketSuffixRe.ReplaceAllString(model, "")
	if stripped != model {
		if e, ok := models[stripped]; ok {
			return b.rates(stripped, e), true
		}
	}

	noDate := dateSuffixRe.ReplaceAllString(stripped, "")
	if noDate != stripped {
		if e, ok := models[noDate]; ok {
			return b.rates(noDate, e), true
		}
	}

	for _, prefix := range [...]string{"anthropic.", "openai."} {
		candidate := prefix + noDate
		if e, ok := models[candidate]; ok {
			return b.rates(candidate, e), true
		}
	}

	var bestKey string
	for key := range models {
		if strings.HasPrefix(noDate, key) && len(key) > len(bestKey) {
			bestKey = key
		}
	}
	if bestKey != "" {
		return b.rates(bestKey, models[bestKey]), true
	}

	return Rates{}, false
}

func (b *Book) rates(model string, e modelEntry) Rates {
	if c, ok := b.cards.Load(model); ok {
		return Rates{model: model, entry: e, card: c.(*rateCard)}
	}
	c, _ := b.cards.LoadOrStore(model, parseRateCard(e))
	return Rates{model: model, entry: e, card: c.(*rateCard)}
}
