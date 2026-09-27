package claude

import (
	"github.com/jasonm4130/wattop/internal/agent/tokenrate"
	"github.com/jasonm4130/wattop/internal/domain"
)

// syntheticModel is the model Claude Code writes on assistant records it
// generates locally (an API error, a login prompt) rather than receiving
// from the API. Such records carry all-zero usage and name no real model,
// so they must never become a session's model.
const syntheticModel = "<synthetic>"

// realModel reports whether m names a model the API actually ran.
func realModel(m string) bool {
	return m != "" && m != syntheticModel
}

// promptTokens is a usage record's prompt size: fresh input plus cache
// reads and writes. It selects the request's long-context pricing tier.
func promptTokens(u domain.Usage) int64 {
	return u.Input + u.CacheRead + u.CacheCreate5m + u.CacheCreate1h
}

// seenUsage is the last usage recorded for one message id, and the ledger
// bucket it was filed under.
type seenUsage struct {
	usage  domain.Usage
	model  string
	prompt int64
}

// usageAccounting replaces repeated usage for a message rather than adding
// it again. Claude emits separate thinking/text/tool rows for one request.
//
// ledger holds the same usage bucketed by each request's own model and
// prompt size, so cost can be priced per request rather than at the latest
// model and tier.
type usageAccounting struct {
	usage  domain.Usage
	seen   map[string]seenUsage
	ledger domain.UsageLedger
	window tokenrate.Window
}

// add records ev's usage, attributed to model (the record's own model, or
// the last real model seen when the record names none).
func (a *usageAccounting) add(ev Event, model string) {
	if !ev.HasUsage {
		return
	}
	if a.seen == nil {
		a.seen = make(map[string]seenUsage)
	}
	prev, exists := a.seen[ev.MessageID]
	old := prev.usage
	u := ev.Usage
	if exists && old == u {
		return
	}
	a.seen[ev.MessageID] = seenUsage{usage: u, model: model, prompt: promptTokens(u)}
	d := domain.Usage{
		Input: u.Input - old.Input, Output: u.Output - old.Output,
		CacheRead:     u.CacheRead - old.CacheRead,
		CacheCreate5m: u.CacheCreate5m - old.CacheCreate5m,
		CacheCreate1h: u.CacheCreate1h - old.CacheCreate1h,
		Thinking:      u.Thinking - old.Thinking,
	}
	a.usage.Input += d.Input
	a.usage.Output += d.Output
	a.usage.CacheRead += d.CacheRead
	a.usage.CacheCreate5m += d.CacheCreate5m
	a.usage.CacheCreate1h += d.CacheCreate1h
	a.usage.Thinking += d.Thinking
	a.window.Add(ev.Timestamp, d.Input+d.CacheRead+d.CacheCreate5m+d.CacheCreate1h, d.Output, d.CacheRead)

	// Re-file the message: take its previous usage out of the bucket it was
	// filed under and put its current usage in its own bucket, so a row
	// that revises a message's usage never leaves the old figure behind.
	// All-zero usage (a <synthetic> record) files nothing.
	if a.ledger == nil {
		a.ledger = make(domain.UsageLedger)
	}
	if exists && !old.IsZero() {
		a.fileLedger(prev.model, prev.prompt, domain.Usage{}.Minus(old))
	}
	if !u.IsZero() {
		a.fileLedger(model, promptTokens(u), u)
	}
}

// fileLedger adds u to one ledger bucket, dropping the bucket if that
// leaves it empty so a re-filed message leaves no zero entry behind.
func (a *usageAccounting) fileLedger(model string, prompt int64, u domain.Usage) {
	a.ledger.Add(model, prompt, u)
	k := domain.UsageKey{Model: model, PromptK: domain.PromptK(prompt)}
	if a.ledger[k].IsZero() {
		delete(a.ledger, k)
	}
}
