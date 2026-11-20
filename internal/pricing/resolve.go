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

	key, e, ok := matchModel(b.snap.Models, model)
	if !ok {
		return Rates{}, false
	}
	c, _ := b.cards.Load(key)
	if c == nil {
		c, _ = b.cards.LoadOrStore(key, parseRateCard(e))
	}
	return Rates{model: key, entry: e, card: c.(*rateCard)}, true
}

func matchModel(models modelTable, model string) (string, modelEntry, bool) {
	if e, ok := models[model]; ok {
		return model, e, true
	}

	stripped := bracketSuffixRe.ReplaceAllString(model, "")
	if stripped != model {
		if e, ok := models[stripped]; ok {
			return stripped, e, true
		}
	}

	noDate := dateSuffixRe.ReplaceAllString(stripped, "")
	if noDate != stripped {
		if e, ok := models[noDate]; ok {
			return noDate, e, true
		}
	}

	for _, prefix := range [...]string{"anthropic.", "openai."} {
		candidate := prefix + noDate
		if e, ok := models[candidate]; ok {
			return candidate, e, true
		}
	}

	var bestKey string
	for key := range models {
		if strings.HasPrefix(noDate, key) && len(key) > len(bestKey) {
			bestKey = key
		}
	}
	if bestKey != "" {
		return bestKey, models[bestKey], true
	}

	return "", nil, false
}
