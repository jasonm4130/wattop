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

const (
	burnWindow = 60 * time.Second
	burnAlpha  = 0.3
)

var replayStart = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

const replayCycles = 60

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

func newGoldenModel(t *testing.T, themeName string) ui.Model {
	t.Helper()
	roles, err := theme.Load(themeName)
	if err != nil {
		t.Fatalf("theme.Load(%q): %v", themeName, err)
	}
	return ui.New(newTestState(t), themeName, roles)
}

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

func TestReplayEndToEndGolden(t *testing.T) {
	ctx := context.Background()
	clock := replay.NewVirtualClock(replayStart)
	sys, proc, agent := newReplaySources(t)

	m := driveModel(ctx, t, newGoldenModel(t, "wattop-dark"), clock, sys, proc, agent, replayCycles)

	golden.RequireEqual(t, []byte(m.View().Content))
}

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

func findSession(sessions []domain.Session, agent, id string) (domain.Session, bool) {
	for _, s := range sessions {
		if s.Agent == agent && s.ID == id {
			return s, true
		}
	}
	return domain.Session{}, false
}

func TestStaleThenDropLifecycle(t *testing.T) {
	const (
		dropAgent   = "claude"
		dropID      = "sess-goes-stale-then-drops"
		dropAtCycle = 20
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

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

var footerStatusLineRe = regexp.MustCompile(`(?m)^.*\? help$`)

func normalizeFooterStatusLine(s string) string {
	return footerStatusLineRe.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Repeat("#", len(m))
	})
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg(tea.Key{Text: s, Code: r})
	}
}

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

func measureFrame(m ui.Model) (widest, at int, lines []string) {
	lines = strings.Split(m.View().Content, "\n")
	for i, line := range lines {
		if w := lipgloss.Width(line); w > widest {
			widest, at = w, i
		}
	}
	return widest, at, lines
}

var frameCases = []struct {
	w, h  int
	frame string
}{
	{80, 24, "table"}, {80, 24, "detail"}, {80, 24, "help"},
	{110, 24, "table"}, {120, 30, "table"},
	{160, 40, "table"}, {160, 40, "detail"}, {160, 40, "help"},
	{200, 60, "table"}, {200, 60, "detail"}, {200, 60, "help"},
}

func TestFrameFitsTerminal(t *testing.T) {
	ctx := context.Background()
	clock := replay.NewVirtualClock(replayStart)
	sys, proc, agent := newReplaySources(t)
	m := driveModel(ctx, t, newGoldenModel(t, "wattop-dark"), clock, sys, proc, agent, replayCycles)

	for _, c := range frameCases {
		if _, known := overflowAt80[c.frame]; known && c.w == 80 {
			continue
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

var overflowAt80 = map[string]int{"detail": 103}

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
		if len(lines) > 24 {
			t.Errorf("%s frame at 80x24: %d lines, want at most 24", name, len(lines))
		}
	}
}

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
