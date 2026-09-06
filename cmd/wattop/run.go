// run.go is the coordinated sampling cycle: one goroutine, one producer,
// one state.Inputs per cycle. See the Task 13 spec for why three
// independent tickers cannot support the product's central claim (SoC
// watts and agent dollars read at the same instant, joinable by pid).
package main

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/pricing"
	"github.com/jasonm4130/wattop/internal/state"
	"github.com/jasonm4130/wattop/internal/ui"
)

// Sources bundles one coordinated cycle's collaborators. main.go builds the
// real ones (soc.NewSampler, proc.NewScanner, claude.NewSource,
// codex.NewSource); run_test.go builds fakes over replay data so the cycle
// itself is testable without cgo, real files, or real time.
//
// Pricing is the same *pricing.Book main.go hands to state.New. The cycle
// never prices anything itself (the reducer does), but it reports the
// book's health each cycle -- see pricingHealth. A nil Book simply omits
// that health entry.
type Sources struct {
	Sys     domain.Sampler
	Proc    domain.ProcSource
	Agents  []domain.AgentSource
	Pricing *pricing.Book
}

// sysReinitBackoff is how long Loop waits between attempts to re-init a
// soc.Sampler that failed Init at startup or that Sample has started
// erroring on, so a persistently broken source is retried but never
// hammered every cycle.
const sysReinitBackoff = 10 * time.Second

// pricingMaxAge is how old the pricing table may get before the cycle
// reports the pricing source as degraded.
//
// Age is the only predicate available: (*pricing.Book).Refresh spawns a
// goroutine and swallows every failure into a log line, so cmd/wattop has
// no error to check -- a network outage or an unreachable GitHub source is
// visible here only as a table that stops getting younger. Refresh's own
// TTL is 24h, so a working refresh keeps the age under a day; 30 days
// leaves an offline binary a month of headroom before the badge fires,
// while a permanently-failing refresh still surfaces instead of never.
const pricingMaxAge = 30 * 24 * time.Hour

// Loop drives the coordinated sampling cycle described in the Task 13
// spec: soc.Sample blocks for the cycle period and is itself the pacing
// mechanism when it is available; when it is not (soc.Init failed at
// startup, or every Sample call is erroring), the loop paces itself with
// Interval instead so the session half of the dashboard keeps running.
type Loop struct {
	Sources  Sources
	Interval time.Duration
	State    *state.State

	// Send receives the built Inputs once per cycle. main.go wires this to
	// tea.Program.Send wrapped in ui.CycleMsg for the interactive TUI, or
	// to a plain accumulator for --json/--once. Nil is valid and simply
	// skips delivery, for a caller (like a test) that only wants Cycle's
	// return value.
	Send func(state.Inputs)

	// MaxCycles stops Run after this many cycles when nonzero. Tests use
	// it instead of a timer so a run is bounded without sleeping on the
	// wall clock.
	MaxCycles int

	// after is the pacing timer, time.After in production. Tests replace
	// it to assert what the loop *asked* to wait for without sleeping on
	// the wall clock.
	after func(time.Duration) <-chan time.Time

	lastSys      domain.SysSample
	lastOK       map[string]time.Time
	sysAvailable bool
	sysNextRetry time.Time
	n            int
}

// NewLoop constructs a Loop. sysAvailable should be the result of the
// caller's own soc.Sampler.Init() call: main.go calls Init once at startup
// (per the spec's "startup failure is soft and specific") and passes the
// result in rather than Loop calling Init itself, so the one-line banner on
// failure stays main.go's responsibility.
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

// Run executes cycles until ctx is cancelled or MaxCycles is reached,
// pacing each one to Interval.
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

// pace sleeps out whatever is left of Interval after a cycle that started
// at start, and reports whether the loop should keep going.
//
// On the healthy path this waits for nothing: soc.Sample blocked inside C
// for the full interval, so the elapsed time already covers it and "the
// block *is* the period" as the spec says. The wait exists for every path
// where the cycle returns early instead -- soc.Init failed at startup and
// there is no blocking sampler, or Sample errors (or panics and recovers)
// straight back. Without it, a source that fails fast turns the producer
// goroutine into a busy spin: 20 cycles of an instantly-erroring sampler
// completed in 7.5 microseconds before this existed.
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

// Cycle runs exactly one coordinated sampling cycle and returns the
// resulting Inputs, calling Send with it (if set) before returning.
//
// Steps, in order, per the Task 13 spec: soc.Sample blocks for the full
// interval (the cycle's pacing mechanism, when available); proc.Scan runs
// immediately on return; time.Now is read once, after both, as the cycle's
// single stamp; every agent source is polled with that stamp and this
// cycle's own procs, and its sessions are concatenated in order. Nothing in
// this method may panic out of it: soc.Sample, proc.Scan and each agent's
// Poll each run under recover, and a failure there degrades that source
// (via a state.SourceHealth entry) rather than aborting the cycle. The
// pricing book contributes a fifth health entry from its own state; it
// costs no I/O and cannot fail the cycle.
//
// Cycle, not Run, counts cycles, so MaxCycles bounds every caller that
// drives cycles directly (runJSON) as well as Run itself.
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
		// A Sample error or recovered panic never disables soc as the
		// pacing source -- Loop.Run keeps calling Sample every cycle and
		// the badge clears the moment one succeeds again. Only a source
		// that never resolved at startup (Init failed; l.sysAvailable was
		// already false on entry) takes the backoff-retried Init path
		// above. Either way Loop.pace covers the period the block did not.
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

// pricingHealth is the "pricing" source's OK predicate, kept as a pure
// function of the book's observable state so both branches are testable --
// a *pricing.Book with an old snapshot cannot be constructed from outside
// the package.
//
// An empty table means the embedded snapshot decoded to nothing, which is
// a build defect rather than a runtime condition; an old one means every
// refresh since has failed (or none has run), which is exactly the case
// the spec wants surfacing as a badge instead of silently pricing today's
// tokens off last quarter's rates.
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

// safeSample, safeScan and safePoll convert a collector panic into an error
// via recover, so one misbehaving source degrades rather than taking the
// whole cycle (and the dashboard) down with it.

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

// runInteractive builds the Bubble Tea program and drives Loop.Cycle on its
// own goroutine, sending each cycle's Inputs into the program as one
// ui.CycleMsg. It blocks until the program exits (quit key, or ctx
// cancellation).
func runInteractive(ctx context.Context, loop *Loop, model ui.Model) error {
	p := tea.NewProgram(model, tea.WithContext(ctx))

	loop.Send = func(in state.Inputs) {
		p.Send(ui.CycleMsg{Inputs: in})
	}

	go loop.Run(ctx)

	_, err := p.Run()
	return err
}
