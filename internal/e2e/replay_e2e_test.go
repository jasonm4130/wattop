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
//
// It sits ~3 h after every usage timestamp in testdata/replay/sessions.json
// (tool calls at 09:01-09:10Z), which is why every $/HR cell in the golden
// reads $0.00. That is the burn tracker working, not a broken fixture: a
// cost delta whose newest transcript record is older than the 60 s burn
// window is spend that happened before wattop was watching, and counting it
// as a rate is what produced the $105,679/hr headline in QA 2026-09-06 §4.
// Numeric burn coverage belongs in internal/pricing/burn_test.go, which
// pins exact rates ($3.60/hr, $60.00/hr) against controlled timestamps; the
// footer's hot-rate styling is covered by
// internal/ui/panel.TestFooterBurnGoesHotAboveThreshold. Restamping this
// corpus would not restore either: its cost series loops 1.42 -> 1.71 ->
// 2.02 -> 1.42 every three cycles, so any rate it produced would be an
// artifact of the loop.
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

// footerStatusLineRe matches the footer's right-aligned "sort:... theme:...
// ? help" status line, added so Model.View() no longer leaves sort/theme/
// pause cycling with zero on-screen feedback. That line is exactly what a
// `t` (or `s`, or `p`) press is supposed to change, so
// TestThemeCycleRecolours blanks it out before comparing layout -- the
// same way stripANSI blanks out colour -- rather than treating its
// changing theme name as a layout regression. It is replaced with filler
// of the same length (the line is always padded/aligned to the panel
// width) so a shift in leading-space padding from a longer or shorter
// theme name never itself reads as a layout change either.
var footerStatusLineRe = regexp.MustCompile(`(?m)^.*\? help$`)

// normalizeFooterStatusLine blanks the sort/theme/pause status line out of
// a rendered frame so a layout comparison sees only what those keys are
// meant to leave alone.
func normalizeFooterStatusLine(s string) string {
	return footerStatusLineRe.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Repeat("#", len(m))
	})
}

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

// sizedFrames drives m to a w x h terminal and returns the three frames the
// model can draw from that one size: the session table, the detail view
// (enter) and the help overlay (?).
func sizedFrames(m ui.Model, w, h int) map[string]ui.Model {
	mi, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	sized := mi.(ui.Model)
	detail, _ := sized.Update(keyMsg("enter"))
	help, _ := sized.Update(keyMsg("?"))
	return map[string]ui.Model{
		"table":  sized,
		"detail": detail.(ui.Model),
		"help":   help.(ui.Model),
	}
}

// measureFrame returns the width in cells of the widest line the frame
// renders, the index of that line, and the frame's own line count.
// "Wrapping corruption" is what an over-wide line becomes once a real
// terminal folds it, so the widest line is the measurement behind
// manual-QA item 12's eyeball check.
func measureFrame(m ui.Model) (widest, at int, lines []string) {
	lines = strings.Split(m.View().Content, "\n")
	for i, line := range lines {
		if w := lipgloss.Width(line); w > widest {
			widest, at = w, i
		}
	}
	return widest, at, lines
}

// frameCases enumerates every (size, frame) pair TestFrameFitsTerminal and
// TestSessionTableOverflowsAt80Columns divide between them, so that no pair
// is silently covered by neither test. Membership is the whole point: a
// pair that passes belongs in the first test, a pair that fails belongs in
// the second, and TestEveryFrameCaseIsCovered asserts the split is total.
var frameCases = []struct {
	w, h  int
	frame string
}{
	{80, 24, "table"}, {80, 24, "detail"}, {80, 24, "help"},
	{110, 24, "table"}, {120, 30, "table"},
	{160, 40, "table"}, {160, 40, "detail"}, {160, 40, "help"},
	{200, 60, "table"}, {200, 60, "detail"}, {200, 60, "help"},
}

// TestFrameFitsTerminal is manual-QA item 12 made runnable in CI, asserting
// the item's actual pass condition with no exception table: the frame fits
// the terminal it was given, in both axes, so nothing wraps.
//
// It covers only the (size, frame) pairs that genuinely satisfy that
// condition. The one pair that does NOT -- the detail view at 80x24 -- is a
// real, open layout defect, not an exemption this test grants itself, so it
// is asserted separately and by name in TestDetailViewOverflowsAt80Columns.
// Read the two test names together: wattop fits its terminal everywhere
// except the detail view below ~103 columns, where it does not.
func TestFrameFitsTerminal(t *testing.T) {
	ctx := context.Background()
	clock := replay.NewVirtualClock(replayStart)
	sys, proc, agent := newReplaySources(t)
	m := driveModel(ctx, t, newGoldenModel(t, "wattop-dark"), clock, sys, proc, agent, replayCycles)

	for _, c := range frameCases {
		if _, known := overflowAt80[c.frame]; known && c.w == 80 {
			continue // asserted by TestDetailViewOverflowsAt80Columns
		}
		widest, at, lines := measureFrame(sizedFrames(m, c.w, c.h)[c.frame])
		t.Logf("%s frame at %dx%d: widest line %d cells (line %d), %d lines", c.frame, c.w, c.h, widest, at, len(lines))
		if widest > c.w {
			t.Errorf("%s frame at %dx%d: widest line is %d cells, want at most %d (wraps in a real terminal):\n%q",
				c.frame, c.w, c.h, widest, c.w, stripANSI(lines[at]))
		}
		if len(lines) > c.h {
			t.Errorf("%s frame at %dx%d: %d lines, want at most %d", c.frame, c.w, c.h, len(lines), c.h)
		}
	}
}

// overflowAt80 records the frames that FAIL manual-QA item 12 at 80x24 and
// the exact width each one overflows to. This is a defect list, not a set
// of floors: the pass condition for every entry is 80, and no entry meets
// it.
//
// The session table used to be in here at 153 cells and is not any more.
// SessionsRender now narrows: columns shrink, the context gauge collapses
// to "~ 55%", the token triple collapses to one figure, surplus rows become
// a "N more" indicator, and $/$HR/CPU%/RSS survive at every width. It
// measures 80 cells at 80x24 on this corpus and is asserted to fit by
// TestFrameFitsTerminal like any other frame.
//
// The detail view was never narrowed and still reserves 103 cells. The fix
// is the same shape as the table's -- a responsive column set in
// internal/ui/panel/detail.go -- and when it lands, delete this map and
// TestDetailViewOverflowsAt80Columns, tick manual-QA item 12, and drop the
// remaining detail-view paragraph from docs/limitations.md. The 80x24 pair
// is already enumerated in frameCases and falls back into
// TestFrameFitsTerminal.
var overflowAt80 = map[string]int{"detail": 103}

// TestDetailViewOverflowsAt80Columns pins the known failure of manual-QA
// item 12 so that "the tests pass" cannot be read as "every frame fits at
// 80x24" -- the detail view does not, and this test says so in its name.
//
// The width is pinned with == rather than <=, deliberately. An inequality
// would let the defect outlive its own fix; equality fails both when the
// overflow grows and when a responsive column set removes it, which is
// exactly when this test should be deleted.
func TestDetailViewOverflowsAt80Columns(t *testing.T) {
	ctx := context.Background()
	clock := replay.NewVirtualClock(replayStart)
	sys, proc, agent := newReplaySources(t)
	m := driveModel(ctx, t, newGoldenModel(t, "wattop-dark"), clock, sys, proc, agent, replayCycles)

	frames := sizedFrames(m, 80, 24)
	for name, want := range overflowAt80 {
		widest, at, lines := measureFrame(frames[name])
		t.Logf("KNOWN DEFECT (manual-QA item 12): %s frame at 80x24 renders %d cells wide, %d cells past the terminal", name, widest, widest-80)
		if widest != want {
			t.Errorf("%s frame at 80x24: widest line is %d cells, pinned at %d.\n"+
				"If it is now <= 80 the layout defect is fixed: delete TestDetailViewOverflowsAt80Columns and overflowAt80, tick manual-QA item 12, and drop the detail-view paragraph from docs/limitations.md.\n"+
				"If it grew, the layout regressed further:\n%q",
				name, widest, want, stripANSI(lines[at]))
		}
		// Height is fine at 80x24 and must stay fine: the defect is
		// horizontal only.
		if len(lines) > 24 {
			t.Errorf("%s frame at 80x24: %d lines, want at most 24", name, len(lines))
		}
	}
}

// TestEveryFrameCaseIsCovered checks frameCases against sizedFrames rather
// than against a restatement of the expected answer: every frame the model
// can render must be enumerated at every size, so adding a panel cannot
// leave it unmeasured while the suite still reports "the frame fits". It
// also pins overflowAt80's keys to frames frameCases actually enumerates,
// so a stale entry there cannot silently exempt a frame from measurement.
func TestEveryFrameCaseIsCovered(t *testing.T) {
	enumerated := map[string]map[int]bool{}
	for _, c := range frameCases {
		if enumerated[c.frame] == nil {
			enumerated[c.frame] = map[int]bool{}
		}
		enumerated[c.frame][c.w] = true
	}

	ctx := context.Background()
	clock := replay.NewVirtualClock(replayStart)
	sys, proc, agent := newReplaySources(t)
	m := driveModel(ctx, t, newGoldenModel(t, "wattop-dark"), clock, sys, proc, agent, 1)

	for name := range sizedFrames(m, 80, 24) {
		widths, ok := enumerated[name]
		if !ok {
			t.Errorf("sizedFrames renders frame %q, which frameCases never enumerates: it is measured by neither test", name)
			continue
		}
		for _, w := range []int{80, 160, 200} {
			if !widths[w] {
				t.Errorf("frame %q is not enumerated at %d columns", name, w)
			}
		}
	}
	for name := range overflowAt80 {
		found := false
		for _, c := range frameCases {
			if c.w == 80 && c.frame == name {
				found = true
			}
		}
		if !found {
			t.Errorf("overflowAt80 lists %q, which frameCases does not enumerate at 80x24", name)
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

		if normalizeFooterStatusLine(stripANSI(frame)) != normalizeFooterStatusLine(stripANSI(first)) {
			t.Errorf("after %d `t` presses: the frame's text layout changed, want only colours (and the footer's theme name) to change", i)
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
