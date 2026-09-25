package panel

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/jasonm4130/wattop/internal/domain"
)

// childrenFixturePath is the hand-built child-tracking snapshot. It is not
// derived from the replay corpus through Reduce (as cycle0.json is): it
// pins every child shape the table and detail panel must render -- a
// nested child, a background child, idle and failed children, an unpriced
// child, a workflow with its agents, a partial-cost session and a
// partial-cost snapshot total -- independent of any source or reducer.
var childrenFixturePath = filepath.Join("testdata", "snapshots", "children.json")

// buildChildrenFixture is the source of children.json. Tree order in
// Subagents: a1, its nested child a2, then a3..a6, with the workflow's
// agents after them.
func buildChildrenFixture() *domain.Snapshot {
	at := fixtureAt
	ago := func(d time.Duration) time.Time { return at.Add(-d) }
	f := floatPtr
	pid := 4321
	const wf = "wf_7f3a9c21e5b4"

	sessions := []domain.Session{
		{
			Agent: "claude", ID: "sess-children", PID: &pid, BindConf: "exact",
			CWD: "/home/u/tree", Name: "tree", Kind: "interactive", Status: "busy",
			StatusSince: ago(3 * time.Minute), Model: "claude-opus-5",
			Usage:   domain.Usage{Input: 120000, Output: 9000, CacheRead: 800000},
			CostUSD: f(12.34), BurnUSDPerHr: f(8.5), Priced: true, CostPartial: true,
			ContextUsed: 90000, ContextMax: 200000,
			TokenRate:   &domain.TokenRate{InputPerSec: 900, OutputPerSec: 42.5},
			LastUsageAt: ago(5 * time.Second),
			Subagents: []domain.Subagent{
				{
					ID: "agent-a1", AgentType: "Explore", Description: "map the repo layout",
					Model: "claude-fable-5-1", Status: domain.SubagentRunning, Live: true,
					StartedAt: ago(10 * time.Minute), LastActivityAt: ago(10 * time.Second),
					CurrentTool: "Grep", ToolCalls: 14, CostUSD: f(0.42), BurnUSDPerHr: f(1.8),
					TokenRate: &domain.TokenRate{OutputPerSec: 12.0},
				},
				{
					ID: "agent-a2", ParentID: "agent-a1", SpawnDepth: 2, AgentType: "worker",
					Description: "patch the parser", Model: "claude-sonnet-5",
					Status: domain.SubagentRunning, Live: true,
					StartedAt: ago(4 * time.Minute), LastActivityAt: ago(2 * time.Second),
					CurrentTool: "Edit", ToolCalls: 6, CostUSD: f(0.10), BurnUSDPerHr: f(0.6),
				},
				{
					ID: "agent-a3", AgentType: "general-purpose", Description: "watch the build",
					Model: "claude-sonnet-5", Background: true, Status: domain.SubagentIdle,
					StartedAt: ago(20 * time.Minute), LastActivityAt: ago(6 * time.Minute),
					ToolCalls: 3, CostUSD: f(0.05), BurnUSDPerHr: f(0),
				},
				{
					ID: "agent-a4", AgentType: "worker", Description: "flaky migration",
					Model: "claude-sonnet-5", Status: domain.SubagentFailed,
					StartedAt: ago(30 * time.Minute), LastActivityAt: ago(25 * time.Minute),
					ToolCalls: 9, CostUSD: f(0.20),
				},
				{
					ID: "agent-a5", AgentType: "Explore", Description: "search the docs",
					Model: "claude-fable-5-1", Status: domain.SubagentDone,
					StartedAt: ago(40 * time.Minute), LastActivityAt: ago(38 * time.Minute),
					ToolCalls: 4, CostUSD: f(0.12),
				},
				{
					ID: "agent-a6", AgentType: "worker", Description: "try the nightly model",
					Model: "claude-nightly-experimental", Status: domain.SubagentDone,
					StartedAt: ago(35 * time.Minute), LastActivityAt: ago(33 * time.Minute),
					ToolCalls: 2,
				},
				{
					ID: "agent-w1", WorkflowID: wf, Phase: "review", AgentType: "workflow-agent",
					Description: "review api layer", Model: "claude-sonnet-5",
					Status: domain.SubagentDone, StartedAt: ago(15 * time.Minute),
					LastActivityAt: ago(12 * time.Minute), ToolCalls: 11, CostUSD: f(0.80),
				},
				{
					ID: "agent-w2", WorkflowID: wf, Phase: "review", AgentType: "workflow-agent",
					Description: "review storage layer", Model: "claude-sonnet-5",
					Status: domain.SubagentDone, StartedAt: ago(15 * time.Minute),
					LastActivityAt: ago(11 * time.Minute), ToolCalls: 8, CostUSD: f(0.70),
				},
				{
					ID: "agent-w3", WorkflowID: wf, Phase: "review", AgentType: "workflow-agent",
					Description: "review ui layer", Model: "claude-nightly-experimental",
					Status: domain.SubagentFailed, StartedAt: ago(15 * time.Minute),
					LastActivityAt: ago(13 * time.Minute), ToolCalls: 1,
				},
				{
					ID: "agent-w4", WorkflowID: wf, Phase: "implement", AgentType: "workflow-agent",
					Description: "implement fixes", Model: "claude-opus-5",
					Status: domain.SubagentRunning, Live: true, StartedAt: ago(9 * time.Minute),
					LastActivityAt: ago(3 * time.Second), CurrentTool: "Bash", ToolCalls: 21,
					CostUSD: f(1.21), BurnUSDPerHr: f(3.1),
				},
				{
					ID: "agent-w5", WorkflowID: wf, Phase: "implement", AgentType: "workflow-agent",
					Description: "write tests", Model: "claude-sonnet-5",
					Status: domain.SubagentRunning, Live: true, StartedAt: ago(8 * time.Minute),
					LastActivityAt: ago(20 * time.Second), ToolCalls: 7,
					CostUSD: f(0.50), BurnUSDPerHr: f(1.3),
				},
			},
			Workflows: []domain.Workflow{{
				ID: wf, Status: domain.SubagentRunning, Phase: "implement",
				Agents: 5, Running: 2, Done: 2, Failed: 1,
				StartedAt: ago(15 * time.Minute), LastActivityAt: ago(3 * time.Second),
				CostUSD: f(3.21), CostPartial: true, BurnUSDPerHr: f(4.4),
				TokenRate: &domain.TokenRate{OutputPerSec: 30.2},
			}},
		},
		{
			Agent: "codex", ID: "sess-plain", BindConf: "unknown", CWD: "/home/u/repo",
			Kind: "interactive", Status: "waiting", Model: "gpt-5.6-terra",
			CostUSD: f(0.02), BurnUSDPerHr: f(0), Priced: true, ContextExact: true,
			ContextUsed: 10000, ContextMax: 272000,
		},
	}
	return &domain.Snapshot{
		At:                at,
		Sessions:          sessions,
		TotalCostUSD:      12.36,
		TotalCostPartial:  true,
		TotalBurnUSDPerHr: 8.5,
		UnpricedModels:    []string{"claude-nightly-experimental"},
	}
}

// TestGenerateChildrenFixture writes children.json with -update and
// otherwise asserts the committed file still matches buildChildrenFixture.
func TestGenerateChildrenFixture(t *testing.T) {
	got, err := json.MarshalIndent(buildChildrenFixture(), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got = append(got, '\n')
	if goldenUpdate() {
		if err := os.WriteFile(childrenFixturePath, got, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	want, err := os.ReadFile(childrenFixturePath)
	if err != nil {
		t.Fatalf("reading %s (run with -update to create it): %v", childrenFixturePath, err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("%s is stale -- rerun with -update if this is intentional", childrenFixturePath)
	}
}

func loadChildrenFixture(t *testing.T) *domain.Snapshot {
	t.Helper()
	raw, err := os.ReadFile(childrenFixturePath)
	if err != nil {
		t.Fatalf("reading %s: %v", childrenFixturePath, err)
	}
	var snap domain.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &snap
}

func TestChildRowsOrderAndDepth(t *testing.T) {
	s := loadChildrenFixture(t).Sessions[0]
	rows := ChildRows(s)

	type want struct {
		id    string
		depth int
		wf    bool
	}
	wants := []want{
		{"agent-a1", 0, false}, {"agent-a2", 1, false}, {"agent-a3", 0, false},
		{"agent-a4", 0, false}, {"agent-a5", 0, false}, {"agent-a6", 0, false},
		{"wf_7f3a9c21e5b4", 0, true},
	}
	if len(rows) != len(wants) {
		t.Fatalf("ChildRows returned %d rows, want %d", len(rows), len(wants))
	}
	for i, w := range wants {
		r := rows[i]
		if w.wf {
			if r.Workflow == nil || r.Subagent != nil || r.Workflow.ID != w.id {
				t.Errorf("row %d = %+v, want workflow %s", i, r, w.id)
			}
			continue
		}
		if r.Subagent == nil || r.Workflow != nil || r.Subagent.ID != w.id || r.Depth != w.depth {
			t.Errorf("row %d = %+v, want subagent %s depth %d", i, r, w.id, w.depth)
		}
	}
	// Rows point into the session, not at copies.
	if rows[0].Subagent != &s.Subagents[0] || rows[6].Workflow != &s.Workflows[0] {
		t.Errorf("ChildRows must point into s.Subagents / s.Workflows")
	}
}

func TestChildRowsUnresolvableParentAndCycle(t *testing.T) {
	s := domain.Session{Subagents: []domain.Subagent{
		{ID: "x", ParentID: "missing"},
		{ID: "p", ParentID: "q"},
		{ID: "q", ParentID: "p"},
		{ID: "r", ParentID: "q"},
	}}
	rows := ChildRows(s)
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4", len(rows))
	}
	if rows[0].Depth != 0 {
		t.Errorf("unresolvable parent: depth = %d, want 0", rows[0].Depth)
	}
	// A cycle must terminate; its depth is bounded by the slice length.
	for _, r := range rows {
		if r.Depth > len(s.Subagents) {
			t.Errorf("depth %d exceeds bound for %s", r.Depth, r.Subagent.ID)
		}
	}
	if ChildRows(domain.Session{}) != nil {
		t.Errorf("a session with no children has no child rows")
	}
}

// TestSessionRowsMatchChildRows pins the row count every caller relies on:
// one parent row plus len(ChildRows) per session.
func TestSessionRowsMatchChildRows(t *testing.T) {
	r := loadDarkRoles(t)
	for _, snap := range []*domain.Snapshot{loadSnapshotFixture(t), loadChildrenFixture(t)} {
		want := 0
		for _, s := range snap.Sessions {
			want += 1 + len(ChildRows(s))
		}
		got := sessionRows(snap.Sessions, r, snap.At, Options{NoColor: true}, -1, computeSessionCols(120))
		if len(got) != want {
			t.Errorf("sessionRows = %d rows, want %d", len(got), want)
		}
	}
}

func TestChildRowsRenderInTable(t *testing.T) {
	snap := loadChildrenFixture(t)
	r := loadDarkRoles(t)
	// 200 columns gives CWD its full +40 bonus, room for the whole
	// workflow summary.
	out := SessionsRender(snap.Sessions, r, 200, 30, -1, snap.At, Options{NoColor: true})

	for _, want := range []string{
		"● run", "idle", "done", "fail", // every child status
		"▸ Grep · map the repo", // current tool on a running child
		"gene…⇢",                // background marker, cut to the agent column
		"wf ● run", "wf_7f3a9c2 · implement · 2 run 2/5 done 1 fail",
		"~$3.21",  // partial workflow cost
		"~$12.34", // partial session cost
		"4/11",    // SA: running/total over every subagent, workflow agents included
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in table, got:\n%s", want, out)
		}
	}
	// Workflow agents never get rows of their own.
	if strings.Contains(out, "review api layer") {
		t.Errorf("workflow agent rendered as its own row:\n%s", out)
	}
	// The nested child is indented two more columns than its parent.
	lines := strings.Split(out, "\n")
	indentOf := func(sub string) int {
		for _, l := range lines {
			if strings.Contains(l, sub) {
				return len(l) - len(strings.TrimLeft(l, " "))
			}
		}
		t.Fatalf("%q not found", sub)
		return 0
	}
	if a1, a2 := indentOf("map the repo"), indentOf("patch the parser"); a2 != a1+2 {
		t.Errorf("nested child indent = %d, parent = %d, want parent+2", a2, a1)
	}
}

// TestChildrenFitWidth renders the table, detail and footer for the child
// fixture at 80, 120 and 160 columns and asserts no line is wider than the
// frame.
func TestChildrenFitWidth(t *testing.T) {
	snap := loadChildrenFixture(t)
	r := loadDarkRoles(t)
	for _, w := range []int{80, 120, 160} {
		for _, noColor := range []bool{false, true} {
			opts := Options{NoColor: noColor}
			renders := map[string]string{
				"sessions": SessionsRender(snap.Sessions, r, w, 30, 2, snap.At, opts),
				"detail":   DetailRender(snap.Sessions[0], r, w, 60, snap.At, opts),
				"footer":   FooterRender(snap, r, w, 4, "status", "wattop-dark", false, 0, opts),
			}
			for name, out := range renders {
				lines := strings.Split(out, "\n")
				if name == "detail" {
					// Only the child tree is new here. The detail view's
					// pre-existing lines (the token line) still overrun 80
					// columns: a known defect pinned by e2e's
					// TestDetailViewOverflowsAt80Columns.
					lines = childTreeSection(lines)
				}
				for i, line := range lines {
					if lw := lipgloss.Width(line); lw > w {
						t.Errorf("%s at %d cols (noColor=%v): line %d is %d wide:\n%s", name, w, noColor, i, lw, line)
					}
				}
			}
		}
	}
}

func TestPartialCostMarkers(t *testing.T) {
	snap := loadChildrenFixture(t)
	r := loadDarkRoles(t)
	opts := Options{NoColor: true}

	footer := FooterRender(snap, r, 160, 4, "status", "wattop-dark", false, 0, opts)
	if !strings.Contains(footer, "~$12.36 session total") {
		t.Errorf("expected a ~ on the partial footer total, got:\n%s", footer)
	}
	snap.TotalCostPartial = false
	footer = FooterRender(snap, r, 160, 4, "status", "wattop-dark", false, 0, opts)
	if strings.Contains(footer, "~$") {
		t.Errorf("a complete total must carry no ~, got:\n%s", footer)
	}

	table := SessionsRender(snap.Sessions, r, 120, 30, -1, snap.At, opts)
	if !strings.Contains(table, "~$12.34") {
		t.Errorf("expected a ~ on the partial session cost, got:\n%s", table)
	}
	if strings.Contains(table, "~$0.02") {
		t.Errorf("a complete session cost must carry no ~, got:\n%s", table)
	}
}

func TestChildrenNoColorHasNoANSI(t *testing.T) {
	snap := loadChildrenFixture(t)
	r := loadDarkRoles(t)
	opts := Options{NoColor: true}
	for name, out := range map[string]string{
		"sessions": SessionsRender(snap.Sessions, r, 120, 30, 3, snap.At, opts),
		"detail":   DetailRender(snap.Sessions[0], r, 120, 40, snap.At, opts),
		"footer":   FooterRender(snap, r, 120, 4, "status", "wattop-dark", false, 0, opts),
	} {
		if strings.Contains(out, "\x1b[") {
			t.Errorf("%s: NoColor output carries an ANSI escape:\n%s", name, out)
		}
	}
	colored := DetailRender(snap.Sessions[0], r, 120, 40, snap.At, Options{})
	if !strings.Contains(colored, "\x1b[") {
		t.Errorf("colored detail should carry ANSI styling")
	}
}

func TestDetailChildTree(t *testing.T) {
	snap := loadChildrenFixture(t)
	r := loadDarkRoles(t)
	out := DetailRender(snap.Sessions[0], r, 160, 60, snap.At, Options{NoColor: true})

	for _, want := range []string{
		"Subagents (11 · 4 running)",
		"├─ ● run Explore",
		"│  └─ ● run worker", // nested under a1 with a continuation bar
		"general-pu…⇢",
		"└─ done  worker", // last root
		"▸ Grep",
		"10m", // a1 elapsed: now - StartedAt while running
		"$0.42 $1.80/h",
		"Workflow wf_7f3a9c21e5b4  ● run  phase implement  2 run 2/5 done 1 fail  ~$3.21 $4.40/h",
		"    ├─ done  review",
		"    └─ ● run implement",
		"review api layer",
		"~$12.34",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in detail, got:\n%s", want, out)
		}
	}
}

// TestChildrenGolden pins the NoColor table at 120 columns and the detail
// panel for the child fixture. Run with -update to regenerate.
func TestChildrenGolden(t *testing.T) {
	snap := loadChildrenFixture(t)
	r := loadDarkRoles(t)
	opts := Options{NoColor: true}
	requireGoldenText(t, "sessions_children", SessionsRender(snap.Sessions, r, 120, 16, -1, snap.At, opts))
	requireGoldenText(t, "detail_children", DetailRender(snap.Sessions[0], r, 120, 40, snap.At, opts))
}

// childTreeSection returns the detail lines from the "Subagents" header up
// to the next blank line.
func childTreeSection(lines []string) []string {
	start := -1
	for i, l := range lines {
		if start < 0 && strings.Contains(l, "Subagents (") {
			start = i
			continue
		}
		if start >= 0 && strings.TrimSpace(l) == "" {
			return lines[start:i]
		}
	}
	if start < 0 {
		return nil
	}
	return lines[start:]
}
