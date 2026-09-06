package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/pricing"
	"github.com/jasonm4130/wattop/internal/state"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	book, err := pricing.Load()
	if err != nil {
		t.Fatalf("pricing.Load: %v", err)
	}
	st := state.New(book, pricing.NewBurnTracker(5*time.Minute, 0.3))
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}
	return New(st, "wattop-dark", r)
}

// keyMsg builds a tea.KeyPressMsg whose String() reproduces s exactly, for
// every keystroke keys.go's keyAction recognises.
func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "up":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	case "down":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "ctrl+c":
		return tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg(tea.Key{Text: s, Code: r})
	}
}

func cycleMsg(at time.Time, sessions []domain.Session) CycleMsg {
	return CycleMsg{Inputs: state.Inputs{At: at, Sessions: sessions}}
}

// TestSelectionMovesWithinBounds drives Update with a scripted key
// sequence and asserts the selected row clamps at both ends rather than
// wrapping or going negative.
func TestSelectionMovesWithinBounds(t *testing.T) {
	m := newTestModel(t)

	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	sessions := []domain.Session{
		{Agent: "claude", ID: "a", Status: "busy"},
		{Agent: "claude", ID: "b", Status: "waiting"},
		{Agent: "codex", ID: "c", Status: "waiting"},
	}

	mi, _ := m.Update(cycleMsg(base, sessions))
	m = mi.(Model)
	if m.selected != 0 {
		t.Fatalf("selected = %d after first cycle, want 0", m.selected)
	}

	for _, key := range []string{"down", "down", "down", "down"} {
		mi, _ = m.Update(keyMsg(key))
		m = mi.(Model)
	}
	if want := m.flatRowCount() - 1; m.selected != want {
		t.Errorf("selected = %d after driving past the end, want clamped at %d", m.selected, want)
	}

	for _, key := range []string{"up", "up", "up", "up", "up"} {
		mi, _ = m.Update(keyMsg(key))
		m = mi.(Model)
	}
	if m.selected != 0 {
		t.Errorf("selected = %d after driving past the start, want clamped at 0", m.selected)
	}
}

// TestThemeCyclingForwardAndBack asserts "t" advances through the embedded
// theme list, wrapping, and "T" reverses it.
func TestThemeCyclingForwardAndBack(t *testing.T) {
	m := newTestModel(t)
	names := theme.Names()
	if len(names) < 2 {
		t.Fatal("need at least two themes to test cycling")
	}
	start := m.themeIdx

	mi, _ := m.Update(keyMsg("t"))
	m = mi.(Model)
	if m.themeIdx != (start+1)%len(names) {
		t.Fatalf("themeIdx = %d after one 't', want %d", m.themeIdx, (start+1)%len(names))
	}

	// Cycle all the way around forward and confirm it wraps back to start.
	for i := 0; i < len(names)-1; i++ {
		mi, _ = m.Update(keyMsg("t"))
		m = mi.(Model)
	}
	if m.themeIdx != start {
		t.Errorf("themeIdx = %d after a full forward cycle, want back at %d", m.themeIdx, start)
	}

	mi, _ = m.Update(keyMsg("T"))
	m = mi.(Model)
	want := (start - 1 + len(names)) % len(names)
	if m.themeIdx != want {
		t.Errorf("themeIdx = %d after one 'T', want %d", m.themeIdx, want)
	}
}

// TestPauseDropsCycleMsg asserts a paused Model's Snapshot does not change
// when a CycleMsg arrives, and resumes updating once unpaused.
func TestPauseDropsCycleMsg(t *testing.T) {
	m := newTestModel(t)
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	mi, _ := m.Update(cycleMsg(base, []domain.Session{{Agent: "claude", ID: "a", Status: "busy"}}))
	m = mi.(Model)
	firstAt := m.snap.At

	mi, _ = m.Update(keyMsg("p"))
	m = mi.(Model)
	if !m.paused {
		t.Fatal("expected paused == true after 'p'")
	}

	mi, _ = m.Update(cycleMsg(base.Add(10*time.Second), []domain.Session{{Agent: "claude", ID: "a", Status: "waiting"}}))
	m = mi.(Model)
	if !m.snap.At.Equal(firstAt) {
		t.Errorf("Snapshot.At advanced while paused: %v -> %v", firstAt, m.snap.At)
	}

	mi, _ = m.Update(keyMsg("p"))
	m = mi.(Model)
	mi, _ = m.Update(cycleMsg(base.Add(20*time.Second), []domain.Session{{Agent: "claude", ID: "a", Status: "busy"}}))
	m = mi.(Model)
	if m.snap.At.Equal(firstAt) {
		t.Error("Snapshot.At did not advance after unpausing")
	}
}

// TestQuitReturnsQuitCmd asserts 'q' and ctrl+c both yield a command that,
// when run, produces a tea.QuitMsg.
func TestQuitReturnsQuitCmd(t *testing.T) {
	for _, key := range []string{"q", "ctrl+c"} {
		t.Run(key, func(t *testing.T) {
			m := newTestModel(t)
			_, cmd := m.Update(keyMsg(key))
			if cmd == nil {
				t.Fatal("expected a non-nil Cmd")
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Errorf("expected the Cmd to produce a tea.QuitMsg")
			}
		})
	}
}

// TestToggleDetailFilterHelp exercises enter/f/? and asserts each flips
// its own bit without disturbing the others.
func TestToggleDetailFilterHelp(t *testing.T) {
	m := newTestModel(t)

	mi, _ := m.Update(keyMsg("enter"))
	m = mi.(Model)
	if !m.showDetail {
		t.Error("expected showDetail == true after enter")
	}

	mi, _ = m.Update(keyMsg("f"))
	m = mi.(Model)
	if !m.filterHeadless {
		t.Error("expected filterHeadless == true after 'f'")
	}

	mi, _ = m.Update(keyMsg("?"))
	m = mi.(Model)
	if !m.showHelp {
		t.Error("expected showHelp == true after '?'")
	}
	if !m.showDetail || !m.filterHeadless {
		t.Error("toggling help must not disturb showDetail/filterHeadless")
	}
}

// TestSortCyclesThroughSortKeys asserts 's' advances sortIdx and wraps,
// and that GPU is never among the cyclable keys -- the spec requires GPU
// ms/sec never be the default sort key, so it is excluded from the whole
// cycle rather than merely placed later in it.
func TestSortCyclesThroughSortKeys(t *testing.T) {
	m := newTestModel(t)
	for _, k := range sortKeys {
		if k == "gpu" {
			t.Fatalf("sortKeys must never include \"gpu\": %v", sortKeys)
		}
	}
	for i := 0; i < len(sortKeys); i++ {
		mi, _ := m.Update(keyMsg("s"))
		m = mi.(Model)
	}
	if m.sortIdx != 0 {
		t.Errorf("sortIdx = %d after a full cycle, want back at 0", m.sortIdx)
	}
}

// TestUpdateIsNonBlocking drives Update with 1,000 scripted messages and
// asserts the whole loop returns in under 250ms, then greps model.go for
// anything that could block: os.Open, os/exec, net/http, time.Sleep. This
// is the same pipeline internal/state's purity grep already runs.
func TestUpdateIsNonBlocking(t *testing.T) {
	m := newTestModel(t)
	base := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	sessions := []domain.Session{
		{Agent: "claude", ID: "a", Status: "busy"},
		{Agent: "codex", ID: "b", Status: "waiting"},
	}

	start := time.Now()
	for i := 0; i < 1000; i++ {
		var mi tea.Model
		if i%4 == 0 {
			mi, _ = m.Update(cycleMsg(base.Add(time.Duration(i)*time.Second), sessions))
		} else {
			mi, _ = m.Update(keyMsg([]string{"down", "up", "t", "enter"}[i%4]))
		}
		m = mi.(Model)
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Errorf("1000 Update calls took %v, want under 250ms", elapsed)
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	modelPath := filepath.Join(filepath.Dir(thisFile), "model.go")
	src, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("reading model.go: %v", err)
	}
	for _, banned := range []string{"os.Open", "os/exec", "net/http", "time.Sleep"} {
		if strings.Contains(string(src), banned) {
			t.Errorf("model.go contains %q -- Update must never block", banned)
		}
	}
}

// TestWindowResizeUpdatesFrameSize asserts a terminal resize reaches the
// model, so View lays out to the real terminal rather than New's 120x40
// default. Both directions are checked -- 80x24 and 200x60 are the two
// sizes manual QA resizes to -- and a degenerate 0x0 (which some
// terminals emit while a resize is in flight) must not blank the frame.
func TestWindowResizeUpdatesFrameSize(t *testing.T) {
	m := newTestModel(t)

	for _, sz := range []struct{ w, h int }{{80, 24}, {200, 60}} {
		mi, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = mi.(Model)
		if m.width != sz.w || m.height != sz.h {
			t.Errorf("after %dx%d resize: got %dx%d", sz.w, sz.h, m.width, m.height)
		}
	}

	mi, _ := m.Update(tea.WindowSizeMsg{Width: 0, Height: 0})
	m = mi.(Model)
	if m.width != 200 || m.height != 60 {
		t.Errorf("a 0x0 resize must be ignored, got %dx%d", m.width, m.height)
	}
}

// TestNoColorSuppressesEscapes asserts the --no-color answer cmd/wattop
// resolves (Task 13) actually reaches every panel: with it set, the
// rendered frame carries no ANSI escape at all.
func TestNoColorSuppressesEscapes(t *testing.T) {
	m := newTestModel(t).WithNoColor(true)
	mi, _ := m.Update(cycleMsg(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC), []domain.Session{
		{Agent: "claude", ID: "a", Status: "busy", Model: "claude-opus-5"},
	}))
	m = mi.(Model)

	for _, view := range []string{
		m.View().Content,
		func() string { mi, _ := m.Update(keyMsg("enter")); return mi.(Model).View().Content }(),
		func() string { mi, _ := m.Update(keyMsg("?")); return mi.(Model).View().Content }(),
	} {
		if strings.Contains(view, "\x1b[") {
			t.Errorf("expected no ANSI escapes under WithNoColor(true), got:\n%q", view)
		}
	}
}
