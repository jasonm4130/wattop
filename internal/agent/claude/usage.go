package claude

import (
	"github.com/jasonm4130/wattop/internal/agent/tokenrate"
	"github.com/jasonm4130/wattop/internal/domain"
)

// usageAccounting replaces repeated usage for a message rather than adding
// it again. Claude emits separate thinking/text/tool rows for one request.
type usageAccounting struct {
	usage  domain.Usage
	seen   map[string]domain.Usage
	window tokenrate.Window
}

func (a *usageAccounting) add(ev Event) {
	if !ev.HasUsage {
		return
	}
	if a.seen == nil {
		a.seen = make(map[string]domain.Usage)
	}
	old, exists := a.seen[ev.MessageID]
	u := ev.Usage
	if exists && old == u {
		return
	}
	a.seen[ev.MessageID] = u
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
}
