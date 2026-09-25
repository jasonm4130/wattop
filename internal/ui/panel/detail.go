package panel

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// recentToolLogLen is how many of the session's most recent tool calls the
// detail view lists, oldest of the kept ones first. Kept short (rather than
// the whole transcript) so a session with a wide tool-name histogram still
// fits the fixed 120x40 the golden tests render at.
const recentToolLogLen = 5

// DetailRender draws the selected session's detail pane: the tool-call
// histogram by name, a recent tool log, the subagent tree with resolved
// model ids and live/finished state, the token breakdown by cache tier,
// per-session cost and burn, Codex rate limits where present, and the bind
// confidence. This is the only place DiskReadB/DiskWriteB surface in v0.1,
// and they surface as cumulative byte counters, labelled as such.
//
// Each section header carries its own one-line summary rather than a
// separate header-then-data pair, so a 12-entry tool-call histogram (the
// widest section) still leaves room for every other section inside a
// fixed 40-line frame.
//
// at is the snapshot's own At: a running subagent's elapsed time is
// measured against it (never the wall clock), so Render stays
// deterministic given its inputs.
func DetailRender(s domain.Session, r theme.Roles, width, height int, at time.Time, opts Options) string {
	var lines []string

	sectionHeader := func(text string) string { return styled(opts, r.Accent, text) }

	lines = append(lines, fmt.Sprintf("Session  %s (%s)  bind=%s", s.ID, s.Agent, s.BindConf))
	lines = append(lines, fmt.Sprintf("Model    %s   cwd %s", modelLabel(s.Model), s.CWD))
	if s.Model == "" {
		// An unresolved model says why on its own line rather than leaving
		// the field blank: no transcript on disk means no model, no tokens
		// and no cost for this session, and all three otherwise read as a
		// genuine zero. Its own line, not a suffix on the one above, so the
		// note cannot push the Model line past a narrow terminal's width.
		lines = append(lines, "         no transcript on disk: tokens and cost unavailable")
	}
	lines = append(lines, "")

	cost := "—"
	if s.Priced && s.CostUSD != nil {
		cost = fmt.Sprintf("$%.2f", *s.CostUSD)
	}
	if s.CostPartial {
		cost = "~" + cost
	}
	burn := "—"
	burnColor := ""
	if s.BurnUSDPerHr != nil {
		burn = fmt.Sprintf("$%.2f/hr", *s.BurnUSDPerHr)
		if *s.BurnUSDPerHr >= burnHotThresholdUSDPerHr {
			burnColor = r.CostHot
		}
	}
	lines = append(lines, fmt.Sprintf("Cost & burn: total %s   rate %s", cost, styled(opts, burnColor, burn)))

	tok := fmt.Sprintf("Tokens: input %s  output %s  cache-read %s  cache-write-5m %s  cache-write-1h %s  thinking %s",
		humanCount(s.Usage.Input), humanCount(s.Usage.Output), humanCount(s.Usage.CacheRead),
		humanCount(s.Usage.CacheCreate5m), humanCount(s.Usage.CacheCreate1h), humanCount(s.Usage.Thinking))
	if s.Usage.CachedInput > 0 {
		tok += fmt.Sprintf("  cached-input(Codex) %s", humanCount(s.Usage.CachedInput))
	}
	lines = append(lines, tok)
	if s.TokenRate != nil {
		lines = append(lines, fmt.Sprintf("Recorded tok/s (60s): in %.1f  out %.1f  cache-read %.1f",
			s.TokenRate.InputPerSec, s.TokenRate.OutputPerSec, s.TokenRate.CacheReadPerSec))
	}
	lines = append(lines, "")

	lines = append(lines, sectionHeader("Tool-call histogram"))
	lines = append(lines, toolHistogramLines(r, opts, s.ToolCounts, width)...)
	lines = append(lines, "")

	lines = append(lines, sectionHeader("Recent tool log"))
	lines = append(lines, recentToolLogLines(s.Tools)...)
	lines = append(lines, "")

	running := 0
	for i := range s.Subagents {
		if childRunning(s.Subagents[i].Status, s.Subagents[i].Live) {
			running++
		}
	}
	lines = append(lines, sectionHeader(fmt.Sprintf("Subagents (%d · %d running)", len(s.Subagents), running)))
	lines = append(lines, subagentTreeLines(r, opts, s, width, at)...)
	lines = append(lines, "")

	lines = append(lines, sectionHeader("Process:"))
	lines = append(lines, processLines(s)...)

	if len(s.RateLimits) > 0 {
		lines = append(lines, "")
		lines = append(lines, sectionHeader("Codex rate limits"))
		lines = append(lines, rateLimitLines(r, opts, s.RateLimits)...)
	}

	return frame(lines, width, height)
}

// burnHotThresholdUSDPerHr is the per-session burn rate that turns the
// cost line CostHot -- the same $5/hr line FooterRender uses for the
// machine-wide total, so a single session that alone crosses the
// machine's own hot threshold reads as hot here too.
const burnHotThresholdUSDPerHr = 5.0

// toolHistogramLabelW is the fixed column width the tool name is padded or
// elided to, so a long MCP tool name (e.g. "mcp__tavily__tavily_extract",
// 27 chars) can't push the bar and count out of alignment.
const toolHistogramLabelW = 20

// toolHistogramCountW is the fixed column width reserved for the trailing
// count, right-aligned so a three-digit session (521 calls) still lines up
// with single-digit ones instead of stretching the row.
const toolHistogramCountW = 6

// toolHistogramLines renders one bar per tool name, sorted by descending
// count then name, so a 12-entry histogram is stable across renders. The
// bar is scaled to the panel width and the largest count rather than drawn
// one block per call: unscaled, a busy session (hundreds of Bash calls)
// fills the whole line with blocks, pushes its own count off-screen, and
// conveys nothing -- padLine only pads a short line, it never truncates a
// long one, so an unscaled bar would also break DetailRender's
// exactly-width-columns contract for every line below it.
func toolHistogramLines(r theme.Roles, opts Options, counts map[string]int, width int) []string {
	if len(counts) == 0 {
		return []string{"  (no tool calls)"}
	}
	names := make([]string, 0, len(counts))
	maxCount := 0
	for n, c := range counts {
		names = append(names, n)
		if c > maxCount {
			maxCount = c
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})

	// "  " indent + label + " " + bar + " " + count.
	barW := width - 2 - toolHistogramLabelW - 1 - 1 - toolHistogramCountW
	if barW < 1 {
		barW = 1
	}

	out := make([]string, 0, len(names))
	for _, n := range names {
		c := counts[n]
		label := elideName(n, toolHistogramLabelW)
		barLen := 0
		if maxCount > 0 {
			barLen = int(math.Round(float64(c) / float64(maxCount) * float64(barW)))
		}
		if barLen < 1 && c > 0 {
			barLen = 1
		}
		if barLen > barW {
			barLen = barW
		}
		bar := styled(opts, r.BarFill, strings.Repeat("█", barLen))
		out = append(out, fmt.Sprintf("  %-*s %s %*d", toolHistogramLabelW, label, bar, toolHistogramCountW, c))
	}
	return out
}

// elideName truncates a tool name to w columns (measured with
// lipgloss.Width so multi-byte glyphs count correctly), replacing the last
// character with "…" when it doesn't fit, so a long name like
// "mcp__tavily__tavily_extract" can't break the fixed label column the bar
// and count are aligned against.
func elideName(name string, w int) string {
	if lipgloss.Width(name) <= w {
		return name
	}
	if w <= 1 {
		return "…"
	}
	r := []rune(name)
	for i := len(r) - 1; i > 0; i-- {
		cand := string(r[:i]) + "…"
		if lipgloss.Width(cand) <= w {
			return cand
		}
	}
	return "…"
}

func recentToolLogLines(tools []domain.ToolCall) []string {
	if len(tools) == 0 {
		return []string{"  (none)"}
	}
	start := 0
	if len(tools) > recentToolLogLen {
		start = len(tools) - recentToolLogLen
	}
	out := make([]string, 0, len(tools)-start)
	for _, t := range tools[start:] {
		out = append(out, fmt.Sprintf("  %s  %-20s %s", t.At.Format("15:04:05"), t.Name, t.ID))
	}
	return out
}

// subagentTreeLines renders the session's children: the non-workflow
// subagents as a box-drawn tree (children under their ParentID, in
// s.Subagents order), then each workflow as a header line followed by its
// agents one level in. Every line is cut to width rather than wrapped.
func subagentTreeLines(r theme.Roles, opts Options, s domain.Session, width int, at time.Time) []string {
	if len(s.Subagents) == 0 && len(s.Workflows) == 0 {
		return []string{"  (none)"}
	}
	fit := func(line string) string { return ansi.Truncate(line, max(0, width), "…") }

	byID := make(map[string]int, len(s.Subagents))
	for i := range s.Subagents {
		if s.Subagents[i].WorkflowID == "" && s.Subagents[i].ID != "" {
			byID[s.Subagents[i].ID] = i
		}
	}
	kids := make(map[int][]int)
	var roots []int
	for i := range s.Subagents {
		sa := &s.Subagents[i]
		if sa.WorkflowID != "" {
			continue
		}
		if p, ok := byID[sa.ParentID]; ok && sa.ParentID != "" && p != i {
			kids[p] = append(kids[p], i)
		} else {
			roots = append(roots, i)
		}
	}

	var out []string
	seen := make(map[int]bool)
	var walk func(i int, prefix string, last bool)
	walk = func(i int, prefix string, last bool) {
		if seen[i] {
			return
		}
		seen[i] = true
		branch, cont := "├─ ", "│  "
		if last {
			branch, cont = "└─ ", "   "
		}
		out = append(out, fit(subagentDetailLine(r, opts, "  "+prefix+branch, &s.Subagents[i], false, at)))
		for k, c := range kids[i] {
			walk(c, prefix+cont, k == len(kids[i])-1)
		}
	}
	for k, i := range roots {
		walk(i, "", k == len(roots)-1)
	}
	// A ParentID cycle has no root to reach its members from; list them
	// flat rather than drop them.
	for i := range s.Subagents {
		if s.Subagents[i].WorkflowID == "" && !seen[i] {
			walk(i, "", true)
		}
	}

	for wi := range s.Workflows {
		w := &s.Workflows[wi]
		color, state := childStatus(r, w.Status, false)
		head := "  Workflow " + w.ID + "  " + styled(opts, color, state)
		if w.Phase != "" {
			head += "  phase " + w.Phase
		}
		head += fmt.Sprintf("  %d run %d/%d done %d fail  %s", w.Running, w.Done, w.Agents, w.Failed, childCost(w.CostUSD, w.CostPartial))
		if b := childBurn(w.BurnUSDPerHr); b != "" {
			head += " " + b + "/h"
		}
		out = append(out, fit(head))

		agents, hidden := workflowAgentsShown(s.Subagents, w)
		for k, i := range agents {
			branch := "├─ "
			if k == len(agents)-1 && hidden == 0 {
				branch = "└─ "
			}
			out = append(out, fit(subagentDetailLine(r, opts, "    "+branch, &s.Subagents[i], true, at)))
		}
		if hidden > 0 {
			out = append(out, fit(styled(opts, r.Muted, fmt.Sprintf("    └─ … %d more done", hidden))))
		}
	}
	return out
}

// workflowDoneShown is how many finished agents a workflow lists: its most
// recently active ones. A single run can hold a hundred agents, and listing
// every one would push the rest of the panel off screen.
const workflowDoneShown = 3

// workflowAgentsShown returns the indexes of w's agents to list, in
// s.Subagents order, and how many finished agents it left out. Every
// unfinished or failed agent is listed; of the done ones, only the
// workflowDoneShown most recently active, and none once the whole workflow
// is done.
func workflowAgentsShown(subs []domain.Subagent, w *domain.Workflow) ([]int, int) {
	var done []int
	keep := make(map[int]bool)
	for i := range subs {
		if subs[i].WorkflowID != w.ID {
			continue
		}
		if subs[i].Status == domain.SubagentDone {
			done = append(done, i)
		} else {
			keep[i] = true
		}
	}
	limit := workflowDoneShown
	if w.Status == domain.SubagentDone {
		limit = 0
	}
	recent := append([]int(nil), done...)
	sort.SliceStable(recent, func(a, b int) bool {
		return subs[recent[a]].LastActivityAt.After(subs[recent[b]].LastActivityAt)
	})
	if len(recent) > limit {
		recent = recent[:limit]
	}
	for _, i := range recent {
		keep[i] = true
	}
	var shown []int
	for i := range subs {
		if keep[i] {
			shown = append(shown, i)
		}
	}
	return shown, len(done) - len(recent)
}

// subagentDetailLine is one child's detail line after its tree prefix:
// status, agent type (or phase, for a workflow agent), description, model,
// elapsed, tool count, the tool a running child is waiting on, cost and
// burn.
func subagentDetailLine(r theme.Roles, opts Options, prefix string, sa *domain.Subagent, inWorkflow bool, at time.Time) string {
	color, state := childStatus(r, sa.Status, sa.Live)
	running := childRunning(sa.Status, sa.Live)
	// The kind column gives up whatever the tree prefix grew past a root's
	// ("  ├─ ", 5 columns), so every column after it lines up at any depth.
	kindW := max(4, 12-(lipgloss.Width(prefix)-5))
	kind := truncate(sa.AgentType, kindW)
	if sa.Background {
		kind = truncate(sa.AgentType, kindW-1) + "⇢"
	}
	if inWorkflow {
		kind = sa.Phase
		if kind == "" {
			kind = "—"
		}
		kind = truncate(kind, kindW)
	}
	line := prefix + styled(opts, color, padLine(state, 5)) + " " +
		padLine(kind, kindW) + " " +
		padLine(truncate(sa.Description, 28), 28) + " " +
		modelLabel(sa.Model) + "  " +
		childElapsed(sa.StartedAt, sa.LastActivityAt, at, running) + "  " +
		fmt.Sprintf("%d tools", sa.ToolCalls)
	if running && sa.CurrentTool != "" {
		line += "  ▸ " + sa.CurrentTool
	}
	line += "  " + childCost(sa.CostUSD, false)
	if b := childBurn(sa.BurnUSDPerHr); b != "" {
		line += " " + b + "/h"
	}
	return line
}

// processLines renders the selected session's process detail. The disk
// figures are labelled "(cumulative)" and carry a plain B unit because
// that is exactly what they are: proc_pid_rusage's
// ri_diskio_bytesread/byteswritten, counted since the process started and
// passed through internal/proc with no delta tracker behind them. A "/s"
// suffix here would read as a rate and be wrong by however long the
// process has been alive -- on the one screen in v0.1 where these two
// fields surface at all.
func processLines(s domain.Session) []string {
	if s.BindConf == "unknown" || s.Proc == nil {
		return []string{"  (pid unknown) cpu — gpu — rss — disk (cumulative) r — w —"}
	}
	p := s.Proc
	return []string{fmt.Sprintf("  pid %d  cpu %.1f%%  gpu %s ms/s  rss %.0fM  disk (cumulative) r %s  w %s",
		p.PID, p.CPUPct, fdash(p.GPUMsPerSec, "%.1f"), float64(p.RSSBytes)/1e6, humanBytes(p.DiskReadB), humanBytes(p.DiskWriteB))}
}

func rateLimitLines(r theme.Roles, opts Options, limits []domain.RateLimit) []string {
	out := make([]string, 0, len(limits))
	for _, rl := range limits {
		used := "—"
		if rl.UsedPct != nil {
			used = fmt.Sprintf("%.1f%%", *rl.UsedPct)
		}
		rejected := ""
		if rl.Rejected {
			rejected = " (learned from rejection)"
		}
		line := fmt.Sprintf("  %-8s used %-6s window %dm resets %s%s",
			rl.Scope, used, rl.WindowMins, rl.ResetsAt.Format("15:04"), rejected)
		out = append(out, styled(opts, r.Warn, line))
	}
	return out
}
