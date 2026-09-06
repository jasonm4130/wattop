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
// sortKey and themeName name the active sort column and palette, and
// paused reports whether the model has frozen updates -- all three are
// otherwise invisible on screen, and a paused monitoring tool with nothing
// saying so is the one state this UI must never hide. The status line
// carrying them is placed right after the machine-totals line (not last)
// so it survives frame's height truncation even when height is too short
// for every line below it.
func FooterRender(snap *domain.Snapshot, r theme.Roles, width, height int, sortKey, themeName string, paused bool, opts Options) string {
	var lines []string

	watts := fdash(snap.Sys.Power.SystemWatts, "%.1fW")
	burnColor := ""
	if snap.TotalBurnUSDPerHr >= 5 {
		burnColor = r.CostHot
	}
	burn := styled(opts, burnColor, fmt.Sprintf("$%.2f/hr", snap.TotalBurnUSDPerHr))
	lines = append(lines, fmt.Sprintf("Machine  %s total  |  %s total  |  $%.2f session total  |  wattop self %.1f%% CPU",
		watts, burn, snap.TotalCostUSD, snap.SelfCPUPct))

	lines = append(lines, statusLine(r, sortKey, themeName, paused, width, opts))

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

// statusLine advertises the three keybindings that otherwise leave no
// trace on screen: `s` (sort), `t` (theme) and `p` (pause), plus the `?`
// help overlay itself. [PAUSED] is styled with r.Warn and always shown
// when paused -- it never shares a badge with an active sort/theme, so it
// cannot be mistaken for the normal state. The line is right-aligned
// within width (padLine only pads on the right, so alignment is done here
// with leading spaces) rather than left-aligned like every other footer
// line, so it reads visually distinct as a status strip.
func statusLine(r theme.Roles, sortKey, themeName string, paused bool, width int, opts Options) string {
	segs := []string{fmt.Sprintf("sort:%s", sortKey), fmt.Sprintf("theme:%s", themeName)}
	if paused {
		segs = append(segs, styled(opts, r.Warn, "[PAUSED]"))
	}
	segs = append(segs, "? help")
	line := strings.Join(segs, "   ")
	if pad := width - lipgloss.Width(line); pad > 0 {
		return strings.Repeat(" ", pad) + line
	}
	return line
}
