package panel

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

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

var fixtureAt = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

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

func TestSessionsGolden(t *testing.T) {
	snap := loadSnapshotFixture(t)
	r := loadDarkRoles(t)

	out := SessionsRender(snap.Sessions, r, 120, 40, -1, snap.At, Options{})
	requireGoldenText(t, "sessions_basic", out)
}

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

func TestUnboundPidRendersDashes(t *testing.T) {
	r := loadDarkRoles(t)
	gpu := 40.0
	s := domain.Session{
		Agent:    "codex",
		ID:       "sess-stale-binding",
		BindConf: domain.BindUnknown,
		Status:   "unknown",
		Proc: &domain.ProcSample{
			PID:         999,
			CPUPct:      floatPtr(55.5),
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

	fixtureRow := domain.Session{Agent: "codex", ID: "sess-unbound-rollout", BindConf: domain.BindUnknown, Status: "unknown"}
	fixtureOut := SessionsRender([]domain.Session{fixtureRow}, r, 120, 3, -1, time.Time{}, Options{})
	if !strings.Contains(fixtureOut, unknownPID) {
		t.Errorf("expected %q for a nil-Proc unbound row too, got:\n%s", unknownPID, fixtureOut)
	}
}

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

func TestSelectedRowHighlightSpansWholeLine(t *testing.T) {
	r := loadDarkRoles(t)
	s := domain.Session{Agent: "claude", ID: "s1", Status: "busy", Model: "claude-opus-5", CWD: "/home/u/project"}

	out := SessionsRender([]domain.Session{s}, r, 160, 3, 0, fixtureAt, Options{})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least a header and one data line, got %d:\n%s", len(lines), out)
	}
	row := lines[1]

	if got := strings.Count(row, "\x1b["); got != 2 {
		t.Fatalf("expected exactly one open+close ANSI pair (the whole-line selection style) on the selected row, got %d escape sequences:\n%q", got, row)
	}
	if !strings.HasPrefix(row, "\x1b[") || !strings.Contains(row, "48;2;") || !strings.Contains(row, "m"+selectionGutter) {
		t.Errorf("expected the selection SGR to open immediately before the gutter, got:\n%q", row)
	}
	if !strings.HasSuffix(row, "\x1b[m") && !strings.HasSuffix(row, "\x1b[0m") {
		t.Errorf("expected the selection wrap to close only at the very end of the row, got:\n%q", row)
	}
}

func TestSelectionGutterSurvivesNoColor(t *testing.T) {
	r := loadDarkRoles(t)
	sessions := []domain.Session{
		{Agent: "claude", ID: "s1", Status: "busy"},
		{Agent: "codex", ID: "s2", Status: "waiting"},
	}

	out := SessionsRender(sessions, r, 120, 4, 1, fixtureAt, Options{NoColor: true})
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NoColor render still contains an ANSI escape sequence:\n%q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected a header plus 2 data lines, got %d:\n%s", len(lines), out)
	}
	if strings.Contains(lines[1], selectionGutter) {
		t.Errorf("unselected row 0 unexpectedly carries the selection gutter:\n%q", lines[1])
	}
	if !strings.Contains(lines[2], selectionGutter) {
		t.Errorf("selected row 1 missing the %q gutter under NoColor:\n%q", selectionGutter, lines[2])
	}
}

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
	if !strings.Contains(out, "● run") {
		t.Errorf("expected the running subagent's state to render, got:\n%s", out)
	}
	if !strings.Contains(out, "done") {
		t.Errorf("expected a finished subagent's state to render, got:\n%s", out)
	}

	filtered := withSubagents
	filtered.Subagents = nil
	filteredOut := SessionsRender([]domain.Session{filtered}, r, 120, 5, -1, snap.At, Options{})
	if strings.Contains(filteredOut, "● run") || strings.Contains(filteredOut, "done") {
		t.Errorf("filtering subagents out should drop their rows entirely, got:\n%s", filteredOut)
	}
}

func TestHumanCountScalesToMAndG(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1.0k"},
		{1_431_200, "1.4M"},
		{286_059_500, "286.1M"},
		{1_000_000_000, "1.0G"},
		{-1500, "-1.5k"},
	}
	for _, c := range cases {
		if got := humanCount(c.n); got != c.want {
			t.Errorf("humanCount(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestHumanBytesScalesToMAndG(t *testing.T) {
	cases := []struct {
		n    uint64
		want string
	}{
		{0, "0B"},
		{999, "999B"},
		{1000, "1.0KB"},
		{580_444_160, "580.4MB"},
		{1_000_000_000, "1.0GB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.n); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func manySessions(n int) []domain.Session {
	out := make([]domain.Session, n)
	for i := range out {
		out[i] = domain.Session{Agent: "claude", ID: fmt.Sprintf("s%d", i), Status: fmt.Sprintf("row%d", i)}
	}
	return out
}

func TestSessionsScrollKeepsSelectionOnScreen(t *testing.T) {
	r := loadDarkRoles(t)
	sessions := manySessions(30)

	out := SessionsRender(sessions, r, 120, 18, 25, time.Time{}, Options{})

	var selectedLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "row25") {
			selectedLine = line
			break
		}
	}
	if selectedLine == "" {
		t.Fatalf("selected row 25 scrolled out of a 17-row window entirely, got:\n%s", out)
	}
	if !strings.HasPrefix(selectedLine, "\x1b[") || !strings.Contains(selectedLine, "48;2;") {
		t.Errorf("selected row 25 rendered but lost its selection highlight, got line:\n%q", selectedLine)
	}
}

func TestSessionsScrollShowsMoreMarkers(t *testing.T) {
	r := loadDarkRoles(t)
	sessions := manySessions(30)

	out := SessionsRender(sessions, r, 120, 18, 15, time.Time{}, Options{})
	if !strings.Contains(out, "▲") {
		t.Errorf("expected a \"▲ N more\" marker for rows hidden above the window, got:\n%s", out)
	}
	if !strings.Contains(out, "▼") {
		t.Errorf("expected a \"▼ N more\" marker for rows hidden below the window, got:\n%s", out)
	}
}

func TestSessionsScrollNoMarkersWhenEverythingFits(t *testing.T) {
	r := loadDarkRoles(t)
	sessions := manySessions(5)

	out := SessionsRender(sessions, r, 120, 18, 2, time.Time{}, Options{})
	if strings.Contains(out, "▲") || strings.Contains(out, "▼") {
		t.Errorf("did not expect a scroll marker when all rows fit the frame, got:\n%s", out)
	}
	for i := 0; i < 5; i++ {
		if want := fmt.Sprintf("row%d", i); !strings.Contains(out, want) {
			t.Errorf("expected %q to render when every row fits, got:\n%s", want, out)
		}
	}
}

func TestWindowRowsNeverHidesTheSelectedRowBehindAMarker(t *testing.T) {
	const n = 10
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("row%d", i)
	}

	for visible := 1; visible <= n; visible++ {
		for selected := 0; selected < n; selected++ {
			got := windowRows(rows, visible, selected)
			want := fmt.Sprintf("row%d", selected)
			found := false
			for _, line := range got {
				if line == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("visible=%d selected=%d: selection missing or hidden behind a marker in %v", visible, selected, got)
			}
		}
	}
}

func TestCtxGaugeOverBudgetRendersLiteralOverflow(t *testing.T) {
	r := loadDarkRoles(t)

	within := domain.Session{ContextUsed: 61000, ContextMax: 200000, ContextExact: true}
	if got := ctxGauge(r, within, Options{NoColor: true}, wCTX); !strings.Contains(got, " 30%") {
		t.Errorf("within-budget gauge = %q, want it to contain \" 30%%\"", got)
	}

	over := domain.Session{ContextUsed: 946000, ContextMax: 200000, ContextExact: true}
	got := ctxGauge(r, over, Options{NoColor: true}, wCTX)
	if !strings.Contains(got, ">100%") {
		t.Errorf("over-budget gauge = %q, want it to contain the literal \">100%%\"", got)
	}
	if strings.Contains(got, "473") {
		t.Errorf("over-budget gauge = %q, must not render the raw unclamped percentage", got)
	}
}

func TestComputeSessionColsNeverOverflowsWidth(t *testing.T) {
	for _, w := range []int{80, 100, 120, 140, 160, 200, 300} {
		c := computeSessionCols(w)
		fields := []int{c.Status, c.PID, c.Agent, c.Model, c.CWD, c.Ctx, c.Rate, c.Cost, c.Burn, c.CPU, c.RSS}
		n := len(fields)
		if c.ShowTL {
			fields = append(fields, 3)
			n++
		}
		if c.ShowSA {
			fields = append(fields, wSA)
			n++
		}
		if c.ShowGPU {
			fields = append(fields, 6)
			n++
		}
		sum := 2
		for _, f := range fields {
			sum += f
		}
		sum += n - 1
		if sum > w {
			t.Errorf("computeSessionCols(%d) = %+v sums to %d columns, want at most %d", w, c, sum, w)
		}
	}
}

func TestSessionsNarrow80ColsShowsCostAndCPU(t *testing.T) {
	snap := loadSnapshotFixture(t)
	r := loadDarkRoles(t)

	out := SessionsRender(snap.Sessions, r, 80, len(snap.Sessions)+2, -1, snap.At, Options{})
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("line %d is %d cells wide, want at most 80: %q", i, w, line)
		}
	}
	if !strings.Contains(out, "$0.30") {
		t.Errorf("expected the fixture's busy session cost ($0.30) to survive at 80 cols, got:\n%s", out)
	}
	if !strings.Contains(out, "12.5%") {
		t.Errorf("expected the fixture's busy session CPU%% (12.5%%) to survive at 80 cols, got:\n%s", out)
	}
}

func TestSessionsWide200ColsWidensCWDAndModel(t *testing.T) {
	cols200 := computeSessionCols(200)
	cols120 := computeSessionCols(120)
	if cols200.CWD <= cols120.CWD {
		t.Errorf("CWD did not widen at 200 cols: got %d at 200, %d at 120", cols200.CWD, cols120.CWD)
	}
	if cols200.Model <= cols120.Model {
		t.Errorf("MODEL did not widen at 200 cols: got %d at 200, %d at 120", cols200.Model, cols120.Model)
	}

	snap := loadSnapshotFixture(t)
	r := loadDarkRoles(t)
	out := SessionsRender(snap.Sessions, r, 200, len(snap.Sessions)+2, -1, snap.At, Options{})
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w != 200 {
			t.Errorf("line %d is %d cells wide, want exactly 200 (frame() pads short lines, so anything else is a computeSessionCols bug): %q", i, w, line)
		}
	}
}

func TestUnknownModelRendersDash(t *testing.T) {
	r := loadDarkRoles(t)
	out := SessionsRender([]domain.Session{
		{Agent: "claude", ID: "sess-1", Status: "busy", BindConf: domain.BindExact, CWD: "/repo/x"},
	}, r, 120, 3, -1, time.Time{}, Options{NoColor: true})

	row := strings.Split(out, "\n")[1]
	if !strings.Contains(row, "—") {
		t.Errorf("a session with no model must render a dash in MODEL, got:\n%s", row)
	}
}
