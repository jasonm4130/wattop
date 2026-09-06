package panel

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// estimateCaveat is permanent UI, not a README line: a Max-plan subscriber
// pays nothing marginal for what a naive dashboard renders as $12/hr, so
// every render carries the caveat rather than trusting a user to have read
// it once.
const estimateCaveat = "Costs are estimates from a pricing table and ignore subscription plans."

// FooterRender draws the machine totals: total SoC watts next to total
// $/hr (the one number no existing tool puts side by side), wattop's own
// CPU cost, an "N models unpriced" count when nonzero, any Degraded source
// badges, and the standing estimate caveat.
//
// sortKey and themeName name the active sort column and palette, paused
// reports whether the model has frozen updates, and hidden is how many
// sessions the table's dormant filter is keeping off screen -- all four
// are otherwise invisible on screen, and a paused monitoring tool with
// nothing saying so is the one state this UI must never hide. The status
// line carrying them is placed right after the machine-totals line (not
// last) so it survives frame's height truncation even when height is too
// short for every line below it.
func FooterRender(snap *domain.Snapshot, r theme.Roles, width, height int, sortKey, themeName string, paused bool, hidden int, opts Options) string {
	var lines []string

	watts := fdash(snap.Sys.Power.SystemWatts, "%.1fW")
	burnColor := r.ChartCost
	if snap.TotalBurnUSDPerHr >= 5 {
		burnColor = r.CostHot
	}
	burn := styled(opts, burnColor, fmt.Sprintf("$%.2f/hr", snap.TotalBurnUSDPerHr))
	lines = append(lines, machineLine(width, []string{
		styled(opts, r.ChartWatts, watts+" total"),
		burn + " total",
		styled(opts, r.ChartCost, fmt.Sprintf("$%.2f session total", snap.TotalCostUSD)),
		fmt.Sprintf("wattop self %.1f%% CPU", snap.SelfCPUPct),
	}))

	lines = append(lines, statusLine(r, sortKey, themeName, paused, hidden, width, opts))

	if n := len(snap.UnpricedModels); n > 0 {
		lines = append(lines, fmt.Sprintf("%d models unpriced: %s", n, strings.Join(snap.UnpricedModels, ", ")))
	}

	if len(snap.Degraded) > 0 {
		badge := styled(opts, r.Warn, "Degraded: "+strings.Join(snap.Degraded, "; "))
		lines = append(lines, badge)
	}

	lines = append(lines, styled(opts, r.Muted, estimateCaveat))

	return frame(lines, width, height)
}

// machineLine joins the machine-totals segments, dropping them from the
// right until the line fits width. Nothing here is truncated: cutting
// "$335.19 session total" mid-number at 80 columns would leave a figure
// that reads as a smaller, wrong one, so the last segment is dropped
// whole instead. The first segment (total watts) is always kept, however
// narrow the terminal -- a footer with no numbers at all is worse than
// one that overruns by a character.
func machineLine(width int, segs []string) string {
	const prefix = "Machine  "
	const sep = "  |  "
	build := func(n int) string { return prefix + strings.Join(segs[:n], sep) }
	n := len(segs)
	for n > 1 && lipgloss.Width(build(n)) > width {
		n--
	}
	return build(n)
}

// statusLine advertises the keybindings that otherwise leave no trace on
// screen: `s` (sort), `t` (theme), `p` (pause) and `a` (show all), plus
// the `?` help overlay itself. [PAUSED] is styled with r.Warn and always
// shown when paused -- it never shares a badge with an active sort/theme,
// so it cannot be mistaken for the normal state. "N hidden" is the same
// contract for the dormant-row filter: a row the table drops always has a
// count on screen saying so, and the key that brings it back.
//
// The line is right-aligned within width (padLine only pads on the right,
// so alignment is done here with leading spaces) rather than left-aligned
// like every other footer line, so it reads visually distinct as a status
// strip. When it does not fit, sort and then theme are dropped -- both are
// also on the help overlay, while [PAUSED] and the hidden count are not
// recoverable from anywhere else.
func statusLine(r theme.Roles, sortKey, themeName string, paused bool, hidden, width int, opts Options) string {
	segs := []string{fmt.Sprintf("sort:%s", sortKey), fmt.Sprintf("theme:%s", themeName)}
	if paused {
		segs = append(segs, styled(opts, r.Warn, "[PAUSED]"))
	}
	if hidden > 0 {
		segs = append(segs, fmt.Sprintf("%d hidden (a)", hidden))
	}
	segs = append(segs, "g graphs", styled(opts, r.Accent, "? help"))

	line := strings.Join(segs, "   ")
	for len(segs) > 1 && lipgloss.Width(line) > width {
		segs = segs[1:]
		line = strings.Join(segs, "   ")
	}
	if pad := width - lipgloss.Width(line); pad > 0 {
		return strings.Repeat(" ", pad) + line
	}
	return line
}
