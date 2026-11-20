package panel

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

const estimateCaveat = "Costs are estimates from a pricing table and ignore subscription plans."

func FooterRender(snap *domain.Snapshot, r theme.Roles, width, height int, sortKey, themeName string, paused bool, hidden int, opts Options) string {
	var lines []string

	watts := fdash(snap.Sys.Power.SystemWatts, "%.1fW")
	burnColor := r.ChartCost
	if snap.TotalBurnUSDPerHr >= opts.burnHot() {
		burnColor = r.CostHot
	}
	burn := styled(opts, burnColor, fmt.Sprintf("$%.2f/hr", snap.TotalBurnUSDPerHr))
	partial := ""
	if snap.TotalCostPartial {
		partial = "~"
	}
	lines = append(lines, machineLine(width, []string{
		styled(opts, r.ChartWatts, watts+" total"),
		burn + " total",
		styled(opts, r.ChartCost, fmt.Sprintf("%s$%.2f session total", partial, snap.TotalCostUSD)),
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
