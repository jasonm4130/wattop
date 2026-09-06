package panel

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

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

// TestDetailHonoursNoColor asserts DetailRender styles through the theme
// when NoColor is false (an ANSI escape appears) and emits plain text when
// NoColor is true (the --no-color / NO_COLOR contract every other panel
// already honours via the shared styled() helper).
func TestDetailHonoursNoColor(t *testing.T) {
	r := loadDarkRoles(t)
	s := sessionWithHistogram()

	colored := DetailRender(s, r, 120, 40, Options{})
	if !strings.Contains(colored, "\x1b[") {
		t.Errorf("expected an ANSI escape when NoColor is false, got:\n%s", colored)
	}

	plain := DetailRender(s, r, 120, 40, Options{NoColor: true})
	if strings.Contains(plain, "\x1b[") {
		t.Errorf("expected no ANSI escape when NoColor is true, got:\n%s", plain)
	}
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

// TestDetailHistogramBarFitsWidthWithBusySession asserts a session with a
// large call count (521 Bash calls) still renders every line within the
// panel width -- unscaled, a 521-block bar filled the entire 120-column
// line and pushed the count off-screen. It also covers a long MCP-style
// tool name ("mcp__tavily__tavily_extract", 27 chars), which must not
// break the fixed label column the bar and count are aligned against.
func TestDetailHistogramBarFitsWidthWithBusySession(t *testing.T) {
	r := loadDarkRoles(t)
	s := domain.Session{
		Agent: "claude", ID: "sess-busy", BindConf: "exact",
		ToolCounts: map[string]int{
			"Bash":                        521,
			"mcp__tavily__tavily_extract": 3,
		},
	}
	out := DetailRender(s, r, 120, 40, Options{})

	for _, line := range strings.Split(out, "\n") {
		if w := lipgloss.Width(line); w > 120 {
			t.Errorf("expected every line <= 120 columns, got %d: %q", w, line)
		}
	}
	if !strings.Contains(out, "521") {
		t.Errorf("expected the count 521 to remain visible, got:\n%s", out)
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
	if !strings.Contains(out, "1.0MB") || !strings.Contains(out, "262.1KB") {
		t.Errorf("expected humanized DiskReadB/DiskWriteB in the render, got:\n%s", out)
	}
}

// TestDetailDiskIOLabelledCumulativeNotRate pins the unit on the disk
// figures. ProcSample.DiskReadB/DiskWriteB are proc_pid_rusage counters
// accumulated since the process started -- internal/proc keeps no delta
// tracker for them -- so a "B/s" suffix would sell a lifetime total as a
// per-second rate, wrong by however long the process has been alive. The
// negative assertion is on "B/s" and not on "/s", because "gpu ... ms/s"
// on the same line legitimately is a rate.
func TestDetailDiskIOLabelledCumulativeNotRate(t *testing.T) {
	r := loadDarkRoles(t)

	bound := DetailRender(sessionWithHistogram(), r, 120, 40, Options{})
	unbound := DetailRender(domain.Session{Agent: "codex", ID: "sess-unbound", BindConf: "unknown"}, r, 120, 40, Options{})

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

// TestFooterCaveatSurvivesAtProductionHeight pins the shape Model.View()
// actually renders (footerH == 4, no Degraded badge competing for space):
// the status line added for sort/theme/paused must not evict the standing
// estimate caveat when there is only an unpriced-model line alongside it.
// The both-unpriced-and-Degraded shape still overflows footerH == 4 and is
// a pre-existing gap this task does not attempt to close.
func TestFooterCaveatSurvivesAtProductionHeight(t *testing.T) {
	r := loadDarkRoles(t)
	snap := &domain.Snapshot{UnpricedModels: []string{"claude-nightly-experimental"}}
	out := FooterRender(snap, r, 120, 4, "status", "dark", false, 0, Options{})
	if !strings.Contains(out, "ignore subscription plans") {
		t.Errorf("expected the estimate caveat to survive at footerH 4 alongside the status line, got:\n%s", out)
	}
}

// TestFooterOmitsUnpricedAndDegradedWhenClean asserts the footer drops
// both the unpriced-model line and the Degraded badge when there is
// nothing to report -- the caveat itself is the only permanent line.
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

// TestFooterBurnGoesHotAboveThreshold pins the $/hr severity colour. The
// e2e golden used to be the only thing covering it, through a fixture whose
// burn rate rendered $455.88/hr; the burn fix now baselines that corpus's
// backfilled spend to $0.00/hr and the golden no longer exercises the hot
// branch at all. Assert it here, where the threshold lives, instead.
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

// TestFooterStatusLineAdvertisesKeymap asserts the footer names the active
// sort key and theme and points at the `?` help overlay -- otherwise s and
// t leave no trace on screen at all.
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

// TestFooterShowsPaused asserts pausing renders a [PAUSED] marker -- the
// one state a monitoring tool must never leave silent.
func TestFooterShowsPaused(t *testing.T) {
	r := loadDarkRoles(t)
	snap := &domain.Snapshot{}
	out := FooterRender(snap, r, 120, 4, "status", "dark", true, 0, Options{})

	if !strings.Contains(out, "[PAUSED]") {
		t.Errorf("expected a [PAUSED] marker while paused, got:\n%s", out)
	}
}

// TestFooterStatusLineSurvivesTruncation asserts the sort/theme/paused
// status line is placed early enough in the footer that it still renders
// even when height is too short for every line the footer can produce --
// e.g. wattop's own on-screen footer height of 3, with both an unpriced
// count and a Degraded badge present, which alone already fill 3 lines
// before the caveat. The status line must not be the one line to lose that
// race.
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

// TestFooterFitsAt80Columns is the QA defect of 2026-09-06: the Machine
// line is a plain Sprintf of four segments, frame() pads short lines but
// never truncates long ones, and at 80 columns the line ran 15 cells past
// the terminal and wrapped over the row below it. Segments are dropped
// whole rather than truncated -- a "$335.19 session total" cut mid-number
// reads as a smaller, wrong figure.
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

// TestFooterKeepsEverySegmentWhenItFits guards the drop rule from the
// other side: at 120 columns nothing is dropped.
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

// TestDetailSaysWhyTheModelIsUnknown: a session with no transcript on disk
// (a headless `claude -p` run writes none) renders a dash and a reason,
// never a blank field that reads as a rendering bug.
func TestDetailSaysWhyTheModelIsUnknown(t *testing.T) {
	r := loadDarkRoles(t)
	out := DetailRender(domain.Session{Agent: "claude", ID: "sess-headless", BindConf: "exact", CWD: "/repo/x"}, r, 120, 40, Options{})
	if !strings.Contains(out, "Model    —") {
		t.Errorf("an unresolved model must render as a dash, got:\n%s", out)
	}
	if !strings.Contains(out, "no transcript on disk") {
		t.Errorf("the detail view must say why the model is unknown, got:\n%s", out)
	}
}
