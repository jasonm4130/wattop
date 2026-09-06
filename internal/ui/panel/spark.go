package panel

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/sparkline"

	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// Sparkline renders series as a single-row braille sparkline of exactly
// width display columns, coloured with the theme's accent role.
//
// v0.1 has no chart panel that calls this -- it exists so the tree's one
// ntcharts import is real (Task 1's manifest pins it and its acceptance
// greps for it) and so v0.2's chart row is a layout change rather than a
// new dependency. An empty series returns width spaces rather than
// panicking: a freshly-started session has no history yet, and a blank
// braille canvas is exactly that -- width empty cells, which the ntcharts
// canvas already renders as spaces.
func Sparkline(r theme.Roles, series []float64, width int) string {
	if width <= 0 {
		return ""
	}
	if len(series) == 0 {
		return strings.Repeat(" ", width)
	}

	m := sparkline.New(width, 1, sparkline.WithStyle(
		lipgloss.NewStyle().Foreground(lipgloss.Color(r.Accent)),
	))
	m.PushAll(series)
	m.DrawBraille()
	return m.Canvas.View()
}
