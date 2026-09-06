package panel

import (
	"fmt"
	"strings"

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
func FooterRender(snap *domain.Snapshot, r theme.Roles, width, height int, opts Options) string {
	var lines []string

	watts := fdash(snap.Sys.Power.SystemWatts, "%.1fW")
	burnColor := ""
	if snap.TotalBurnUSDPerHr >= 5 {
		burnColor = r.CostHot
	}
	burn := styled(opts, burnColor, fmt.Sprintf("$%.2f/hr", snap.TotalBurnUSDPerHr))
	lines = append(lines, fmt.Sprintf("Machine  %s total  |  %s total  |  $%.2f session total  |  wattop self %.1f%% CPU",
		watts, burn, snap.TotalCostUSD, snap.SelfCPUPct))

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
