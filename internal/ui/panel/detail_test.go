package panel

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/jasonm4130/wattop/internal/domain"
)

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
		BindConf:     domain.BindExact,
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
			PID: 1234, CPUPct: floatPtr(12.5), RSSBytes: 512_000_000,
			DiskReadB: 1048576, DiskWriteB: 262144,
		},
		RateLimits: []domain.RateLimit{
			{Scope: "primary", UsedPct: floatPtr(42.5), WindowMins: 300, ResetsAt: rejectedResets},
			{Scope: "secondary", WindowMins: 10080, ResetsAt: rejectedResets, Rejected: true},
		},
	}
}

func floatPtr(v float64) *float64 { return &v }

func TestDetailGolden(t *testing.T) {
	r := loadDarkRoles(t)
	out := DetailRender(sessionWithHistogram(), r, 120, 40, time.Time{}, Options{})
	requireGoldenText(t, "detail_histogram", out)
}

func TestDetailHonoursNoColor(t *testing.T) {
	r := loadDarkRoles(t)
	s := sessionWithHistogram()

	colored := DetailRender(s, r, 120, 40, time.Time{}, Options{})
	if !strings.Contains(colored, "\x1b[") {
		t.Errorf("expected an ANSI escape when NoColor is false, got:\n%s", colored)
	}

	plain := DetailRender(s, r, 120, 40, time.Time{}, Options{NoColor: true})
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("expected no ANSI escape when NoColor is true, got:\n%s", plain)
	}
}

func TestDetailHistogramCovers12Entries(t *testing.T) {
	r := loadDarkRoles(t)
	s := sessionWithHistogram()
	out := DetailRender(s, r, 120, 40, time.Time{}, Options{})
	for name := range s.ToolCounts {
		if !strings.Contains(out, name) {
			t.Errorf("expected tool %q in the histogram, got:\n%s", name, out)
		}
	}
}

func TestDetailHistogramBarFitsWidthWithBusySession(t *testing.T) {
	r := loadDarkRoles(t)
	s := domain.Session{
		Agent: "claude", ID: "sess-busy", BindConf: domain.BindExact,
		ToolCounts: map[string]int{
			"Bash":                        521,
			"mcp__tavily__tavily_extract": 3,
		},
	}
	out := DetailRender(s, r, 120, 40, time.Time{}, Options{})

	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 120 {
			t.Errorf("expected every line <= 120 columns, got %d: %q", w, line)
		}
	}
	if !strings.Contains(out, "521") {
		t.Errorf("expected the count 521 to remain visible, got:\n%s", out)
	}
}

func TestDetailShowsSubagentTreeAndRateLimits(t *testing.T) {
	r := loadDarkRoles(t)
	out := DetailRender(sessionWithHistogram(), r, 120, 40, time.Time{}, Options{})

	for _, want := range []string{"claude-fable-5-1", "claude-sonnet-5", "● run", "done"} {
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

func TestDetailShowsBindConfidenceAndDiskIO(t *testing.T) {
	r := loadDarkRoles(t)
	s := sessionWithHistogram()
	out := DetailRender(s, r, 120, 40, time.Time{}, Options{})

	if !strings.Contains(out, "bind=exact") {
		t.Errorf("expected bind confidence in the render, got:\n%s", out)
	}
	if !strings.Contains(out, "1.0MB") || !strings.Contains(out, "262.1KB") {
		t.Errorf("expected humanized DiskReadB/DiskWriteB in the render, got:\n%s", out)
	}
}

func TestDetailDiskIOLabelledCumulativeNotRate(t *testing.T) {
	r := loadDarkRoles(t)

	bound := DetailRender(sessionWithHistogram(), r, 120, 40, time.Time{}, Options{})
	unbound := DetailRender(domain.Session{Agent: "codex", ID: "sess-unbound", BindConf: domain.BindUnknown}, r, 120, 40, time.Time{}, Options{})

	for name, out := range map[string]string{"bound": bound, "unbound": unbound} {
		if !strings.Contains(out, "disk (cumulative)") {
			t.Errorf("%s: expected the disk counters marked cumulative, got:\n%s", name, out)
		}
		if strings.Contains(out, "B/s") {
			t.Errorf("%s: disk bytes are cumulative counters, never a per-second rate, got:\n%s", name, out)
		}
	}

	if !strings.Contains(bound, "r 1.0MB") || !strings.Contains(bound, "w 262.1KB") {
		t.Errorf("expected humanized cumulative byte counts, got:\n%s", bound)
	}
}

func TestDetailUnboundShowsPidUnknown(t *testing.T) {
	r := loadDarkRoles(t)
	s := domain.Session{Agent: "codex", ID: "sess-unbound", BindConf: domain.BindUnknown}
	out := DetailRender(s, r, 120, 40, time.Time{}, Options{})
	if !strings.Contains(out, "(pid unknown)") {
		t.Errorf("expected %q in the render, got:\n%s", "(pid unknown)", out)
	}
}

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

	out := FooterRender(snap, r, 120, 5, "cost", "nord", false, 0, Options{})

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

func TestFooterCaveatSurvivesAtProductionHeight(t *testing.T) {
	r := loadDarkRoles(t)
	snap := &domain.Snapshot{UnpricedModels: []string{"claude-nightly-experimental"}}
	out := FooterRender(snap, r, 120, 4, "status", "dark", false, 0, Options{})
	if !strings.Contains(out, "ignore subscription plans") {
		t.Errorf("expected the estimate caveat to survive at footerH 4 alongside the status line, got:\n%s", out)
	}
}

func TestFooterOmitsUnpricedAndDegradedWhenClean(t *testing.T) {
	r := loadDarkRoles(t)
	snap := &domain.Snapshot{}
	out := FooterRender(snap, r, 120, 4, "status", "dark", false, 0, Options{})
	if strings.Contains(out, "unpriced") {
		t.Errorf("expected no unpriced line when UnpricedModels is empty, got:\n%s", out)
	}
	if strings.Contains(out, "Degraded") {
		t.Errorf("expected no Degraded badge when Degraded is empty, got:\n%s", out)
	}
}

func TestFooterBurnGoesHotAboveThreshold(t *testing.T) {
	r := loadDarkRoles(t)
	hot := FooterRender(&domain.Snapshot{TotalBurnUSDPerHr: 9.5}, r, 120, 4, "cost", "dark", false, 0, Options{})
	cold := FooterRender(&domain.Snapshot{TotalBurnUSDPerHr: 4.99}, r, 120, 4, "cost", "dark", false, 0, Options{})

	if want := styled(Options{}, r.CostHot, "$9.50/hr"); !strings.Contains(hot, want) {
		t.Errorf("expected $9.50/hr to carry the CostHot colour %q, got:\n%s", r.CostHot, hot)
	}
	if strings.Contains(cold, styled(Options{}, r.CostHot, "$4.99/hr")) {
		t.Errorf("expected $4.99/hr to be unstyled, got:\n%s", cold)
	}
	if !strings.Contains(cold, "$4.99/hr") {
		t.Errorf("expected the rate itself to render below the threshold, got:\n%s", cold)
	}
}

func TestFooterBurnHotThresholdIsConfigurable(t *testing.T) {
	r := loadDarkRoles(t)
	opts := Options{BurnHotUSDPerHr: 10}
	below := FooterRender(&domain.Snapshot{TotalBurnUSDPerHr: 9.5}, r, 120, 4, "cost", "dark", false, 0, opts)
	above := FooterRender(&domain.Snapshot{TotalBurnUSDPerHr: 10.5}, r, 120, 4, "cost", "dark", false, 0, opts)

	if strings.Contains(below, styled(Options{}, r.CostHot, "$9.50/hr")) {
		t.Errorf("with a $10/hr threshold, $9.50/hr must not be hot; got:\n%s", below)
	}
	if want := styled(Options{}, r.CostHot, "$10.50/hr"); !strings.Contains(above, want) {
		t.Errorf("with a $10/hr threshold, $10.50/hr must be hot; got:\n%s", above)
	}
}

func TestFooterStatusLineAdvertisesKeymap(t *testing.T) {
	r := loadDarkRoles(t)
	snap := &domain.Snapshot{}
	out := FooterRender(snap, r, 120, 4, "cost", "nord", false, 0, Options{})

	if !strings.Contains(out, "sort:cost") {
		t.Errorf("expected the active sort key in the footer, got:\n%s", out)
	}
	if !strings.Contains(out, "theme:nord") {
		t.Errorf("expected the active theme name in the footer, got:\n%s", out)
	}
	if !strings.Contains(out, "? help") {
		t.Errorf("expected a hint pointing at the ? help overlay, got:\n%s", out)
	}
	if strings.Contains(out, "PAUSED") {
		t.Errorf("expected no PAUSED marker when not paused, got:\n%s", out)
	}
}

func TestFooterShowsPaused(t *testing.T) {
	r := loadDarkRoles(t)
	snap := &domain.Snapshot{}
	out := FooterRender(snap, r, 120, 4, "status", "dark", true, 0, Options{})

	if !strings.Contains(out, "[PAUSED]") {
		t.Errorf("expected a [PAUSED] marker while paused, got:\n%s", out)
	}
}

func TestFooterStatusLineSurvivesTruncation(t *testing.T) {
	r := loadDarkRoles(t)
	snap := &domain.Snapshot{
		UnpricedModels: []string{"claude-nightly-experimental"},
		Degraded:       []string{"soc: ioreport"},
	}
	out := FooterRender(snap, r, 120, 3, "burn", "nord", true, 0, Options{})

	if !strings.Contains(out, "sort:burn") || !strings.Contains(out, "theme:nord") {
		t.Errorf("expected sort/theme to survive truncation at height 3, got:\n%s", out)
	}
	if !strings.Contains(out, "[PAUSED]") {
		t.Errorf("expected [PAUSED] to survive truncation at height 3, got:\n%s", out)
	}
}

func TestFooterFitsAt80Columns(t *testing.T) {
	r := loadDarkRoles(t)
	watts := 8.4
	snap := &domain.Snapshot{
		Sys:               domain.SysSample{Power: domain.Power{SystemWatts: &watts}},
		TotalCostUSD:      335.19,
		TotalBurnUSDPerHr: 102.53,
		SelfCPUPct:        0.4,
	}

	out := FooterRender(snap, r, 80, 4, "status", "wattop-dark", true, 3, Options{})
	for i, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("footer line %d is %d cells wide at width 80: %q", i, w, line)
		}
	}
	if !strings.Contains(out, "$335.19 session total") {
		t.Errorf("the session total was truncated rather than a whole segment dropped, got:\n%s", out)
	}
	if !strings.Contains(out, "3 hidden (a)") || !strings.Contains(out, "PAUSED") {
		t.Errorf("the hidden count and [PAUSED] must survive a narrow terminal, got:\n%s", out)
	}
}

func TestFooterKeepsEverySegmentWhenItFits(t *testing.T) {
	r := loadDarkRoles(t)
	watts := 8.4
	snap := &domain.Snapshot{
		Sys:          domain.SysSample{Power: domain.Power{SystemWatts: &watts}},
		TotalCostUSD: 335.19,
		SelfCPUPct:   0.4,
	}
	out := FooterRender(snap, r, 120, 4, "status", "wattop-dark", false, 0, Options{})
	for _, want := range []string{"8.4W total", "session total", "wattop self 0.4% CPU"} {
		if !strings.Contains(out, want) {
			t.Errorf("footer at 120 columns dropped %q, got:\n%s", want, out)
		}
	}
}

func TestDetailSaysWhyTheModelIsUnknown(t *testing.T) {
	r := loadDarkRoles(t)
	out := DetailRender(domain.Session{Agent: "claude", ID: "sess-headless", BindConf: domain.BindExact, CWD: "/repo/x"}, r, 120, 40, time.Time{}, Options{})
	if !strings.Contains(out, "Model    —") {
		t.Errorf("an unresolved model must render as a dash, got:\n%s", out)
	}
	if !strings.Contains(out, "no transcript on disk") {
		t.Errorf("the detail view must say why the model is unknown, got:\n%s", out)
	}
}
