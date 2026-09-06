package panel

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

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
func DetailRender(s domain.Session, r theme.Roles, width, height int, opts Options) string {
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

	lines = append(lines, sectionHeader(fmt.Sprintf("Subagents (%d)", len(s.Subagents))))
	lines = append(lines, subagentTreeLines(r, opts, s.Subagents)...)
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

func subagentTreeLines(r theme.Roles, opts Options, subagents []domain.Subagent) []string {
	if len(subagents) == 0 {
		return []string{"  (none)"}
	}
	out := make([]string, 0, len(subagents))
	for _, sa := range subagents {
		state := "finished"
		color := r.Muted
		if sa.Live {
			state = "live"
			color = r.Busy
		}
		cost := "$—"
		if sa.CostUSD != nil {
			cost = fmt.Sprintf("$%.2f", *sa.CostUSD)
		}
		state = styled(opts, color, fmt.Sprintf("%-8s", state))
		out = append(out, fmt.Sprintf("  └─ %-10s %-24s model=%-20s %s %s",
			sa.AgentType, sa.Description, sa.Model, state, cost))
	}
	return out
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
