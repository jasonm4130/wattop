package panel

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/fixture"
	"github.com/jasonm4130/wattop/internal/pricing"
	"github.com/jasonm4130/wattop/internal/state"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// goldenUpdate reads the "update" flag that github.com/charmbracelet/x/exp/golden
// already registers (soc_test.go imports it in this same package/binary) --
// this package's own golden files live under testdata/golden/*.txt rather
// than that package's testdata/<Test>.golden convention, so this file
// implements its own compare-and-write helper but shares the one flag
// rather than redefining "update" a second time, which flag.Bool would
// panic on.
func goldenUpdate() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

// requireGoldenText compares got against testdata/golden/<name>.txt,
// writing it when run with -update.
func requireGoldenText(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	if goldenUpdate() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden %s (run with -update to create it): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("output does not match %s.\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

// fixtureAt is the single, fixed cycle timestamp every committed snapshot
// fixture is built from, so a regeneration always reproduces the same
// bytes given the same internal/state behaviour.
var fixtureAt = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

// loadReplayCycle reads cycle index idx (0-based) of Task 4's
// testdata/replay/sessions.json -- a JSON array of per-cycle session
// lists, each already shaped exactly like domain.Session's own JSON tags.
func loadReplayCycle(t *testing.T, idx int) []domain.Session {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixture.CorpusDir(), "replay", "sessions.json"))
	if err != nil {
		t.Fatalf("reading replay sessions fixture: %v", err)
	}
	var cycles [][]domain.Session
	if err := json.Unmarshal(raw, &cycles); err != nil {
		t.Fatalf("unmarshal replay sessions fixture: %v", err)
	}
	if idx >= len(cycles) {
		t.Fatalf("replay sessions fixture has %d cycles, want at least %d", len(cycles), idx+1)
	}
	return cycles[idx]
}

// procsFromSessions extracts the ProcSample rows embedded in a replay
// cycle's sessions, deduplicated by pid, so they can be fed back in as
// Inputs.Procs -- Reduce joins a session to its process by pid through the
// Procs list, not through whatever Proc the raw session already carries.
func procsFromSessions(sessions []domain.Session) []domain.ProcSample {
	byPID := make(map[int]domain.ProcSample)
	for _, s := range sessions {
		if s.Proc != nil {
			byPID[s.Proc.PID] = *s.Proc
		}
	}
	procs := make([]domain.ProcSample, 0, len(byPID))
	for _, p := range byPID {
		procs = append(procs, p)
	}
	return procs
}

// TestGenerateSnapshotFixture (re)builds testdata/snapshots/cycle0.json:
// one state.Inputs at the fixed fixtureAt, sourced from Task 4's
// testdata/replay/sessions.json cycle 0, run through exactly one
// state.New(book, burn).Reduce(in) call against the real embedded pricing
// table, then committed. Run with
// `go test ./internal/ui/panel/ -run TestGenerateSnapshotFixture -update`
// to regenerate after a change to internal/state's enrichment or pricing
// behaviour; every other run re-derives the same Inputs and asserts the
// committed file still matches byte-for-byte, so a silent Reduce change is
// caught here instead of surfacing as an unexplained diff in a golden test
// three files away.
func TestGenerateSnapshotFixture(t *testing.T) {
	sessions := loadReplayCycle(t, 0)
	in := state.Inputs{
		At:       fixtureAt,
		Sys:      domain.SysSample{},
		Procs:    procsFromSessions(sessions),
		Sessions: sessions,
	}

	book, err := pricing.Load()
	if err != nil {
		t.Fatalf("pricing.Load: %v", err)
	}
	st := state.New(book, pricing.NewBurnTracker(5*time.Minute, 0.3))
	snap := st.Reduce(in)

	got, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", "snapshots", "cycle0.json")
	if goldenUpdate() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write snapshot fixture: %v", err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading committed snapshot fixture %s (run with -update to create it): %v", path, err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("testdata/snapshots/cycle0.json is stale -- rerun with -update if this is intentional")
	}
}

// loadSnapshotFixture reads the committed cycle0.json Snapshot every other
// test in this file renders from.
func loadSnapshotFixture(t *testing.T) *domain.Snapshot {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "snapshots", "cycle0.json"))
	if err != nil {
		t.Fatalf("reading snapshot fixture (run TestGenerateSnapshotFixture -update first): %v", err)
	}
	var snap domain.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("unmarshal snapshot fixture: %v", err)
	}
	return &snap
}

func loadDarkRoles(t *testing.T) theme.Roles {
	t.Helper()
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}
	return r
}

// TestSessionsGolden renders the whole cycle0 fixture -- a busy Claude
// session, a waiting Codex session, an unbound rollout, an unpriced model
// and a three-subagent session with one live child -- at a fixed 120x40
// and golden-compares it. Run with -update to regenerate.
func TestSessionsGolden(t *testing.T) {
	snap := loadSnapshotFixture(t)
	r := loadDarkRoles(t)

	out := SessionsRender(snap.Sessions, r, 120, 40, -1, snap.At, Options{})
	requireGoldenText(t, "sessions_basic", out)
}

// TestClaudeApproxMarkerCodexExact renders the fixture's Claude row (busy,
// ContextExact == false) and Codex row (waiting, ContextExact == true) side
// by side and asserts the "~" approximation marker appears on exactly the
// Claude row.
func TestClaudeApproxMarkerCodexExact(t *testing.T) {
	snap := loadSnapshotFixture(t)
	r := loadDarkRoles(t)

	var claudeRow, codexRow domain.Session
	for _, s := range snap.Sessions {
		switch s.ID {
		case "sess-busy-claude":
			claudeRow = s
		case "sess-waiting-codex":
			codexRow = s
		}
	}
	if claudeRow.ID == "" || codexRow.ID == "" {
		t.Fatalf("fixture missing expected sessions; have %d sessions", len(snap.Sessions))
	}

	out := SessionsRender([]domain.Session{claudeRow, codexRow}, r, 120, 6, -1, snap.At, Options{})

	if got := strings.Count(out, "~"); got != 1 {
		t.Errorf("expected exactly one \"~\" approximation marker across a Claude+Codex pair, got %d in:\n%s", got, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 3 || !strings.Contains(lines[1], "~") || strings.Contains(lines[2], "~") {
		t.Errorf("expected the \"~\" marker on the Claude row (line 2) and not the Codex row (line 3):\n%s", out)
	}
}

// TestUnpricedRendersDash asserts the fixture's unpriced-model session
// renders its cost cell as "$—", never "$0.00".
func TestUnpricedRendersDash(t *testing.T) {
	snap := loadSnapshotFixture(t)
	r := loadDarkRoles(t)

	var unpriced domain.Session
	for _, s := range snap.Sessions {
		if s.ID == "sess-unpriced-model" {
			unpriced = s
		}
	}
	if unpriced.ID == "" {
		t.Fatal("fixture missing sess-unpriced-model")
	}
	if unpriced.Priced {
		t.Fatalf("fixture session sess-unpriced-model unexpectedly priced by the real table -- regenerate the fixture or pick another unpriced model")
	}

	out := SessionsRender([]domain.Session{unpriced}, r, 120, 3, -1, snap.At, Options{})
	if !strings.Contains(out, "$—") {
		t.Errorf("expected \"$—\" for an unpriced model, got:\n%s", out)
	}
	if strings.Contains(out, "$0.00") {
		t.Errorf("unpriced model rendered as \"$0.00\" rather than a dash:\n%s", out)
	}
}

// TestUnboundPidRendersDashes pins the spec's exact rule: the "(pid
// unknown)" cell and dashed CPU/GPU/RSS columns are keyed on
// Session.BindConf == "unknown", never on Proc == nil. A session can in
// principle carry BindConf == "unknown" with a populated Proc (a stale
// binding whose process happens to still resolve), and this must still
// render as unbound -- so this test builds that case directly rather than
// relying on the fixture's coincidence that its unbound row also has a nil
// Proc.
func TestUnboundPidRendersDashes(t *testing.T) {
	r := loadDarkRoles(t)
	gpu := 40.0
	s := domain.Session{
		Agent:    "codex",
		ID:       "sess-stale-binding",
		BindConf: "unknown",
		Status:   "unknown",
		Proc: &domain.ProcSample{
			PID:         999,
			CPUPct:      55.5,
			RSSBytes:    100_000_000,
			GPUMsPerSec: &gpu,
		},
	}

	out := SessionsRender([]domain.Session{s}, r, 120, 3, -1, time.Time{}, Options{})

	if !strings.Contains(out, unknownPID) {
		t.Errorf("expected %q in the render, got:\n%s", unknownPID, out)
	}
	if strings.Contains(out, "55.5%") || strings.Contains(out, "40.0") || strings.Contains(out, "95M") {
		t.Errorf("BindConf==unknown row rendered live Proc data instead of dashes:\n%s", out)
	}

	// The fixture's own unbound row (Proc == nil too) still renders the
	// same way, as a sanity check that the two paths agree.
	fixtureRow := domain.Session{Agent: "codex", ID: "sess-unbound-rollout", BindConf: "unknown", Status: "unknown"}
	fixtureOut := SessionsRender([]domain.Session{fixtureRow}, r, 120, 3, -1, time.Time{}, Options{})
	if !strings.Contains(fixtureOut, unknownPID) {
		t.Errorf("expected %q for a nil-Proc unbound row too, got:\n%s", unknownPID, fixtureOut)
	}
}

// TestAllFiveStatusesRender builds one synthetic session per Status value
// the spec names (the committed fixture has no rate-limited row) plus a
// sixth, unrecognised status, and asserts each renders a distinct,
// non-blank label -- an unrecognised status must fall through to Muted and
// the literal string, never a blank cell.
func TestAllFiveStatusesRender(t *testing.T) {
	r := loadDarkRoles(t)
	since := fixtureAt.Add(-90 * time.Second)

	statuses := []struct {
		status string
		want   string
	}{
		{"busy", "Busy"},
		{"waiting", "Waiting"},
		{"rate-limited", "RateLimited"},
		{"unknown", "Unknown"},
		{"stale", "Stale"},
		{"totally-made-up", "totally-made-up"},
	}

	for _, tc := range statuses {
		t.Run(tc.status, func(t *testing.T) {
			s := domain.Session{Agent: "claude", ID: "s-" + tc.status, Status: tc.status, StatusSince: since}
			out := SessionsRender([]domain.Session{s}, r, 120, 3, -1, fixtureAt, Options{})
			if !strings.Contains(out, tc.want) {
				t.Errorf("status %q: expected %q in render, got:\n%s", tc.status, tc.want, out)
			}
		})
	}
}

// TestBackgroundSessionStyledDifferently asserts a non-interactive
// (background) session's row differs from an otherwise-identical
// interactive one -- a Nightshift run burning $9/hr must not look like the
// session a person is typing into.
func TestBackgroundSessionStyledDifferently(t *testing.T) {
	r := loadDarkRoles(t)
	mk := func(kind string) domain.Session {
		return domain.Session{Agent: "claude", ID: "s1", Kind: kind, Status: "busy", Model: "claude-opus-5"}
	}
	interactive := SessionsRender([]domain.Session{mk("interactive")}, r, 120, 3, -1, fixtureAt, Options{})
	background := SessionsRender([]domain.Session{mk("headless")}, r, 120, 3, -1, fixtureAt, Options{})
	if interactive == background {
		t.Error("a background (non-interactive) session rendered identically to an interactive one")
	}
}

// TestFilterHeadlessHidesSubagentRows exercises the subagent-tree render
// path (a three-subagent session, one live) and confirms the child rows
// disappear once callers filter them out, mirroring model.go's 'f'
// keybinding.
func TestSubagentRowsRenderIndented(t *testing.T) {
	snap := loadSnapshotFixture(t)
	r := loadDarkRoles(t)

	var withSubagents domain.Session
	for _, s := range snap.Sessions {
		if s.ID == "sess-three-subagents" {
			withSubagents = s
		}
	}
	if len(withSubagents.Subagents) != 3 {
		t.Fatalf("expected 3 subagents in fixture, got %d", len(withSubagents.Subagents))
	}

	out := SessionsRender([]domain.Session{withSubagents}, r, 120, 5, -1, snap.At, Options{})
	if !strings.Contains(out, "live") {
		t.Errorf("expected the live subagent's state to render, got:\n%s", out)
	}
	if !strings.Contains(out, "finished") {
		t.Errorf("expected a finished subagent's state to render, got:\n%s", out)
	}

	filtered := withSubagents
	filtered.Subagents = nil
	filteredOut := SessionsRender([]domain.Session{filtered}, r, 120, 5, -1, snap.At, Options{})
	if strings.Contains(filteredOut, "live") || strings.Contains(filteredOut, "finished") {
		t.Errorf("filtering subagents out should drop their rows entirely, got:\n%s", filteredOut)
	}
}
