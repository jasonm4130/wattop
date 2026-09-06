package panel

import (
	"fmt"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// spinnerGlyph is a single static braille frame. The animation itself lives
// in model.go's tick-driven redraw (a Snapshot carries no frame counter);
// this is the glyph a busy row renders on any given frame.
const spinnerGlyph = "⠋"

const ctxBarWidth = 10

// Column widths shared between sessionsHeader and sessionRow/subagentRow so
// the two cannot drift out of alignment. wCTX is 18, not 16: the gauge's
// widest form (marker + bracket + ctxBarWidth-wide bar + bracket + " NNN%")
// is 18 visible columns, and a header slot narrower than its widest row
// content misaligns every column to its right.
const (
	wStatus = 16
	wPID    = 13
	wAgent  = 6
	wModel  = 14
	wCWD    = 16
	wCTX    = 18
	wTok    = 18
	wCost   = 7
	wBurn   = 8
)

// unknownPID is the literal string a BindConf == "unknown" session renders
// in place of a real pid -- keyed on BindConf, never on Proc == nil, since
// a session can in principle carry BindConf == "unknown" with a populated
// Proc (see TestUnboundPidRendersDashes's synthetic case).
const unknownPID = "(pid unknown)"

// SessionsRender draws the session table from a flat, already
// sorted-and-filtered list of sessions, at is the snapshot's own At (never
// the wall clock: a stale row's age is measured against it, and Render
// must stay deterministic given its inputs). selected is the flattened row
// index to highlight, or -1 for none.
//
// Subagents render as indented child rows directly under their parent
// session, in the order Session.Subagents lists them.
//
// When the flattened row count exceeds the frame's data height, the table
// scrolls: the visible window is recentred on selected (windowRows), and
// the first/last visible line becomes a "▲/▼ N more" marker whenever rows
// are hidden on that side, so an off-screen selection is never silent and
// never itself hidden behind a marker.
func SessionsRender(sessions []domain.Session, r theme.Roles, width, height, selected int, at time.Time, opts Options) string {
	rows := sessionRows(sessions, r, at, opts, selected)
	rows = windowRows(rows, height-1, selected)

	lines := append([]string{sessionsHeader()}, rows...)
	return frame(lines, width, height)
}

// sessionRows flattens sessions (and their subagents) into one
// already-styled line per row, in flattened-row order.
func sessionRows(sessions []domain.Session, r theme.Roles, at time.Time, opts Options, selected int) []string {
	var lines []string
	row := 0
	for _, s := range sessions {
		lines = append(lines, styleSelected(sessionRow(r, s, at, opts), row == selected, opts))
		row++
		for i := range s.Subagents {
			lines = append(lines, styleSelected(subagentRow(r, &s.Subagents[i], opts), row == selected, opts))
			row++
		}
	}
	return lines
}

// windowRows returns at most visible lines from rows, scrolled so selected
// (a flattened row index, or -1 for none) stays on screen. When rows are
// hidden above or below the window, the first/last visible line is
// replaced with a "▲/▼ N more" marker -- unless that line is the selected
// row itself, in which case the marker is skipped rather than hiding the
// selection.
func windowRows(rows []string, visible, selected int) []string {
	n := len(rows)
	if visible <= 0 {
		return nil
	}
	if n <= visible {
		return rows
	}

	first := selected - visible/2
	if first < 0 {
		first = 0
	}
	if max := n - visible; first > max {
		first = max
	}

	window := make([]string, visible)
	copy(window, rows[first:first+visible])

	if first > 0 && first != selected {
		window[0] = fmt.Sprintf("▲ %d more", first+1)
	}
	if last := first + visible - 1; last < n-1 && last != selected {
		window[visible-1] = fmt.Sprintf("▼ %d more", n-last)
	}
	return window
}

func styleSelected(line string, isSelected bool, opts Options) string {
	if !isSelected || opts.NoColor {
		return line
	}
	return "\x1b[7m" + line + "\x1b[0m"
}

func sessionsHeader() string {
	return fmt.Sprintf("%-*s %-*s %-*s %-*s %-*s %-*s %-*s %-*s %-*s %3s %3s %5s %6s %6s",
		wStatus, "STATUS", wPID, "PID", wAgent, "AGENT", wModel, "MODEL", wCWD, "CWD", wCTX, "CTX",
		wTok, "IN/OUT/CACHE", wCost, "$", wBurn, "$/HR", "TL", "SA", "CPU%", "GPU/s", "RSS")
}

func sessionRow(r theme.Roles, s domain.Session, at time.Time, opts Options) string {
	// Every cell is assembled plain and padded to its column width with
	// padLine (which measures visible width via lipgloss.Width) before any
	// ANSI styling is applied. Padding a styled cell with a fmt width verb
	// counts escape bytes as columns and silently produces a short cell,
	// shifting every column to its right.
	statusColor, statusLabel := statusInfo(r, s, at)
	status := styled(opts, statusColor, padLine(truncate(statusLabel, wStatus), wStatus))

	pid := unknownPID
	if s.BindConf != "unknown" {
		if s.PID != nil {
			pid = fmt.Sprintf("pid %d", *s.PID)
		} else {
			pid = "—"
		}
	}
	pid = padLine(pid, wPID)

	agent := s.Agent
	model := truncate(s.Model, wModel)
	if s.Kind != "" && s.Kind != "interactive" {
		// A background claude -p / sdk-cli run must never look like the
		// session a person is typing into: mute the identity columns
		// rather than let a $9/hr Nightshift run blend in with the row
		// beside it.
		agent = styled(opts, r.Muted, padLine(agent+"*", wAgent))
		model = styled(opts, r.Muted, padLine(model, wModel))
	} else {
		agent = padLine(agent, wAgent)
		model = padLine(model, wModel)
	}

	cwd := padLine(shortenLeft(s.CWD, wCWD), wCWD)
	ctx := ctxGauge(r, s, opts)
	tok := padLine(fmt.Sprintf("%s/%s/%s", formatTokens(s.Usage.Input), formatTokens(s.Usage.Output), formatTokens(s.Usage.CacheRead+s.Usage.CacheCreate5m+s.Usage.CacheCreate1h)), wTok)

	cost := "$—"
	if s.Priced && s.CostUSD != nil {
		cost = fmt.Sprintf("$%.2f", *s.CostUSD)
	}
	cost = padLine(cost, wCost)
	burn := "—"
	if s.BurnUSDPerHr != nil {
		burn = fmt.Sprintf("$%.2f/hr", *s.BurnUSDPerHr)
	}
	burn = padLine(burn, wBurn)

	cpu, gpu, rss := "—", "—", "—"
	if s.BindConf != "unknown" && s.Proc != nil {
		cpu = fmt.Sprintf("%.1f%%", s.Proc.CPUPct)
		gpu = fdash(s.Proc.GPUMsPerSec, "%.1f")
		rss = fmt.Sprintf("%.0fM", float64(s.Proc.RSSBytes)/1e6)
	}

	return fmt.Sprintf("%s %s %s %s %s %s %s %s %s %3d %3d %5s %6s %6s",
		status, pid, agent, model, cwd, ctx, tok, cost, burn, len(s.Tools), len(s.Subagents), cpu, gpu, rss)
}

// subagentRow renders one child row, indented under its parent.
// AgentType/Description identify it, Model is the resolved (not requested)
// model id, and Live/finished state and cost round it out. An unpriced
// subagent model renders "$—", matching the parent's own rule.
func subagentRow(r theme.Roles, sa *domain.Subagent, opts Options) string {
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
	label := styled(opts, color, padLine(state, 11))
	return fmt.Sprintf("  %s %s %s %s %s %s %s %s %s",
		label, padLine("", wPID), padLine(sa.AgentType, wAgent), padLine(truncate(sa.Model, wModel), wModel),
		padLine(truncate(sa.Description, wCWD), wCWD), padLine("", wCTX), padLine("", wTok), padLine(cost, wCost), padLine("", wBurn))
}

// statusInfo maps a Session.Status to its severity color and display
// label. All five spec'd values render distinctly; a status the renderer
// does not recognise falls through to Muted and the literal string, never
// a blank cell.
func statusInfo(r theme.Roles, s domain.Session, at time.Time) (color, label string) {
	switch s.Status {
	case "busy":
		return r.Busy, spinnerGlyph + " Busy"
	case "waiting":
		return r.Waiting, "Waiting"
	case "rate-limited":
		return r.Warn, "RateLimited"
	case "unknown":
		return r.Muted, "Unknown"
	case "stale":
		age := "—"
		if !s.StatusSince.IsZero() && !at.IsZero() {
			age = formatAge(at.Sub(s.StatusSince))
		}
		return r.Muted, "Stale " + age
	default:
		return r.Muted, s.Status
	}
}

func formatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

// ctxGauge renders the context-fill bar. ContextExact == false (Claude)
// carries a "~" prefix and a dashed bar edge; ContextExact == true (Codex)
// is unmarked. One uniformly-wrong gauge is worse than two honestly
// labelled ones.
func ctxGauge(r theme.Roles, s domain.Session, opts Options) string {
	pct := 0.0
	if s.ContextMax > 0 {
		pct = float64(s.ContextUsed) / float64(s.ContextMax) * 100
	}
	bar := Bar(r, pct, ctxBarWidth, r.Severity(pct, 70, 90), opts.NoColor)

	left, right, marker := "[", "]", ""
	if !s.ContextExact {
		left, right, marker = "┊", "┊", "~"
	}
	// bar already carries ANSI fill/empty styling; padLine measures its
	// visible width via lipgloss.Width, so padding here (rather than with
	// a fmt width verb) stays correct regardless of that styling.
	return padLine(fmt.Sprintf("%s%s%s%s%3.0f%%", marker, left, bar, right, pct), wCTX)
}

// formatTokens renders a token count compactly: below 1000 as-is, at or
// above 1000 as "N.Nk"/"N.NM"/"N.NG".
func formatTokens(n int64) string {
	return humanCount(n)
}

// humanCount renders a raw count with a k/M/G suffix once it clears 1000,
// keeping one decimal place at every scale -- a 6-8 digit raw number (token
// counts, cache-read counters) is the widest, least readable cell in the
// sessions table and overruns its column.
func humanCount(n int64) string {
	neg := ""
	if n < 0 {
		neg = "-"
		n = -n
	}
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%s%.1fG", neg, float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%s%.1fM", neg, float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%s%.1fk", neg, float64(n)/1e3)
	default:
		return fmt.Sprintf("%s%d", neg, n)
	}
}

// humanBytes renders a byte count with a KB/MB/GB suffix once it clears
// 1000, one decimal place at every scale -- used wherever a raw byte
// counter (disk cumulative r/w) would otherwise render as an 8-9 digit
// integer.
func humanBytes(n uint64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fGB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fMB", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%.1fKB", float64(n)/1e3)
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// truncate cuts s to at most n runes, marking the cut with a trailing "…".
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// shortenLeft cuts from the left (keeping the tail, which usually carries
// the project name) when s is longer than n runes, marking the cut with a
// leading "…" -- a cwd's most identifying part is its last component.
func shortenLeft(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return "…" + string(r[len(r)-(n-1):])
}
