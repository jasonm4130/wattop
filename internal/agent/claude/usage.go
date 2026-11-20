package claude

import (
	"github.com/jasonm4130/wattop/internal/agent/tokenrate"
	"github.com/jasonm4130/wattop/internal/domain"
)

const syntheticModel = "<synthetic>"

func realModel(m string) bool {
	return m != "" && m != syntheticModel
}

func promptTokens(u domain.Usage) int64 {
	return u.Input + u.CacheRead + u.CacheCreate5m + u.CacheCreate1h
}

type seenUsage struct {
	usage  domain.Usage
	model  string
	prompt int64
}

// usageAccounting replaces repeated usage for a message rather than adding
// it again. Claude emits separate thinking/text/tool rows for one request.
type usageAccounting struct {
	usage  domain.Usage
	seen   map[string]seenUsage
	ledger domain.UsageLedger
	window tokenrate.Window
}

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

func (a *usageAccounting) fileLedger(model string, prompt int64, u domain.Usage) {
	a.ledger.Add(model, prompt, u)
	k := domain.UsageKey{Model: model, PromptK: domain.PromptK(prompt)}
	if a.ledger[k].IsZero() {
		delete(a.ledger, k)
	}
}
