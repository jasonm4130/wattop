package panel

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// Rule embeds a section label in a quiet, full-width divider.
func Rule(title string, r theme.Roles, width int, opts Options) string {
	if width < 5 {
		return styled(opts, r.Border, strings.Repeat("─", max(0, width)))
	}
	label := "─ " + title + " "
	return styled(opts, r.Border, "─ ") + styled(opts, r.Accent, ansi.Truncate(title, max(0, width-4), "…")) +
		styled(opts, r.Border, " "+strings.Repeat("─", max(0, width-lipgloss.Width(label))))
}

func card(title string, lines []string, r theme.Roles, width, height int, opts Options) string {
	inner := max(0, width-4)
	border := func(s string) string { return styled(opts, r.Border, s) }
	top := border("╭─ ") + styled(opts, r.Accent, title) + border(" "+strings.Repeat("─", max(0, width-lipgloss.Width(title)-5))+"╮")
	out := []string{top}
	for i := 0; i < height-2; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, border("│ ")+padLine(ansi.Truncate(line, inner, "…"), inner)+border(" │"))
	}
	out = append(out, border("╰"+strings.Repeat("─", max(0, width-2))+"╯"))
	return strings.Join(out, "\n")
}

// HardwareHeight reserves a compact strip or two neighbouring instruments.
func HardwareHeight(s domain.SysSample, width int) int {
	if width < 110 {
		return 10 + len(s.Clusters)
	}
	return max(12, 10+len(s.Clusters))
}

// HardwareRender uses the available width to put related readings together.
// The compact strip retains its full vocabulary at ordinary 80-column sizes.
func HardwareRender(s domain.SysSample, r theme.Roles, width, height int, opts Options) string {
	if width < 110 {
		return Render(s, r, width, height, opts)
	}
	leftW := (width - 1) / 2
	rightW := width - leftW - 1
	barWidth := leftW - 33
	var compute []string
	for _, c := range s.Clusters {
		compute = append(compute, clusterMeter(r, c, barWidth, opts))
	}
	compute = append(compute, gpuMeter(r, s, barWidth, opts), "")
	pct := 0.0
	if s.Memory.TotalBytes > 0 {
		pct = 100 * float64(s.Memory.UsedBytes) / float64(s.Memory.TotalBytes)
	}
	compute = append(compute, fmt.Sprintf("Memory   [%s] %5.1f%%", Bar(r, pct, barWidth, r.Severity(pct, 70, 90), opts.NoColor), pct))
	compute = append(compute, memoryLine(s.Memory))
	if s.Bandwidth.DRAMCombinedGBs != nil {
		compute = append(compute, "DRAM   Total "+bwdash(s.Bandwidth.DRAMCombinedGBs, s.Bandwidth.DRAMEstimated))
	}
	compute = append(compute, fmt.Sprintf("BW     DRAM R %s  W %s", bwdash(s.Bandwidth.DRAMReadGBs, false), bwdash(s.Bandwidth.DRAMWriteGBs, false)))
	compute = append(compute, "ANE BW "+bwdash(s.Bandwidth.ANECombinedGBs, false))

	p := s.Power
	power := []string{
		styled(opts, r.ChartWatts, fdash(p.SystemWatts, "%.1f W")) + " system" + "    " + thermalLine(r, s.ThermalState, opts),
		fmt.Sprintf("CPU %s   GPU %s   ANE %s", fdash(p.CPUWatts, "%.1fW"), fdash(p.GPUWatts, "%.1fW"), fdash(p.ANEWatts, "%.1fW")),
		"DRAM " + fdash(p.DRAMWatts, "%.1fW"),
		"",
		tempLine(s.Temps),
		fansLine(s.Fans),
		"",
		fmt.Sprintf("Net    ↓ %-14s ↑ %s", rate(s.Net.InBytesPerSec), rate(s.Net.OutBytesPerSec)),
		fmt.Sprintf("Disk   R %-14s W %s", rate(s.Disk.ReadBytesPerSec), rate(s.Disk.WriteBytesPerSec)),
	}
	cardH := HardwareHeight(s, width) - 1
	left := card("COMPUTE / MEMORY", compute, r, leftW, cardH, opts)
	right := card("POWER / THERMALS / I/O", power, r, rightW, cardH, opts)
	header := styled(opts, r.Accent, " wattop") + styled(opts, r.Muted, "  /  ") + valueOrDash(s.SoCName)
	return frame(append([]string{header}, strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right), "\n")...), width, height)
}
