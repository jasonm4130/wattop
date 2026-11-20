package panel

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

const spinnerGlyph = "⠋"

const ctxBarWidth = 10

const (
	wStatus = 16
	wPID    = 13
	wAgent  = 6
	wModel  = 14
	wCWD    = 16
	wCTX    = 18
	wRate   = 7
	wCost   = 8
	wBurn   = 8
	wCPU    = 5
	wRSS    = 6
	wSA     = 7
)

const colFloor = 5

type sessionCols struct {
	Status, PID, Model, CWD, Ctx, Rate int
	Agent, Cost, Burn, CPU, RSS        int
	ShowTL, ShowSA, ShowGPU            bool
}

func computeSessionCols(width int) sessionCols {
	c := sessionCols{
		Status: wStatus, PID: wPID, Model: wModel, CWD: wCWD, Ctx: wCTX, Rate: wRate,
		Agent: wAgent, Cost: wCost, Burn: wBurn, CPU: wCPU, RSS: wRSS,
		ShowTL: true, ShowSA: true, ShowGPU: true,
	}
	budget := width - 2

	total := func() int {
		cols := []int{c.Status, c.PID, c.Agent, c.Model, c.CWD, c.Ctx, c.Rate, c.Cost, c.Burn, c.CPU, c.RSS}
		if c.ShowTL {
			cols = append(cols, 3)
		}
		if c.ShowSA {
			cols = append(cols, wSA)
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
	shrink(&c.Rate)
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

const unknownPID = "(pid unknown)"

func SessionsRender(sessions []domain.Session, r theme.Roles, width, height, selected int, at time.Time, opts Options) string {
	cols := computeSessionCols(width)
	rows := sessionRows(sessions, r, at, opts, selected, cols)
	rows = windowRows(rows, height-1, selected)

	if len(sessions) == 0 && height > 2 {
		rows = []string{"", styled(opts, r.Muted, "  No visible sessions · a shows dormant sessions")}
	}
	lines := append([]string{styled(opts, r.Muted, sessionsHeader(cols))}, rows...)
	return frame(lines, width, height)
}

func sessionRows(sessions []domain.Session, r theme.Roles, at time.Time, opts Options, selected int, cols sessionCols) []string {
	var lines []string
	row := 0
	for _, s := range sessions {
		isSel := row == selected
		lines = append(lines, styleSelected(sessionRow(r, s, at, plainIfSelected(opts, isSel), cols), isSel, r, opts))
		row++
		for _, c := range ChildRows(s) {
			isSel = row == selected
			o := plainIfSelected(opts, isSel)
			var line string
			if c.Workflow != nil {
				line = workflowRow(r, c.Workflow, o, cols)
			} else {
				line = subagentRow(r, c.Subagent, c.Depth, o, cols)
			}
			lines = append(lines, styleSelected(line, isSel, r, opts))
			row++
		}
	}
	return lines
}

// plainIfSelected forces NoColor for a selected row's own cell rendering,
// so styleSelected's whole-line selection wrap has plain, escape-free
// text to enclose. Wrapping already-ANSI-styled cells (each ending in its
// own reset) in a second style collapses that outer style at the first
// inner reset instead of surviving to the end of the line.
func plainIfSelected(opts Options, isSelected bool) Options {
	if isSelected {
		opts.NoColor = true
	}
	return opts
}

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

const selectionGutter = "▸ "

func styleSelected(line string, isSelected bool, r theme.Roles, opts Options) string {
	gutter := "  "
	if isSelected {
		gutter = selectionGutter
	}
	line = gutter + line

	if !isSelected || opts.NoColor {
		return line
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(r.Foreground)).Background(lipgloss.Color(r.Border)).Bold(true).Render(line)
}

func sessionsHeader(cols sessionCols) string {
	parts := []string{
		padLine(truncate("STATUS", cols.Status), cols.Status),
		padLine(truncate("PID", cols.PID), cols.PID),
		padLine(truncate("AGENT", cols.Agent), cols.Agent),
		padLine(truncate("MODEL", cols.Model), cols.Model),
		padLine(truncate("CWD", cols.CWD), cols.CWD),
		padLine(truncate("CTX", cols.Ctx), cols.Ctx),
		padLine(truncate("OUT/s", cols.Rate), cols.Rate),
		padLine(truncate("$", cols.Cost), cols.Cost),
		padLine(truncate("$/HR", cols.Burn), cols.Burn),
	}
	if cols.ShowTL {
		parts = append(parts, "TL")
	}
	if cols.ShowSA {
		parts = append(parts, padLine("SA", wSA))
	}
	parts = append(parts, padLine(truncate("CPU%", cols.CPU), cols.CPU))
	if cols.ShowGPU {
		parts = append(parts, "GPU/s")
	}
	parts = append(parts, padLine(truncate("RSS", cols.RSS), cols.RSS))
	return "  " + strings.Join(parts, " ")
}

func sessionRow(r theme.Roles, s domain.Session, at time.Time, opts Options, cols sessionCols) string {
	statusColor, statusLabel := statusInfo(r, s, at)
	status := styled(opts, statusColor, padLine(truncate(statusLabel, cols.Status), cols.Status))

	pid := padLine(pidCell(s, cols.PID), cols.PID)

	agent := s.Agent
	model := truncate(modelLabel(s.Model), cols.Model)
	if s.Kind != "" && s.Kind != "interactive" {
		agent = styled(opts, r.Muted, padLine(truncate(agent+"*", cols.Agent), cols.Agent))
		model = styled(opts, r.Muted, padLine(model, cols.Model))
	} else {
		agent = padLine(truncate(agent, cols.Agent), cols.Agent)
		model = padLine(model, cols.Model)
	}

	cwd := padLine(shortenLeft(s.CWD, cols.CWD), cols.CWD)
	ctx := ctxGauge(r, s, opts, cols.Ctx)
	rate := tokenRateCell(s.TokenRate, cols.Rate)

	cost := "$—"
	if s.Priced && s.CostUSD != nil {
		cost = fmt.Sprintf("$%.2f", *s.CostUSD)
	}
	if s.CostPartial {
		cost = "~" + cost
	}
	cost = padLine(truncate(cost, cols.Cost), cols.Cost)
	burn := "—"
	if s.BurnUSDPerHr != nil {
		burn = fmt.Sprintf("$%.2f", *s.BurnUSDPerHr)
	}
	burn = padLine(truncate(burn, cols.Burn), cols.Burn)

	cpu, gpu, rss := "—", "—", "—"
	if s.HasLiveProcess() {
		cpu = fdash(s.Proc.CPUPct, "%.1f%%")
		gpu = fdash(s.Proc.GPUMsPerSec, "%.1f")
		rss = fmt.Sprintf("%.0fM", float64(s.Proc.RSSBytes)/1e6)
	}

	parts := []string{status, pid, agent, model, cwd, ctx, rate, cost, burn}
	if cols.ShowTL {
		parts = append(parts, fmt.Sprintf("%3d", len(s.Tools)))
	}
	if cols.ShowSA {
		parts = append(parts, fmt.Sprintf("%*s", wSA, truncate(subagentCounts(s.Subagents), wSA)))
	}
	parts = append(parts, padLine(truncate(cpu, cols.CPU), cols.CPU))
	if cols.ShowGPU {
		parts = append(parts, fmt.Sprintf("%6s", gpu))
	}
	parts = append(parts, padLine(truncate(rss, cols.RSS), cols.RSS))
	return strings.Join(parts, " ")
}

func modelLabel(model string) string {
	if model == "" {
		return "—"
	}
	return model
}

func pidCell(s domain.Session, width int) string {
	verbose := unknownPID
	if s.BindConf != domain.BindUnknown {
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
	if s.BindConf != domain.BindUnknown && s.PID != nil {
		compact = fmt.Sprintf("%d", *s.PID)
	}
	return truncate(compact, width)
}

func tokenRateCell(rate *domain.TokenRate, width int) string {
	text := "—"
	if rate != nil {
		text = fmt.Sprintf("%.1f", rate.OutputPerSec)
		if len(text) > width {
			text = humanCount(int64(rate.OutputPerSec))
		}
	}
	return padLine(truncate(text, width), width)
}

func subagentCounts(subagents []domain.Subagent) string {
	running := 0
	for i := range subagents {
		if childRunning(subagents[i].Status, subagents[i].Live) {
			running++
		}
	}
	return fmt.Sprintf("%d/%d", running, len(subagents))
}

func childStatusCell(opts Options, color, label string, depth, width int) string {
	indent := 2 + 2*depth
	if limit := width - 4; indent > limit {
		indent = max(0, limit)
	}
	return styled(opts, color, padLine(strings.Repeat(" ", indent)+truncate(label, width-indent), width))
}

func childTailCells(cols sessionCols) []string {
	var parts []string
	if cols.ShowTL {
		parts = append(parts, padLine("", 3))
	}
	if cols.ShowSA {
		parts = append(parts, padLine("", wSA))
	}
	parts = append(parts, padLine("", cols.CPU))
	if cols.ShowGPU {
		parts = append(parts, padLine("", 6))
	}
	return append(parts, padLine("", cols.RSS))
}

func subagentRow(r theme.Roles, sa *domain.Subagent, depth int, opts Options, cols sessionCols) string {
	color, state := childStatus(r, sa.Status, sa.Live)
	label := childStatusCell(opts, color, state, depth, cols.Status)

	agent := truncate(sa.AgentType, cols.Agent)
	if sa.Background {
		agent = truncate(sa.AgentType, cols.Agent-1) + "⇢"
	}
	desc := sa.Description
	if sa.CurrentTool != "" && childRunning(sa.Status, sa.Live) {
		desc = "▸ " + sa.CurrentTool + " · " + desc
	}
	parts := []string{
		label, padLine("", cols.PID), padLine(agent, cols.Agent),
		padLine(truncate(sa.Model, cols.Model), cols.Model), padLine(truncate(desc, cols.CWD), cols.CWD),
		padLine("", cols.Ctx), tokenRateCell(sa.TokenRate, cols.Rate),
		padLine(truncate(childCost(sa.CostUSD, sa.CostPartial), cols.Cost), cols.Cost),
		padLine(truncate(childBurn(sa.BurnUSDPerHr), cols.Burn), cols.Burn),
	}
	return strings.Join(append(parts, childTailCells(cols)...), " ")
}

func workflowRow(r theme.Roles, w *domain.Workflow, opts Options, cols sessionCols) string {
	color, state := childStatus(r, w.Status, false)
	label := childStatusCell(opts, color, "wf "+state, 0, cols.Status)

	summary := shortWorkflowID(w.ID)
	if w.Phase != "" {
		summary += " · " + w.Phase
	}
	summary += " · " + workflowCounts(w)
	parts := []string{
		label, padLine("", cols.PID), padLine(truncate("workflow", cols.Agent), cols.Agent),
		padLine("", cols.Model), padLine(truncate(summary, cols.CWD), cols.CWD),
		padLine("", cols.Ctx), tokenRateCell(w.TokenRate, cols.Rate),
		padLine(truncate(childCost(w.CostUSD, w.CostPartial), cols.Cost), cols.Cost),
		padLine(truncate(childBurn(w.BurnUSDPerHr), cols.Burn), cols.Burn),
	}
	return strings.Join(append(parts, childTailCells(cols)...), " ")
}

func statusInfo(r theme.Roles, s domain.Session, at time.Time) (color, label string) {
	switch s.Status {
	case domain.StatusBusy:
		return r.Busy, spinnerGlyph + " Busy"
	case domain.StatusWaiting:
		return r.Waiting, "Waiting"
	case domain.StatusRateLimited:
		return r.Warn, "RateLimited"
	case domain.StatusUnknown:
		return r.Muted, "Unknown"
	case domain.StatusStale:
		age := "—"
		if !s.StatusSince.IsZero() && !at.IsZero() {
			age = formatAge(at.Sub(s.StatusSince))
		}
		return r.Muted, "Stale " + age
	default:
		return r.Muted, string(s.Status)
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
		return padLine(fmt.Sprintf("%s%s%s%s%s", marker, left, bar, right, pctText), width)
	}
	cell := truncate(marker+plainPct, width)
	if pct > 100 {
		cell = styled(opts, r.Hot, cell)
	}
	return padLine(cell, width)
}

func formatTokens(n int64) string {
	return humanCount(n)
}

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
