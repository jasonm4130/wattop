package panel

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// Options controls presentation choices that are not part of the theme
// itself.
type Options struct {
	// NoColor drops the palette entirely: gauges degrade to plain block
	// characters and no lipgloss styling is applied anywhere in the panel.
	// This is the --no-color / NO_COLOR contract.
	NoColor bool
}

const gaugeWidth = 20

var thermalNames = map[int]string{
	0: "Nominal",
	1: "Fair",
	2: "Serious",
	3: "Critical",
}

func thermalLabel(state int) string {
	if name, ok := thermalNames[state]; ok {
		return name
	}
	return "Unknown"
}

// thermalColor picks the severity role for a thermal state: Nominal is
// baseColor (the panel's normal border/text color), Fair is Warn, and
// Serious/Critical/anything unrecognised is Hot.
func thermalColor(r theme.Roles, state int, baseColor string) string {
	switch {
	case state >= 2 || state < 0:
		return r.Hot
	case state == 1:
		return r.Warn
	default:
		return baseColor
	}
}

// Render draws the SoC strip -- clusters (iterated from the reported
// topology, never a hardcoded E/P pair), GPU utilisation and frequency, the
// power row, bandwidth, temperatures, fans, thermal state, memory/swap and
// network/disk -- from a domain.SysSample and nothing else.
//
// Every optional (*float64) field that is nil renders as "—", never "0"; and
// only nil renders as "—". A bandwidth channel that resolved and read 0.0
// GB/s prints 0.0 GB/s, because on this hardware that is a real idle
// measurement -- dashing it was what hid live DRAM traffic (QA 2026-09-06
// §2). A bandwidth figure the platform derived rather than counted prints
// with a leading "~".
//
// The returned string is exactly height lines of exactly width display
// columns each (measured with lipgloss.Width, so ANSI styling never throws
// off the padding).
func Render(sample domain.SysSample, r theme.Roles, width, height int, opts Options) string {
	var lines []string

	lines = append(lines, borderLine(r, sample.ThermalState, width, opts))
	lines = append(lines, "SoC    "+valueOrDash(sample.SoCName))

	for _, c := range sample.Clusters {
		lines = append(lines, clusterLine(r, c, opts))
	}

	lines = append(lines, gpuLine(r, sample, opts))
	lines = append(lines, powerLine(sample.Power))
	lines = append(lines, bandwidthLine(sample.Bandwidth))
	lines = append(lines, tempLine(sample.Temps))
	lines = append(lines, fansLine(sample.Fans))
	lines = append(lines, thermalLine(r, sample.ThermalState, opts))
	lines = append(lines, memoryLine(sample.Memory))
	lines = append(lines, netDiskLine(sample.Net, sample.Disk))

	return frame(lines, width, height)
}

func borderLine(r theme.Roles, state, width int, opts Options) string {
	if width < 0 {
		width = 0
	}
	line := strings.Repeat("─", width)
	return styled(opts, thermalColor(r, state, r.Border), line)
}

func clusterLine(r theme.Roles, c domain.Cluster, opts Options) string {
	pct := 0.0
	pctStr := "  —"
	if c.ActivePct != nil {
		pct = *c.ActivePct
		pctStr = fmt.Sprintf("%5.1f%%", pct)
	}
	freqStr := "—"
	if c.FreqMHz != nil {
		freqStr = fmt.Sprintf("%.0f MHz", *c.FreqMHz)
	}
	color := r.Severity(pct, 70, 90)
	bar := Bar(r, pct, gaugeWidth, color, opts.NoColor)
	label := fmt.Sprintf("%s (%d)", c.Label, c.CoreCount)
	return fmt.Sprintf("%-8s [%s] %s  %s", label, bar, pctStr, freqStr)
}

func gpuLine(r theme.Roles, s domain.SysSample, opts Options) string {
	pct := 0.0
	pctStr := "  —"
	if s.GPU.ActivePct != nil {
		pct = *s.GPU.ActivePct
		pctStr = fmt.Sprintf("%5.1f%%", pct)
	}
	freqStr := "—"
	if s.GPU.FreqMHz != nil {
		freqStr = fmt.Sprintf("%.0f MHz", *s.GPU.FreqMHz)
	}
	label := "GPU"
	if s.GPU.CoreCount > 0 {
		label = fmt.Sprintf("GPU (%d)", s.GPU.CoreCount)
	}
	bar := Bar(r, pct, gaugeWidth, r.ChartGPU, opts.NoColor)
	return fmt.Sprintf("%-8s [%s] %s  %s", label, bar, pctStr, freqStr)
}

func fdash(v *float64, format string) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf(format, *v)
}

// bwdash renders a bandwidth reading. Only nil is a dash; 0.0 GB/s is a
// reading. estimated prefixes "~", the project's mark for a derived number.
func bwdash(v *float64, estimated bool) string {
	if v == nil {
		return "—"
	}
	if estimated {
		return fmt.Sprintf("~%.1f GB/s", *v)
	}
	return fmt.Sprintf("%.1f GB/s", *v)
}

func powerLine(p domain.Power) string {
	return fmt.Sprintf("Power  CPU %s  GPU %s  ANE %s  DRAM %s  Sys %s",
		fdash(p.CPUWatts, "%.1fW"),
		fdash(p.GPUWatts, "%.1fW"),
		fdash(p.ANEWatts, "%.1fW"),
		fdash(p.DRAMWatts, "%.1fW"),
		fdash(p.SystemWatts, "%.1fW"),
	)
}

// bandwidthLine shows the DRAM total only when there is one to show -- on a
// combined-only or estimated source that total is the whole reading, with
// both directions dashed; on a directional source it is the sum.
func bandwidthLine(b domain.Bandwidth) string {
	line := fmt.Sprintf("BW     DRAM R %s  W %s",
		bwdash(b.DRAMReadGBs, false),
		bwdash(b.DRAMWriteGBs, false),
	)
	if b.DRAMCombinedGBs != nil {
		line += "  Total " + bwdash(b.DRAMCombinedGBs, b.DRAMEstimated)
	}
	return line + "  ANE " + bwdash(b.ANECombinedGBs, false)
}

// tempKeys is the whole temperature vocabulary this row renders, in the order
// it renders them. Anything else in the map is a raw sensor key, not a
// metric, and is ignored -- a sampler that hands over 325 SMC keys must not
// be able to blow the frame apart (QA 2026-09-06 §2).
var tempKeys = []string{"cpu", "gpu", "soc"}

func tempLine(t map[string]float64) string {
	parts := make([]string, 0, len(tempKeys))
	for _, k := range tempKeys {
		v, ok := t[k]
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %.1f°C", strings.ToUpper(k), v))
	}
	if len(parts) == 0 {
		return "Temp   —"
	}
	return "Temp   " + strings.Join(parts, "  ")
}

func fansLine(fans []domain.Fan) string {
	if len(fans) == 0 {
		return "Fans   —"
	}
	parts := make([]string, 0, len(fans))
	for _, f := range fans {
		parts = append(parts, fmt.Sprintf("%s %.0f RPM", valueOrDash(f.Label), f.RPM))
	}
	return "Fans   " + strings.Join(parts, "  ")
}

func thermalLine(r theme.Roles, state int, opts Options) string {
	return "Thermal  " + styled(opts, thermalColor(r, state, r.Idle), thermalLabel(state))
}

func gb(b uint64) float64 { return float64(b) / 1e9 }

func memoryLine(m domain.MemorySample) string {
	return fmt.Sprintf("Mem    %.1f/%.1f GB  Swap %.1f/%.1f GB",
		gb(m.UsedBytes), gb(m.TotalBytes), gb(m.SwapUsedBytes), gb(m.SwapTotalBytes))
}

func rate(bps float64) string {
	switch {
	case bps >= 1e6:
		return fmt.Sprintf("%.1f MB/s", bps/1e6)
	case bps >= 1e3:
		return fmt.Sprintf("%.1f KB/s", bps/1e3)
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

func netDiskLine(n domain.NetSample, d domain.DiskSample) string {
	return fmt.Sprintf("Net    ↓%s ↑%s   Disk R %s W %s",
		rate(n.InBytesPerSec), rate(n.OutBytesPerSec),
		rate(d.ReadBytesPerSec), rate(d.WriteBytesPerSec))
}

func valueOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func styled(opts Options, color, s string) string {
	if opts.NoColor || color == "" {
		return s
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(s)
}

// padLine pads s with spaces up to width display columns, measured with
// lipgloss.Width so embedded ANSI styling is never counted. A line already
// at or past width is returned unpadded rather than truncated: chopping a
// styled string risks splitting an escape sequence.
func padLine(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func frame(lines []string, width, height int) string {
	if height < 0 {
		height = 0
	}
	if width < 0 {
		width = 0
	}
	out := make([]string, height)
	for i := 0; i < height; i++ {
		if i < len(lines) {
			out[i] = padLine(lines[i], width)
		} else {
			out[i] = strings.Repeat(" ", width)
		}
	}
	return strings.Join(out, "\n")
}
