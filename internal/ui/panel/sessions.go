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
func SessionsRender(sessions []domain.Session, r theme.Roles, width, height, selected int, at time.Time, opts Options) string {
	lines := []string{sessionsHeader()}

	row := 0
	for _, s := range sessions {
		lines = append(lines, styleSelected(sessionRow(r, s, at, opts), row == selected, opts))
		row++
		for i := range s.Subagents {
			lines = append(lines, styleSelected(subagentRow(r, &s.Subagents[i], opts), row == selected, opts))
			row++
		}
	}

	return frame(lines, width, height)
}

func styleSelected(line string, isSelected bool, opts Options) string {
	if !isSelected || opts.NoColor {
		return line
	}
	return "\x1b[7m" + line + "\x1b[0m"
}

func sessionsHeader() string {
	return fmt.Sprintf("%-16s %-13s %-6s %-14s %-16s %-16s %-18s %-7s %-8s %3s %3s %5s %6s %6s",
		"STATUS", "PID", "AGENT", "MODEL", "CWD", "CTX", "IN/OUT/CACHE", "$", "$/HR", "TL", "SA", "CPU%", "GPU/s", "RSS")
}

func sessionRow(r theme.Roles, s domain.Session, at time.Time, opts Options) string {
	statusColor, statusLabel := statusInfo(r, s, at)
	status := styled(opts, statusColor, fmt.Sprintf("%-16s", truncate(statusLabel, 16)))

	pid := unknownPID
	if s.BindConf != "unknown" {
		if s.PID != nil {
			pid = fmt.Sprintf("pid %d", *s.PID)
		} else {
			pid = "—"
		}
	}

	agent := s.Agent
	model := truncate(s.Model, 14)
	if s.Kind != "" && s.Kind != "interactive" {
		// A background claude -p / sdk-cli run must never look like the
		// session a person is typing into: mute the identity columns
		// rather than let a $9/hr Nightshift run blend in with the row
		// beside it.
		agent = styled(opts, r.Muted, agent+"*")
		model = styled(opts, r.Muted, model)
	}

	cwd := shortenLeft(s.CWD, 16)
	ctx := ctxGauge(r, s, opts)
	tok := fmt.Sprintf("%s/%s/%s", formatTokens(s.Usage.Input), formatTokens(s.Usage.Output), formatTokens(s.Usage.CacheRead+s.Usage.CacheCreate5m+s.Usage.CacheCreate1h))

	cost := "$—"
	if s.Priced && s.CostUSD != nil {
		cost = fmt.Sprintf("$%.2f", *s.CostUSD)
	}
	burn := "—"
	if s.BurnUSDPerHr != nil {
		burn = fmt.Sprintf("$%.2f/hr", *s.BurnUSDPerHr)
	}

	cpu, gpu, rss := "—", "—", "—"
	if s.BindConf != "unknown" && s.Proc != nil {
		cpu = fmt.Sprintf("%.1f%%", s.Proc.CPUPct)
		gpu = fdash(s.Proc.GPUMsPerSec, "%.1f")
		rss = fmt.Sprintf("%.0fM", float64(s.Proc.RSSBytes)/1e6)
	}

	return fmt.Sprintf("%s %-13s %-6s %-14s %-16s %-16s %-18s %-7s %-8s %3d %3d %5s %6s %6s",
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
	label := styled(opts, color, fmt.Sprintf("%-11s", state))
	return fmt.Sprintf("  %s %-13s %-6s %-14s %-16s %-16s %-18s %-7s %-8s",
		label, "", sa.AgentType, truncate(sa.Model, 14), truncate(sa.Description, 16), "", "", cost, "")
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
	return fmt.Sprintf("%s%s%s%s%3.0f%%", marker, left, bar, right, pct)
}

// formatTokens renders a token count compactly: below 1000 as-is, at or
// above 1000 as "N.Nk".
func formatTokens(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
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
