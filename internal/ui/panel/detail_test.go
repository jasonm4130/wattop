package panel

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// sessionWithHistogram builds a synthetic session carrying a 12-entry
// tool-call histogram, a bind confidence, cache-tier tokens and a Codex
// rate limit -- one place to build all the detail-view inputs so the
// golden test and the targeted assertions below don't drift apart.
func sessionWithHistogram() domain.Session {
	counts := map[string]int{}
	names := []string{
		"Read", "Edit", "Write", "Bash", "Grep", "Glob",
		"Task", "WebFetch", "WebSearch", "NotebookEdit", "TodoWrite", "MultiEdit",
	}
	var tools []domain.ToolCall
	base := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	for i, n := range names {
		counts[n] = i + 1
		tools = append(tools, domain.ToolCall{Name: n, At: base.Add(time.Duration(i) * time.Minute), ID: n + "-1"})
	}

	cost := 4.2
	burn := 6.15
	rejectedResets := base.Add(5 * time.Hour)

	return domain.Session{
		Agent:        "claude",
		ID:           "sess-detail",
		BindConf:     "exact",
		CWD:          "/home/u/project",
		Model:        "claude-opus-5",
		Priced:       true,
		CostUSD:      &cost,
		BurnUSDPerHr: &burn,
		Usage: domain.Usage{
			Input: 42000, Output: 3100, CacheRead: 18000,
			CacheCreate5m: 900, CacheCreate1h: 500, Thinking: 200,
		},
		Tools:      tools,
		ToolCounts: counts,
		Subagents: []domain.Subagent{
			{AgentType: "Explore", Description: "search the codebase", Model: "claude-fable-5-1", Live: false, CostUSD: floatPtr(0.18)},
			{AgentType: "worker", Description: "implement task 4", Model: "claude-sonnet-5", Live: true},
		},
		Proc: &domain.ProcSample{
			PID: 1234, CPUPct: 12.5, RSSBytes: 512_000_000,
			DiskReadB: 1048576, DiskWriteB: 262144,
		},
		RateLimits: []domain.RateLimit{
			{Scope: "primary", UsedPct: floatPtr(42.5), WindowMins: 300, ResetsAt: rejectedResets},
			{Scope: "secondary", WindowMins: 10080, ResetsAt: rejectedResets, Rejected: true},
		},
	}
}

func floatPtr(v float64) *float64 { return &v }

// TestDetailGolden renders the 12-entry-histogram session at a fixed
// 120x40 and golden-compares it. Run with -update to regenerate.
func TestDetailGolden(t *testing.T) {
	r := loadDarkRoles(t)
	out := DetailRender(sessionWithHistogram(), r, 120, 40, Options{})
	requireGoldenText(t, "detail_histogram", out)
}

// TestDetailHistogramCovers12Entries asserts every one of the 12 tool
// names appears in the rendered histogram.
func TestDetailHistogramCovers12Entries(t *testing.T) {
	r := loadDarkRoles(t)
	s := sessionWithHistogram()
	out := DetailRender(s, r, 120, 40, Options{})
	for name := range s.ToolCounts {
		if !strings.Contains(out, name) {
			t.Errorf("expected tool %q in the histogram, got:\n%s", name, out)
		}
	}
}

// TestDetailShowsSubagentTreeAndRateLimits asserts the subagent tree names
// both live and finished children with their resolved models, and that
// Codex rate limits render including a rejection-learned window.
func TestDetailShowsSubagentTreeAndRateLimits(t *testing.T) {
	r := loadDarkRoles(t)
	out := DetailRender(sessionWithHistogram(), r, 120, 40, Options{})

	for _, want := range []string{"claude-fable-5-1", "claude-sonnet-5", "live", "finished"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in subagent tree, got:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "primary") || !strings.Contains(out, "42.5%") {
		t.Errorf("expected the primary rate limit rendered, got:\n%s", out)
	}
	if !strings.Contains(out, "learned from rejection") {
		t.Errorf("expected the rejection-learned rate limit to be marked as such, got:\n%s", out)
	}
}

// TestDetailShowsBindConfidenceAndDiskIO asserts the bind confidence and
// the cumulative disk read/write bytes render -- this is the only place in
// v0.1 DiskReadB/DiskWriteB surface.
func TestDetailShowsBindConfidenceAndDiskIO(t *testing.T) {
	r := loadDarkRoles(t)
	s := sessionWithHistogram()
	out := DetailRender(s, r, 120, 40, Options{})

	if !strings.Contains(out, "bind=exact") {
		t.Errorf("expected bind confidence in the render, got:\n%s", out)
	}
	if !strings.Contains(out, "1048576") || !strings.Contains(out, "262144") {
		t.Errorf("expected DiskReadB/DiskWriteB in the render, got:\n%s", out)
	}
}

// TestDetailUnboundShowsPidUnknown asserts the process section falls back
// to "(pid unknown)" for a session whose bind confidence is unknown, never
// a crash or a zeroed process line.
func TestDetailUnboundShowsPidUnknown(t *testing.T) {
	r := loadDarkRoles(t)
	s := domain.Session{Agent: "codex", ID: "sess-unbound", BindConf: "unknown"}
	out := DetailRender(s, r, 120, 40, Options{})
	if !strings.Contains(out, "(pid unknown)") {
		t.Errorf("expected %q in the render, got:\n%s", "(pid unknown)", out)
	}
}

// TestFooterCarriesEstimateCaveat asserts the footer always renders the
// standing "estimates ... ignore subscription plans" caveat -- permanent
// UI, not a README line -- alongside total SoC watts next to total $/hr,
// wattop's own CPU cost, the unpriced-model count and any Degraded badges.
func TestFooterCarriesEstimateCaveat(t *testing.T) {
	r := loadDarkRoles(t)
	watts := 45.2
	snap := &domain.Snapshot{
		Sys:               domain.SysSample{Power: domain.Power{SystemWatts: &watts}},
		TotalCostUSD:      12.34,
		TotalBurnUSDPerHr: 9.5,
		UnpricedModels:    []string{"claude-nightly-experimental"},
		Degraded:          []string{"soc: ioreport"},
		SelfCPUPct:        1.2,
	}

	out := FooterRender(snap, r, 120, 4, Options{})

	if !strings.Contains(out, "estimates") || !strings.Contains(out, "ignore subscription plans") {
		t.Errorf("expected the estimate caveat in every footer render, got:\n%s", out)
	}
	if !strings.Contains(out, "45.2W") {
		t.Errorf("expected total SoC watts in the footer, got:\n%s", out)
	}
	if !strings.Contains(out, "$9.50/hr") {
		t.Errorf("expected total $/hr next to total watts in the footer, got:\n%s", out)
	}
	if !strings.Contains(out, "1.2%") {
		t.Errorf("expected wattop's own CPU cost in the footer, got:\n%s", out)
	}
	if !strings.Contains(out, "1 models unpriced") {
		t.Errorf("expected the unpriced-model count in the footer, got:\n%s", out)
	}
	if !strings.Contains(out, "Degraded") || !strings.Contains(out, "soc") {
		t.Errorf("expected a Degraded badge naming the source, got:\n%s", out)
	}
}

// TestFooterOmitsUnpricedAndDegradedWhenClean asserts the footer drops
// both the unpriced-model line and the Degraded badge when there is
// nothing to report -- the caveat itself is the only permanent line.
func TestFooterOmitsUnpricedAndDegradedWhenClean(t *testing.T) {
	r := loadDarkRoles(t)
	snap := &domain.Snapshot{}
	out := FooterRender(snap, r, 120, 4, Options{})
	if strings.Contains(out, "unpriced") {
		t.Errorf("expected no unpriced line when UnpricedModels is empty, got:\n%s", out)
	}
	if strings.Contains(out, "Degraded") {
		t.Errorf("expected no Degraded badge when Degraded is empty, got:\n%s", out)
	}
}
