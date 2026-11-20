package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/pricing"
	"github.com/jasonm4130/wattop/internal/state"
	"github.com/jasonm4130/wattop/internal/ui"
)

type Sources struct {
	Sys     domain.Sampler
	Proc    domain.ProcSource
	Agents  []domain.AgentSource
	Pricing *pricing.Book
}

const sysReinitBackoff = 10 * time.Second

const pricingMaxAge = 30 * 24 * time.Hour

type Loop struct {
	Sources  Sources
	Interval time.Duration
	State    *state.State

	Send func(state.Inputs)

	MaxCycles int

	after func(time.Duration) <-chan time.Time

	lastSys      domain.SysSample
	lastOK       map[string]time.Time
	sysAvailable bool
	sysNextRetry time.Time
	n            int
}

func NewLoop(src Sources, interval time.Duration, st *state.State, sysAvailable bool) *Loop {
	return &Loop{
		Sources:      src,
		Interval:     interval,
		State:        st,
		after:        time.After,
		lastOK:       make(map[string]time.Time),
		sysAvailable: sysAvailable,
	}
}

func (l *Loop) Run(ctx context.Context) {
	for {
		if err := ctx.Err(); err != nil {
			return
		}
		start := time.Now()
		l.Cycle(ctx)
		if l.MaxCycles > 0 && l.n >= l.MaxCycles {
			return
		}
		if !l.pace(ctx, start) {
			return
		}
	}
}

func (l *Loop) pace(ctx context.Context, start time.Time) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	rest := l.Interval - time.Since(start)
	if rest <= 0 {
		return true
	}
	select {
	case <-ctx.Done():
		return false
	case <-l.after(rest):
		return true
	}
}

func (l *Loop) Cycle(ctx context.Context) state.Inputs {
	var sys domain.SysSample
	var errSys error

	if l.sysAvailable {
		sys, errSys = safeSample(ctx, l.Sources.Sys, l.Interval)
	} else {
		errSys = fmt.Errorf("soc: unavailable")
		if !l.sysNextRetry.After(time.Now()) {
			if err := l.Sources.Sys.Init(); err == nil {
				l.sysAvailable = true
				sys, errSys = safeSample(ctx, l.Sources.Sys, l.Interval)
			} else {
				l.sysNextRetry = time.Now().Add(sysReinitBackoff)
			}
		}
	}

	sysOK := errSys == nil
	if sysOK {
		l.lastSys = sys
	} else {
		sys = l.lastSys
	}

	procs, errProc := safeScan(ctx, l.Sources.Proc)
	procOK := errProc == nil

	at := time.Now()

	health := []state.SourceHealth{
		l.health("soc", sysOK, errSys, at),
		l.health("proc", procOK, errProc, at),
	}

	var sessions []domain.Session
	for _, agent := range l.Sources.Agents {
		sess, err := safePoll(ctx, agent, at, procs)
		ok := err == nil
		if ok {
			sessions = append(sessions, sess...)
		}
		health = append(health, l.health(agent.Name(), ok, err, at))
	}

	if l.Sources.Pricing != nil {
		_, _, fetchedAt := l.Sources.Pricing.Source()
		pricingOK, errPricing := pricingHealth(l.Sources.Pricing.Entries(), fetchedAt, at)
		health = append(health, l.health("pricing", pricingOK, errPricing, at))
	}

	in := state.Inputs{
		At:       at,
		Sys:      sys,
		Procs:    procs,
		Sessions: sessions,
		Health:   health,
	}

	l.n++
	if l.Send != nil {
		l.Send(in)
	}
	return in
}

func pricingHealth(entries int, fetchedAt, now time.Time) (bool, error) {
	if entries == 0 {
		return false, fmt.Errorf("pricing table is empty")
	}
	if fetchedAt.IsZero() {
		return false, fmt.Errorf("pricing table has no generation stamp")
	}
	if age := now.Sub(fetchedAt); age > pricingMaxAge {
		return false, fmt.Errorf("pricing table is %d days old; refresh is failing", int(age.Hours()/24))
	}
	return true, nil
}

func (l *Loop) health(name string, ok bool, err error, at time.Time) state.SourceHealth {
	if ok {
		l.lastOK[name] = at
	}
	h := state.SourceHealth{Name: name, OK: ok, LastOK: l.lastOK[name]}
	if err != nil {
		h.Err = err.Error()
	}
	return h
}

func safeSample(ctx context.Context, s domain.Sampler, interval time.Duration) (out domain.SysSample, err error) {
	defer recoverToErr(&err)
	return s.Sample(ctx, int(interval.Milliseconds()))
}

func safeScan(ctx context.Context, p domain.ProcSource) (out []domain.ProcSample, err error) {
	defer recoverToErr(&err)
	return p.Scan(ctx)
}

func safePoll(ctx context.Context, a domain.AgentSource, now time.Time, procs []domain.ProcSample) (out []domain.Session, err error) {
	defer recoverToErr(&err)
	return a.Poll(ctx, now, procs)
}

func recoverToErr(err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("panic: %v", r)
	}
}

func discardStdLog() (restore func()) {
	prev := log.Writer()
	log.SetOutput(io.Discard)
	return func() { log.SetOutput(prev) }
}

func runInteractive(ctx context.Context, loop *Loop, model ui.Model) error {
	defer discardStdLog()()

	p := tea.NewProgram(model, tea.WithContext(ctx))

	loop.Send = func(in state.Inputs) {
		p.Send(ui.CycleMsg{Inputs: in})
	}

	go loop.Run(ctx)

	_, err := p.Run()
	return err
}
