package pricing

import (
	"regexp"
	"strings"
)

// Rates is a resolved pricing entry for one model. It carries no exported
// fields — (*Book).Cost is the only place tier selection happens, and
// callers only ever need the (Rates, ok) pair Resolve returns.
type Rates struct {
	model string
	entry modelEntry
}

var (
	bracketSuffixRe = regexp.MustCompile(`\[[^\]]*\]$`)
	dateSuffixRe    = regexp.MustCompile(`-\d{8}$`)
)

// Resolve walks: exact key -> strip a bracketed suffix (claude-opus-5[1m] ->
// claude-opus-5) -> strip a trailing -YYYYMMDD date -> try anthropic./
// openai.-prefixed forms -> longest-prefix match. ok is false for a model
// the table does not carry; callers must then set Session.Priced=false and
// render "$—", never $0.00.
func (b *Book) Resolve(model string) (Rates, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	models := b.snap.Models

	if e, ok := models[model]; ok {
		return Rates{model: model, entry: e}, true
	}

	stripped := bracketSuffixRe.ReplaceAllString(model, "")
	if stripped != model {
		if e, ok := models[stripped]; ok {
			return Rates{model: stripped, entry: e}, true
		}
	}

	noDate := dateSuffixRe.ReplaceAllString(stripped, "")
	if noDate != stripped {
		if e, ok := models[noDate]; ok {
			return Rates{model: noDate, entry: e}, true
		}
	}

	for _, prefix := range [...]string{"anthropic.", "openai."} {
		candidate := prefix + noDate
		if e, ok := models[candidate]; ok {
			return Rates{model: candidate, entry: e}, true
		}
	}

	var bestKey string
	for key := range models {
		if strings.HasPrefix(noDate, key) && len(key) > len(bestKey) {
			bestKey = key
		}
	}
	if bestKey != "" {
		return Rates{model: bestKey, entry: models[bestKey]}, true
	}

	return Rates{}, false
}
