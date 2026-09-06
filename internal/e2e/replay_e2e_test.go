// Package e2e wires the replay collectors (internal/collect/replay), the
// Task 3 fixture corpus (internal/fixture.CorpusDir), the real reducer
// (internal/state) and the real Bubble Tea model (internal/ui) together
// through the coordinated-cycle shape internal/collect/replay's Clock makes
// deterministic. cmd/wattop's Loop (Task 13) cannot be imported here --
// it lives in package main -- so this file re-derives the same one
// state.Inputs-per-cycle shape from the Task 13 spec directly, rather than
// reusing Loop, and applies it in three independent runs so no run's state
// mutation (the burn tracker, the history rings, the stale-session TTL
// clock) leaks into another's assertions.
package e2e

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/jasonm4130/wattop/internal/collect/replay"
	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/fixture"
	"github.com/jasonm4130/wattop/internal/pricing"
	"github.com/jasonm4130/wattop/internal/state"
	"github.com/jasonm4130/wattop/internal/ui"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// burnWindow and burnAlpha mirror cmd/wattop/main.go's own constants so the
// reducer's burn-rate math here matches what a real run would compute over
// the same inputs. They cannot be imported (cmd/wattop is package main).
const (
	burnWindow = 60 * time.Second
	burnAlpha  = 0.3
)

// replayStart is the virtual clock's t=0, arbitrary but fixed so every run
// in this file is byte-for-byte reproducible.
var replayStart = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

// replayCycles is how many virtual seconds (one cycle each) every test in
// this file drives, per the Task 14 spec.
const replayCycles = 60

// newReplaySources builds a fresh Sys/Proc/Agent triple over the Task 3
// corpus for one run. Fresh instances matter: replay.SysSampler,
// replay.ProcSource and replay.AgentSource each hold their own read
// position, so two runs sharing one instance would silently desync.
func newReplaySources(t *testing.T) (*replay.SysSampler, *replay.ProcSource, *replay.AgentSource) {
	t.Helper()

	sys, err := replay.NewSysSampler(filepath.Join(fixture.CorpusDir(), "soc"))
	if err != nil {
		t.Fatalf("replay.NewSysSampler: %v", err)
	}
	proc, err := replay.NewProcSource(filepath.Join(fixture.CorpusDir(), "replay", "procs.json"))
	if err != nil {
		t.Fatalf("replay.NewProcSource: %v", err)
	}
	agent, err := replay.NewAgentSource(filepath.Join(fixture.CorpusDir(), "replay"))
	if err != nil {
		t.Fatalf("replay.NewAgentSource: %v", err)
	}
	return sys, proc, agent
}

// buildCycle runs the Task 13 coordinated-cycle shape for one tick: the SoC
// sample and process scan for this instant, then every agent polled with
// that same instant and process table, all stamped with one At from clock.
// Every replay source is infallible, so health is reported OK throughout --
// this file has nothing to say about the degraded-source paths Task 13's
// own tests already cover.
func buildCycle(ctx context.Context, t *testing.T, clock replay.Clock, sys domain.Sampler, proc domain.ProcSource, agents []domain.AgentSource) state.Inputs {
	t.Helper()

	sample, err := sys.Sample(ctx, 1000)
	if err != nil {
		t.Fatalf("sys.Sample: %v", err)
	}
	procs, err := proc.Scan(ctx)
	if err != nil {
		t.Fatalf("proc.Scan: %v", err)
	}

	at := clock.Now()

	var sessions []domain.Session
	for _, a := range agents {
		polled, err := a.Poll(ctx, at, procs)
		if err != nil {
			t.Fatalf("agent.Poll: %v", err)
		}
		sessions = append(sessions, polled...)
	}

	return state.Inputs{
		At:       at,
		Sys:      sample,
		Procs:    procs,
		Sessions: sessions,
		Health: []state.SourceHealth{
			{Name: "soc", OK: true, LastOK: at},
			{Name: "proc", OK: true, LastOK: at},
			{Name: "replay", OK: true, LastOK: at},
		},
	}
}

func newTestState(t *testing.T) *state.State {
	t.Helper()
	book, err := pricing.Load()
	if err != nil {
		t.Fatalf("pricing.Load: %v", err)
	}
	return state.New(book, pricing.NewBurnTracker(burnWindow, burnAlpha))
}

// TestReplayEndToEndGolden drives the replay sources through one real
// state.New(book, burn) and the real UI model over 60 virtual seconds,
// golden-comparing the final rendered frame. This is the test that would
// catch a cluster-labelling regression, a burn rate that never decays, or a
// reducer that drops a row when a pid vanishes.
//
// It also rides the coordinated-cycle invariant along for free: every
// emitted Snapshot must satisfy At == Sys.At, checked here on all 60
// cycles rather than only the last, since that is exactly where a
// regression to independent tickers (rather than one shared stamp) would
// show up.
func TestReplayEndToEndGolden(t *testing.T) {
	ctx := context.Background()
	clock := replay.NewVirtualClock(replayStart)
	sys, proc, agent := newReplaySources(t)
	st := newTestState(t)

	roles, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}
	m := ui.New(st, "wattop-dark", roles)

	for i := 0; i < replayCycles; i++ {
		in := buildCycle(ctx, t, clock, sys, proc, []domain.AgentSource{agent})

		snap := st.Reduce(in)
		if !snap.At.Equal(snap.Sys.At) {
			t.Fatalf("cycle %d: Snapshot.At = %v, Snapshot.Sys.At = %v, want equal", i, snap.At, snap.Sys.At)
		}

		mi, _ := m.Update(ui.CycleMsg{Inputs: in})
		m = mi.(ui.Model)

		clock.Advance(time.Second)
	}

	golden.RequireEqual(t, []byte(m.View().Content))
}

// staleThenDropAgent wraps a real replay.AgentSource and, from dropAtCycle
// onward, filters dropID out of every poll -- scripting the "the source
// stops emitting this session" half of Task 10's stale-then-drop lifecycle
// on top of a real corpus-backed source, rather than a bespoke fixture file
// this task's file list does not include.
type staleThenDropAgent struct {
	inner       *replay.AgentSource
	dropAgent   string
	dropID      string
	dropAtCycle int
	cycle       int
}

func (a *staleThenDropAgent) Name() string { return a.inner.Name() }

func (a *staleThenDropAgent) Poll(ctx context.Context, now time.Time, procs []domain.ProcSample) ([]domain.Session, error) {
	sessions, err := a.inner.Poll(ctx, now, procs)
	if err != nil {
		return nil, err
	}
	cycle := a.cycle
	a.cycle++
	if cycle < a.dropAtCycle {
		return sessions, nil
	}
	out := make([]domain.Session, 0, len(sessions))
	for _, s := range sessions {
		if s.Agent == a.dropAgent && s.ID == a.dropID {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (a *staleThenDropAgent) Close() error { return a.inner.Close() }

// findSession returns the session in sessions matching (agent, id).
func findSession(sessions []domain.Session, agent, id string) (domain.Session, bool) {
	for _, s := range sessions {
		if s.Agent == agent && s.ID == id {
			return s, true
		}
	}
	return domain.Session{}, false
}

// TestStaleThenDropLifecycle scripts the replay agent source (Task 4) to
// stop emitting one session at t=20s and asserts the full lifecycle Task
// 10's reducer owns (sessionTTL = 30s): the row is present with
// Status == "stale" at t=25s, still present at t=49s (30s after its last
// real sighting at t=19s, not yet past the TTL), and gone entirely at
// t=51s. Manual QA step 3 observes the same behaviour against a real kill;
// this is the version CI can run.
func TestStaleThenDropLifecycle(t *testing.T) {
	const (
		dropAgent   = "claude"
		dropID      = "sess-goes-stale-then-drops"
		dropAtCycle = 20 // t=20s: first cycle the source omits the session.
	)

	ctx := context.Background()
	clock := replay.NewVirtualClock(replayStart)
	sys, proc, agent := newReplaySources(t)
	scripted := &staleThenDropAgent{inner: agent, dropAgent: dropAgent, dropID: dropID, dropAtCycle: dropAtCycle}
	st := newTestState(t)

	for i := 0; i < replayCycles; i++ {
		in := buildCycle(ctx, t, clock, sys, proc, []domain.AgentSource{scripted})
		snap := st.Reduce(in)

		switch i {
		case 19:
			if _, ok := findSession(snap.Sessions, dropAgent, dropID); !ok {
				t.Fatalf("cycle %d (t=%ds, last real sighting): session missing, want present", i, i)
			}
		case 25:
			s, ok := findSession(snap.Sessions, dropAgent, dropID)
			if !ok {
				t.Fatalf("cycle %d (t=25s): session missing, want present and stale", i)
			}
			if s.Status != "stale" {
				t.Errorf("cycle %d (t=25s): Status = %q, want %q", i, s.Status, "stale")
			}
		case 49:
			if _, ok := findSession(snap.Sessions, dropAgent, dropID); !ok {
				t.Fatalf("cycle %d (t=49s): session missing, want still present (30s since t=19s, not yet past sessionTTL)", i)
			}
		case 51:
			if _, ok := findSession(snap.Sessions, dropAgent, dropID); ok {
				t.Fatalf("cycle %d (t=51s): session present, want dropped (32s since t=19s, past sessionTTL)", i)
			}
		}

		clock.Advance(time.Second)
	}
}
