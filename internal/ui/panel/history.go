package panel

import (
	"fmt"
	"math"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// HistoryRender keeps every chart on the same two-minute wall-clock axis.
// Values are sampled at each column's right edge; missing periods stay blank.
func HistoryRender(s *domain.Snapshot, history func(string) []float64, r theme.Roles, width, height int, opts Options) string {
	if height <= 0 {
		return ""
	}
	times := history("time")
	end := float64(s.At.UnixMilli()) / 1000
	input, output := "—", "—"
	if s.TokenRate != nil {
		input = fmt.Sprintf("%.1f", s.TokenRate.InputPerSec)
		output = fmt.Sprintf("%.1f", s.TokenRate.OutputPerSec)
	}
	leftW := (width - 1) / 2
	rightW := width - leftW - 1
	plotH := max(1, (height-5)/2)
	chart := func(key, title, color string, ceiling float64, w int) []string {
		values := history(key)
		if ceiling == 0 {
			ceiling = 1
			for i, v := range values {
				if i < len(times) && times[i] >= end-120 && !math.IsNaN(v) {
					ceiling = max(ceiling, v)
				}
			}
		}
		label := fmt.Sprintf("%s  0–%.0f", title, ceiling)
		lines := []string{styled(opts, color, label)}
		return append(lines, areaGraph(values, times, end, ceiling, w, plotH, color, opts)...)
	}
	left := chart("tokens_out", "OUT "+output+" tok/s", r.ChartCost, 0, leftW-4)
	left = append(left, chart("tokens_in", "IN  "+input+" tok/s", r.Accent, 0, leftW-4)...)
	plotH = max(1, (height-6)/3)
	right := chart("cpu", "CPU %", r.ChartCPU, 100, rightW-4)
	right = append(right, chart("gpu", "GPU %", r.ChartGPU, 100, rightW-4)...)
	// Power keeps its own scale rather than sharing the percentage axis.
	right = append(right, chart("watts", "POWER W", r.ChartWatts, 0, rightW-4)...)
	a := card("TOKEN THROUGHPUT", left, r, leftW, height-1, opts)
	b := card("HARDWARE HISTORY", right, r, rightW, height-1, opts)
	axis := styled(opts, r.Muted, "120s ago → now · tok/s: recorded over 60s")
	return frame(append(strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, a, " ", b), "\n"), axis), width, height)
}

// HardwareSummary keeps readings visible when graphs leave little vertical room.
func HardwareSummary(s domain.SysSample, r theme.Roles, width, height int, opts Options) string {
	return frame([]string{
		Rule("wattop / "+valueOrDash(s.SoCName)+" / g meters", r, width, opts),
		memoryLine(s.Memory) + "  " + thermalLine(r, s.ThermalState, opts),
		powerLine(s.Power),
	}, width, height)
}

func areaGraph(values, times []float64, end, ceiling float64, width, height int, color string, opts Options) []string {
	const blocks = "▁▂▃▄▅▆▇█"
	glyphs := []rune(blocks)
	rows := make([][]rune, height)
	for y := range rows {
		rows[y] = []rune(strings.Repeat(" ", max(0, width)))
	}
	idx := -1
	for x := 0; x < width; x++ {
		at := end - 120 + 120*float64(x+1)/float64(width)
		for idx+1 < len(times) && times[idx+1] <= at {
			idx++
		}
		if idx < 0 || idx >= len(values) || at-times[idx] > 15 || math.IsNaN(values[idx]) || math.IsInf(values[idx], 0) {
			continue
		}
		level := int(math.Round(min(1, max(0, values[idx]/ceiling)) * float64(height*8)))
		if level == 0 {
			rows[height-1][x] = '·'
		}
		for y := height - 1; y >= 0 && level > 0; y-- {
			rows[y][x] = glyphs[min(level, 8)-1]
			level -= 8
		}
	}
	lines := make([]string, height)
	for y := range rows {
		lines[y] = styled(opts, color, string(rows[y]))
	}
	return lines
}
