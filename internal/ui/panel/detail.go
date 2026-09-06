package panel

import (
	"fmt"
	"sort"
	"strings"

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
// confidence. This is the only place DiskReadB/DiskWriteB surface in v0.1.
//
// Each section header carries its own one-line summary rather than a
// separate header-then-data pair, so a 12-entry tool-call histogram (the
// widest section) still leaves room for every other section inside a
// fixed 40-line frame.
func DetailRender(s domain.Session, r theme.Roles, width, height int, opts Options) string {
	var lines []string

	lines = append(lines, fmt.Sprintf("Session  %s (%s)  bind=%s", s.ID, s.Agent, s.BindConf))
	lines = append(lines, fmt.Sprintf("Model    %s   cwd %s", s.Model, s.CWD))
	lines = append(lines, "")

	cost := "—"
	if s.Priced && s.CostUSD != nil {
		cost = fmt.Sprintf("$%.4f", *s.CostUSD)
	}
	burn := "—"
	if s.BurnUSDPerHr != nil {
		burn = fmt.Sprintf("$%.2f/hr", *s.BurnUSDPerHr)
	}
	lines = append(lines, fmt.Sprintf("Cost & burn: total %s   rate %s", cost, burn))

	tok := fmt.Sprintf("Tokens: input %d  output %d  cache-read %d  cache-write-5m %d  cache-write-1h %d  thinking %d",
		s.Usage.Input, s.Usage.Output, s.Usage.CacheRead, s.Usage.CacheCreate5m, s.Usage.CacheCreate1h, s.Usage.Thinking)
	if s.Usage.CachedInput > 0 {
		tok += fmt.Sprintf("  cached-input(Codex) %d", s.Usage.CachedInput)
	}
	lines = append(lines, tok)
	lines = append(lines, "")

	lines = append(lines, "Tool-call histogram")
	lines = append(lines, toolHistogramLines(s.ToolCounts)...)
	lines = append(lines, "")

	lines = append(lines, "Recent tool log")
	lines = append(lines, recentToolLogLines(s.Tools)...)
	lines = append(lines, "")

	lines = append(lines, fmt.Sprintf("Subagents (%d)", len(s.Subagents)))
	lines = append(lines, subagentTreeLines(s.Subagents)...)
	lines = append(lines, "")

	lines = append(lines, "Process:")
	lines = append(lines, processLines(s)...)

	if len(s.RateLimits) > 0 {
		lines = append(lines, "")
		lines = append(lines, "Codex rate limits")
		lines = append(lines, rateLimitLines(s.RateLimits)...)
	}

	return frame(lines, width, height)
}

// toolHistogramLines renders one bar per tool name, sorted by descending
// count then name, so a 12-entry histogram is stable across renders.
func toolHistogramLines(counts map[string]int) []string {
	if len(counts) == 0 {
		return []string{"  (no tool calls)"}
	}
	names := make([]string, 0, len(counts))
	for n := range counts {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, fmt.Sprintf("  %-20s %s %d", n, strings.Repeat("█", counts[n]), counts[n]))
	}
	return out
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

func subagentTreeLines(subagents []domain.Subagent) []string {
	if len(subagents) == 0 {
		return []string{"  (none)"}
	}
	out := make([]string, 0, len(subagents))
	for _, sa := range subagents {
		state := "finished"
		if sa.Live {
			state = "live"
		}
		cost := "$—"
		if sa.CostUSD != nil {
			cost = fmt.Sprintf("$%.2f", *sa.CostUSD)
		}
		out = append(out, fmt.Sprintf("  └─ %-10s %-24s model=%-20s %-8s %s",
			sa.AgentType, sa.Description, sa.Model, state, cost))
	}
	return out
}

func processLines(s domain.Session) []string {
	if s.BindConf == "unknown" || s.Proc == nil {
		return []string{"  (pid unknown) cpu — gpu — rss — disk r/w —/—"}
	}
	p := s.Proc
	return []string{fmt.Sprintf("  pid %d  cpu %.1f%%  gpu %s ms/s  rss %.0fM  disk r %d B/s w %d B/s",
		p.PID, p.CPUPct, fdash(p.GPUMsPerSec, "%.1f"), float64(p.RSSBytes)/1e6, p.DiskReadB, p.DiskWriteB)}
}

func rateLimitLines(limits []domain.RateLimit) []string {
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
		out = append(out, fmt.Sprintf("  %-8s used %-6s window %dm resets %s%s",
			rl.Scope, used, rl.WindowMins, rl.ResetsAt.Format("15:04"), rejected))
	}
	return out
}
