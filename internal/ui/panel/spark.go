package panel

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/sparkline"

	"github.com/jasonm4130/wattop/internal/ui/theme"
)

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
