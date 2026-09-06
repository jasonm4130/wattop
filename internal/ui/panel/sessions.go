package panel

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// spinnerGlyph is a single static braille frame. The animation itself lives
// in model.go's tick-driven redraw (a Snapshot carries no frame counter);
// this is the glyph a busy row renders on any given frame.
const spinnerGlyph = "⠋"

const ctxBarWidth = 10

// Base column widths -- the layout used when width comfortably fits every
// column. wCTX is 18, not 16: the gauge's widest form (marker + bracket +
// ctxBarWidth-wide bar + bracket + " NNN%") is 18 visible columns, and a
// header slot narrower than its widest row content misaligns every column
// to its right. computeSessionCols shrinks (or, given extra room, grows)
// these per render; sessionsHeader and sessionRow/subagentRow always read
// widths from its result, never these constants directly, so header and
// row can never drift out of alignment with each other.
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
	wCPU    = 5
	wRSS    = 6
)

// colFloor is the narrowest any flexible column is shrunk to before it
// switches to a degraded-but-legible form (a bare pid number instead of
// "pid 1234", a percentage instead of a context bar, a single token total
// instead of the in/out/cache triple) rather than losing more characters.
const colFloor = 6

// sessionCols is the per-render column-width plan computed from the
// available frame width. STATUS/PID/MODEL/CWD/CTX/TOK are flexible: they
// shrink toward colFloor, in priority order, only as far as the frame
// forces, and grow past their base width to spend any leftover space
// (CWD first, then MODEL) rather than leaving it as blank trailing
// columns. AGENT, COST, BURN, CPU and RSS never shrink -- $/$/HR/CPU% are
// exactly the figures a narrow terminal must still show in full, and
// AGENT/RSS are already at their minimum useful width.
type sessionCols struct {
	Status, PID, Model, CWD, Ctx, Tok int
	Agent, Cost, Burn, CPU, RSS       int
	ShowTL, ShowSA, ShowGPU           bool
}

// computeSessionCols fits the table into width display columns, minus the
// 2-column selection gutter every row carries (see styleSelected). A full
// table (all 14 columns at base width) needs 154 columns; short of that,
// TL, SA and GPU/s -- the three lowest-value columns -- drop first, in
// that order. If the table still does not fit, the flexible columns
// shrink toward colFloor in priority order -- TOK and CTX first, since
// both degrade to a shorter but still meaningful form, then MODEL, CWD,
// PID and STATUS last, since truncating an identity column costs more
// than a percentage losing its bar. This is what keeps $, $/HR and CPU%
// on screen at 80 columns and gives 200 columns' worth of slack to CWD
// (up to +40) and then MODEL instead of leaving it as dead space.
func computeSessionCols(width int) sessionCols {
	c := sessionCols{
		Status: wStatus, PID: wPID, Model: wModel, CWD: wCWD, Ctx: wCTX, Tok: wTok,
		Agent: wAgent, Cost: wCost, Burn: wBurn, CPU: wCPU, RSS: wRSS,
		ShowTL: true, ShowSA: true, ShowGPU: true,
	}
	budget := width - 2

	total := func() int {
		cols := []int{c.Status, c.PID, c.Agent, c.Model, c.CWD, c.Ctx, c.Tok, c.Cost, c.Burn, c.CPU, c.RSS}
		if c.ShowTL {
			cols = append(cols, 3)
		}
		if c.ShowSA {
			cols = append(cols, 3)
		}
		if c.ShowGPU {
			cols = append(cols, 6)
		}
		sum := 0
		for _, w := range cols {
			sum += w
		}
		return sum + (len(cols) - 1)
	}

	if total() > budget && c.ShowTL {
		c.ShowTL = false
	}
	if total() > budget && c.ShowSA {
		c.ShowSA = false
	}
	if total() > budget && c.ShowGPU {
		c.ShowGPU = false
	}

	shrink := func(field *int) {
		over := total() - budget
		if over <= 0 || *field <= colFloor {
			return
		}
		cut := over
		if room := *field - colFloor; cut > room {
			cut = room
		}
		*field -= cut
	}
	shrink(&c.Tok)
	shrink(&c.Ctx)
	shrink(&c.Model)
	shrink(&c.CWD)
	shrink(&c.PID)
	shrink(&c.Status)

	if slack := budget - total(); slack > 0 {
		cwdBonus := slack
		if cwdBonus > 40 {
			cwdBonus = 40
		}
		c.CWD += cwdBonus
		c.Model += slack - cwdBonus
	}

	return c
}

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
	cols := computeSessionCols(width)
	rows := sessionRows(sessions, r, at, opts, selected, cols)
	rows = windowRows(rows, height-1, selected)

	lines := append([]string{sessionsHeader(cols)}, rows...)
	return frame(lines, width, height)
}

// sessionRows flattens sessions (and their subagents) into one
// already-styled line per row, in flattened-row order.
func sessionRows(sessions []domain.Session, r theme.Roles, at time.Time, opts Options, selected int, cols sessionCols) []string {
	var lines []string
	row := 0
	for _, s := range sessions {
		isSel := row == selected
		lines = append(lines, styleSelected(sessionRow(r, s, at, plainIfSelected(opts, isSel), cols), isSel, opts))
		row++
		for i := range s.Subagents {
			isSel = row == selected
			lines = append(lines, styleSelected(subagentRow(r, &s.Subagents[i], plainIfSelected(opts, isSel), cols), isSel, opts))
			row++
		}
	}
	return lines
}

// plainIfSelected forces NoColor for a selected row's own cell rendering,
// so styleSelected's whole-line reverse-video wrap has plain, escape-free
// text to enclose. Wrapping already-ANSI-styled cells (each ending in its
// own reset) in a second style collapses that outer style at the first
// inner reset instead of surviving to the end of the line.
func plainIfSelected(opts Options, isSelected bool) Options {
	if isSelected {
		opts.NoColor = true
	}
	return opts
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

// selectionGutter marks the selected row with "▸ " (and every other row
// with "  " in its place) so the current selection stays visible even under
// NO_COLOR or when piped to a colorless terminal -- not just via the
// reverse-video styling layered on top of it in color mode.
const selectionGutter = "▸ "

func styleSelected(line string, isSelected bool, opts Options) string {
	gutter := "  "
	if isSelected {
		gutter = selectionGutter
	}
	line = gutter + line

	if !isSelected || opts.NoColor {
		return line
	}
	// line was built via plainIfSelected, so it carries no embedded ANSI of
	// its own -- this is the only style applied to it, and it covers every
	// cell all the way to the end of the line. Wrapping an already-styled
	// line (the old \x1b[7m...\x1b[0m hand-rolled wrapper, and an
	// equally-broken lipgloss Render() over pre-styled cells) collapses the
	// reverse attribute at the first embedded reset instead.
	return lipgloss.NewStyle().Reverse(true).Render(line)
}

func sessionsHeader(cols sessionCols) string {
	parts := []string{
		padLine(truncate("STATUS", cols.Status), cols.Status),
		padLine(truncate("PID", cols.PID), cols.PID),
		padLine(truncate("AGENT", cols.Agent), cols.Agent),
		padLine(truncate("MODEL", cols.Model), cols.Model),
		padLine(truncate("CWD", cols.CWD), cols.CWD),
		padLine(truncate("CTX", cols.Ctx), cols.Ctx),
		padLine(truncate("TOK", cols.Tok), cols.Tok),
		padLine(truncate("$", cols.Cost), cols.Cost),
		padLine(truncate("$/HR", cols.Burn), cols.Burn),
	}
	if cols.ShowTL {
		parts = append(parts, "TL")
	}
	if cols.ShowSA {
		parts = append(parts, "SA")
	}
	parts = append(parts, padLine(truncate("CPU%", cols.CPU), cols.CPU))
	if cols.ShowGPU {
		parts = append(parts, "GPU/s")
	}
	parts = append(parts, padLine(truncate("RSS", cols.RSS), cols.RSS))
	return "  " + strings.Join(parts, " ")
}

func sessionRow(r theme.Roles, s domain.Session, at time.Time, opts Options, cols sessionCols) string {
	// Every cell is assembled plain and padded to its column width with
	// padLine (which measures visible width via lipgloss.Width) before any
	// ANSI styling is applied. Padding a styled cell with a fmt width verb
	// counts escape bytes as columns and silently produces a short cell,
	// shifting every column to its right.
	statusColor, statusLabel := statusInfo(r, s, at)
	status := styled(opts, statusColor, padLine(truncate(statusLabel, cols.Status), cols.Status))

	pid := padLine(pidCell(s, cols.PID), cols.PID)

	agent := s.Agent
	model := truncate(s.Model, cols.Model)
	if s.Kind != "" && s.Kind != "interactive" {
		// A background claude -p / sdk-cli run must never look like the
		// session a person is typing into: mute the identity columns
		// rather than let a $9/hr Nightshift run blend in with the row
		// beside it.
		agent = styled(opts, r.Muted, padLine(truncate(agent+"*", cols.Agent), cols.Agent))
		model = styled(opts, r.Muted, padLine(model, cols.Model))
	} else {
		agent = padLine(truncate(agent, cols.Agent), cols.Agent)
		model = padLine(model, cols.Model)
	}

	cwd := padLine(shortenLeft(s.CWD, cols.CWD), cols.CWD)
	ctx := ctxGauge(r, s, opts, cols.Ctx)
	tok := tokCell(s, cols.Tok)

	cost := "$—"
	if s.Priced && s.CostUSD != nil {
		cost = fmt.Sprintf("$%.2f", *s.CostUSD)
	}
	cost = padLine(truncate(cost, cols.Cost), cols.Cost)
	burn := "—"
	if s.BurnUSDPerHr != nil {
		burn = fmt.Sprintf("$%.2f/hr", *s.BurnUSDPerHr)
	}
	burn = padLine(truncate(burn, cols.Burn), cols.Burn)

	cpu, gpu, rss := "—", "—", "—"
	if s.BindConf != "unknown" && s.Proc != nil {
		cpu = fmt.Sprintf("%.1f%%", s.Proc.CPUPct)
		gpu = fdash(s.Proc.GPUMsPerSec, "%.1f")
		rss = fmt.Sprintf("%.0fM", float64(s.Proc.RSSBytes)/1e6)
	}

	parts := []string{status, pid, agent, model, cwd, ctx, tok, cost, burn}
	if cols.ShowTL {
		parts = append(parts, fmt.Sprintf("%3d", len(s.Tools)))
	}
	if cols.ShowSA {
		parts = append(parts, fmt.Sprintf("%3d", len(s.Subagents)))
	}
	parts = append(parts, padLine(truncate(cpu, cols.CPU), cols.CPU))
	if cols.ShowGPU {
		parts = append(parts, fmt.Sprintf("%6s", gpu))
	}
	parts = append(parts, padLine(truncate(rss, cols.RSS), cols.RSS))
	return strings.Join(parts, " ")
}

// pidCell renders the pid cell, keyed on BindConf == "unknown" (never on
// Proc == nil -- see unknownPID). At full width it is the verbose
// "pid 1234" / "(pid unknown)" / "—" form; when width is too narrow for
// that (a compact terminal drops to it, see computeSessionCols), it falls
// back to the bare number -- the minimum that still identifies the row.
func pidCell(s domain.Session, width int) string {
	verbose := unknownPID
	if s.BindConf != "unknown" {
		if s.PID != nil {
			verbose = fmt.Sprintf("pid %d", *s.PID)
		} else {
			verbose = "—"
		}
	}
	if len([]rune(verbose)) <= width {
		return verbose
	}
	compact := "—"
	if s.BindConf != "unknown" && s.PID != nil {
		compact = fmt.Sprintf("%d", *s.PID)
	}
	return truncate(compact, width)
}

// tokCell renders the token cell. At full width it is the in/out/cache
// triple; when width is too narrow to hold that (a compact terminal, see
// computeSessionCols), it falls back to a single combined total rather
// than silently overflowing its column.
func tokCell(s domain.Session, width int) string {
	triple := fmt.Sprintf("%s/%s/%s", formatTokens(s.Usage.Input), formatTokens(s.Usage.Output),
		formatTokens(s.Usage.CacheRead+s.Usage.CacheCreate5m+s.Usage.CacheCreate1h))
	if len([]rune(triple)) <= width {
		return padLine(triple, width)
	}
	total := s.Usage.Input + s.Usage.Output + s.Usage.CacheRead + s.Usage.CacheCreate5m + s.Usage.CacheCreate1h
	return padLine(truncate(formatTokens(total), width), width)
}

// subagentRow renders one child row, indented under its parent.
// AgentType/Description identify it, Model is the resolved (not requested)
// model id, and Live/finished state and cost round it out. An unpriced
// subagent model renders "$—", matching the parent's own rule.
func subagentRow(r theme.Roles, sa *domain.Subagent, opts Options, cols sessionCols) string {
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
	// The two-space indent lives inside the STATUS cell itself (rather than
	// as a separate prefix) so an indented child row costs exactly the same
	// total width as a parent row -- both are built from the same cols,
	// and a prefix on top of that would silently push every subagent row
	// two columns past whatever computeSessionCols budgeted for width.
	const indent = "  "
	label := styled(opts, color, padLine(indent+truncate(state, cols.Status-len(indent)), cols.Status))
	parts := []string{
		label, padLine("", cols.PID), padLine(truncate(sa.AgentType, cols.Agent), cols.Agent),
		padLine(truncate(sa.Model, cols.Model), cols.Model), padLine(truncate(sa.Description, cols.CWD), cols.CWD),
		padLine("", cols.Ctx), padLine("", cols.Tok), padLine(truncate(cost, cols.Cost), cols.Cost), padLine("", cols.Burn),
	}
	if cols.ShowTL {
		parts = append(parts, padLine("", 3))
	}
	if cols.ShowSA {
		parts = append(parts, padLine("", 3))
	}
	parts = append(parts, padLine("", cols.CPU))
	if cols.ShowGPU {
		parts = append(parts, padLine("", 6))
	}
	parts = append(parts, padLine("", cols.RSS))
	return strings.Join(parts, " ")
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
//
// Bar() clamps its fill at 100%, so a session over its window (a live
// Codex row can read 473% -- see the parser-side note on ContextUsed)
// rendered a saturated bar next to a three-digit, unclamped percentage:
// the two disagreed, and every saturated row looked identical regardless
// of how far over budget it was. Past 100%, render the literal ">100%"
// in r.Hot instead of the raw number: it stays honest about being over
// scale (unlike silently clamping the number to "100%", which would read
// as exactly full) while agreeing with the bar's own saturation.
func ctxGauge(r theme.Roles, s domain.Session, opts Options, width int) string {
	pct := 0.0
	if s.ContextMax > 0 {
		pct = float64(s.ContextUsed) / float64(s.ContextMax) * 100
	}

	marker := ""
	if !s.ContextExact {
		marker = "~"
	}
	plainPct := fmt.Sprintf("%3.0f%%", pct)
	if pct > 100 {
		plainPct = ">100%"
	}

	// A gauge's fixed overhead -- marker + both brackets + " NNN%" -- is 8
	// columns wide (see wCTX's doc comment), so a bar only fits above that;
	// below it (computeSessionCols has shrunk cols.Ctx toward colFloor for
	// a narrow terminal) there is no room for a bracketed bar at all, and
	// the cell degrades to the bare, still-honest percentage rather than
	// overflowing its column.
	if barWidth := width - 8; barWidth >= 1 {
		bar := Bar(r, pct, barWidth, r.Severity(pct, 70, 90), opts.NoColor)
		left, right := "[", "]"
		if !s.ContextExact {
			left, right = "┊", "┊"
		}
		pctText := plainPct
		if pct > 100 {
			pctText = styled(opts, r.Hot, plainPct)
		}
		// bar already carries ANSI fill/empty styling; padLine measures its
		// visible width via lipgloss.Width, so padding here (rather than
		// with a fmt width verb) stays correct regardless of that styling.
		return padLine(fmt.Sprintf("%s%s%s%s%s", marker, left, bar, right, pctText), width)
	}
	// Truncate the plain (unstyled) text first -- cutting a styled string
	// by rune count risks slicing an embedded ANSI escape in two -- then
	// style the already-final-width result.
	cell := truncate(marker+plainPct, width)
	if pct > 100 {
		cell = styled(opts, r.Hot, cell)
	}
	return padLine(cell, width)
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
