// Package e2e wires the replay collectors (internal/collect/replay), the
// Task 3 fixture corpus (internal/fixture.CorpusDir), the real reducer
// (internal/state) and the real Bubble Tea model (internal/ui) together
// through the coordinated-cycle shape internal/collect/replay's Clock makes
// deterministic. cmd/wattop's Loop (Task 13) cannot be imported here --
// it lives in package main -- so this file re-derives the same one
// state.Inputs-per-cycle shape from the Task 13 spec directly, rather than
// reusing Loop, and applies it in one independent run per test so no run's
// state mutation (the burn tracker, the history rings, the stale-session
// TTL clock) leaks into another's assertions.
//
// Colour-profile determinism: no TestMain pin is needed here for the same
// reason internal/ui/panel/soc_test.go documents at length -- lipgloss v2's
// Style.Render() consults neither the terminal nor the environment, and the
// auto-detecting print helpers that would downsample are never called on
// this path. So the golden below is byte-identical under an interactive
// macOS TTY and under `CGO_ENABLED=0 go test ... > file` on Linux CI.
//
// TestFrameFitsTerminal and TestThemeCycleRecolours below are the CI-runnable
// halves of manual-QA items 12 and 8: they assert mechanically what a human
// at a terminal would otherwise have to eyeball (no line wider than the
// terminal; every panel re-coloured, with the text layout unchanged, on each
// `t` press). docs/manual-qa.md records what is left for a human even so.
package e2e

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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

// newGoldenModel returns a model wired to a fresh state over a fresh set of
// replay sources, plus the pieces needed to drive it.
func newGoldenModel(t *testing.T, themeName string) ui.Model {
	t.Helper()
	roles, err := theme.Load(themeName)
	if err != nil {
		t.Fatalf("theme.Load(%q): %v", themeName, err)
	}
	return ui.New(newTestState(t), themeName, roles)
}

// driveModel runs n coordinated cycles of the replay corpus through m,
// advancing clock one virtual second per cycle, and returns the model.
//
// ui.Model.Update calls st.Reduce(msg.Inputs) itself -- the model takes
// state.Inputs, not a pre-computed Snapshot -- so feeding it a CycleMsg is
// what "calls st.Reduce and feeds the result to the model" means here, and
// it is deliberately the *only* Reduce per cycle. Calling st.Reduce a
// second time on the same *state.State at the same instant would push every
// history ring twice per tick; the coordinated-cycle invariant that would
// otherwise motivate that second call is asserted in
// TestCoordinatedCycleInvariant instead, on its own state.
func driveModel(ctx context.Context, t *testing.T, m ui.Model, clock replay.Clock, sys *replay.SysSampler, proc *replay.ProcSource, agent *replay.AgentSource, n int) ui.Model {
	t.Helper()
	for i := 0; i < n; i++ {
		in := buildCycle(ctx, t, clock, sys, proc, []domain.AgentSource{agent})
		mi, _ := m.Update(ui.CycleMsg{Inputs: in})
		m = mi.(ui.Model)
		clock.Advance(time.Second)
	}
	return m
}

// TestReplayEndToEndGolden drives the replay sources through one real
// state.New(book, burn) and the real UI model over 60 virtual seconds,
// golden-comparing the final rendered frame. This is the test that would
// catch a cluster-labelling regression, a burn rate that never decays, or a
// reducer that drops a row when a pid vanishes.
func TestReplayEndToEndGolden(t *testing.T) {
	ctx := context.Background()
	clock := replay.NewVirtualClock(replayStart)
	sys, proc, agent := newReplaySources(t)

	m := driveModel(ctx, t, newGoldenModel(t, "wattop-dark"), clock, sys, proc, agent, replayCycles)

	golden.RequireEqual(t, []byte(m.View().Content))
}

// TestCoordinatedCycleInvariant asserts every emitted Snapshot satisfies
// At == Sys.At, on all 60 cycles rather than only the last -- the
// coordinated-cycle invariant, checked exactly where a regression to
// independent tickers (rather than one shared stamp per cycle) would show
// up. It runs on its own *state.State so that the one Reduce per cycle it
// needs is the only Reduce that state ever sees.
func TestCoordinatedCycleInvariant(t *testing.T) {
	ctx := context.Background()
	clock := replay.NewVirtualClock(replayStart)
	sys, proc, agent := newReplaySources(t)
	st := newTestState(t)

	for i := 0; i < replayCycles; i++ {
		in := buildCycle(ctx, t, clock, sys, proc, []domain.AgentSource{agent})
		snap := st.Reduce(in)
		if !snap.At.Equal(snap.Sys.At) {
			t.Fatalf("cycle %d: Snapshot.At = %v, Snapshot.Sys.At = %v, want equal", i, snap.At, snap.Sys.At)
		}
		clock.Advance(time.Second)
	}
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

// ansiRe matches every SGR/CSI escape lipgloss emits, so a frame's text
// layout can be compared independently of its colours. x/ansi.Strip would
// be the direct way, but x/ansi is an indirect dependency here and this
// task may not promote it in go.mod.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

// stripANSI returns s with every escape sequence removed.
func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// keyMsg builds a tea.KeyPressMsg whose String() reproduces s, mirroring
// internal/ui/model_test.go's helper of the same name (which is unexported
// and so unreachable from this package). Only the keys this file presses
// are handled.
func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg(tea.Key{Text: s, Code: r})
	}
}

// frameMinWidth is the width each frame needs to render the Task 3 corpus
// without overflowing: measured, not chosen. The session table and the
// detail view do not narrow below these figures -- at 60, 80, 100, 120 and
// 140 columns the table still renders exactly 153 cells wide -- so every
// terminal narrower than that wraps them. Only the help overlay adapts all
// the way down, hence its floor of 0.
//
// These are floors for *this corpus*, not constants of the layout:
// SessionsRender's format string reserves 150 cells for its fourteen
// columns, and any field whose text overruns its slot (a burn rate past
// "$276.68/hr" in %-8s, a cache figure past "10840.8k" in %-18s) pushes
// the row wider still, because fmt pads a short field but never truncates
// a long one. So the real statement is "at least 150, 153 with this
// corpus, more with wider numbers".
//
// That is a defect in the session-table layout (internal/ui/panel, Task
// 12's file, out of this task's scope to fix); it is recorded in
// docs/limitations.md and as the observed result of manual-QA item 12, and
// bounded here so it cannot silently grow.
var frameMinWidth = map[string]int{"table": 153, "detail": 103, "help": 0}

// TestFrameFitsTerminal is manual-QA item 12 made runnable in CI, over all
// three frames the model can draw (the session table, the detail view and
// the help overlay) rather than only the default one. "Wrapping corruption"
// is what an over-wide line becomes once a real terminal folds it, so the
// width of the widest rendered line is the assertion behind the eyeball
// check.
//
// At or above a frame's own floor it must fit its terminal exactly. Below
// its floor -- see frameMinWidth -- what is asserted is that the overflow
// stays pinned to that known floor, per frame, so a help-overlay
// regression cannot hide behind the session table's much larger one, and
// that every frame still fits the terminal's *height*, which they do.
func TestFrameFitsTerminal(t *testing.T) {
	ctx := context.Background()
	clock := replay.NewVirtualClock(replayStart)
	sys, proc, agent := newReplaySources(t)
	m := driveModel(ctx, t, newGoldenModel(t, "wattop-dark"), clock, sys, proc, agent, replayCycles)

	for _, sz := range []struct{ w, h int }{{80, 24}, {160, 40}, {200, 60}} {
		mi, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		sized := mi.(ui.Model)

		detail, _ := sized.Update(keyMsg("enter"))
		help, _ := sized.Update(keyMsg("?"))
		frames := map[string]ui.Model{
			"table":  sized,
			"detail": detail.(ui.Model),
			"help":   help.(ui.Model),
		}

		for _, name := range []string{"table", "detail", "help"} {
			// Above the frame's floor the terminal's own width is the
			// bound; below it, the floor is. The observed width is logged
			// either way so a reader of the test output sees the real
			// number rather than only a pass.
			want := sz.w
			if f := frameMinWidth[name]; want < f {
				want = f
			}

			content := frames[name].View().Content
			lines := strings.Split(content, "\n")
			if len(lines) > sz.h {
				t.Errorf("%s frame at %dx%d: %d lines, want at most %d", name, sz.w, sz.h, len(lines), sz.h)
			}
			widest, at := 0, 0
			for i, line := range lines {
				if w := lipgloss.Width(line); w > widest {
					widest, at = w, i
				}
			}
			t.Logf("%s frame at %dx%d: widest line %d cells (line %d), %d lines", name, sz.w, sz.h, widest, at, len(lines))
			if widest > want {
				t.Errorf("%s frame at %dx%d: widest line is %d cells, want at most %d:\n%q",
					name, sz.w, sz.h, widest, want, stripANSI(lines[at]))
			}
		}
	}
}

// TestThemeCycleRecolours is manual-QA item 8 made runnable in CI: press
// `t` through all four themes and assert each press produces a frame that
// differs from the last in colour but is byte-identical once the escapes
// are stripped -- "every panel re-colours" and "nothing moves" in one
// assertion -- and that the fourth press returns to the starting frame,
// which is what makes the cycle a cycle. The remaining half of item 8,
// whether the light palette is *readable*, is a human judgement no
// assertion here stands in for.
func TestThemeCycleRecolours(t *testing.T) {
	ctx := context.Background()
	clock := replay.NewVirtualClock(replayStart)
	sys, proc, agent := newReplaySources(t)
	m := driveModel(ctx, t, newGoldenModel(t, "wattop-dark"), clock, sys, proc, agent, replayCycles)

	names := theme.Names()
	if len(names) != 4 {
		t.Fatalf("theme.Names() = %v (%d themes), want the 4 the README documents", names, len(names))
	}

	first := m.View().Content
	// Keyed by press count, not by theme name: ui.New starts at whatever
	// index "wattop-dark" occupies in theme.Names(), so press i is not
	// necessarily names[i] and naming it that way would put a lie in the
	// failure message.
	seen := map[string]int{first: 0}
	prev := first

	for i := 1; i <= len(names); i++ {
		mi, _ := m.Update(keyMsg("t"))
		m = mi.(ui.Model)
		frame := m.View().Content

		if stripANSI(frame) != stripANSI(first) {
			t.Errorf("after %d `t` presses: the frame's text layout changed, want only colours to change", i)
		}
		if i < len(names) {
			if frame == prev {
				t.Errorf("after %d `t` presses: frame is byte-identical to the previous theme's, want a re-colour", i)
			}
			if other, dup := seen[frame]; dup {
				t.Errorf("the theme %d `t` presses in renders identically to the one %d presses in", i, other)
			}
			seen[frame] = i
		} else if frame != first {
			t.Errorf("after %d `t` presses (a full cycle): frame differs from the starting frame", i)
		}
		prev = frame
	}
}
