package panel

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// Bar renders a width-wide horizontal gauge for pct (0-100), filled up to
// pct with fillColor and the remainder with the theme's BarTrack role. When
// noColor is true it renders plain block characters with no lipgloss
// styling at all -- the --no-color / NO_COLOR contract drops the palette
// entirely rather than merely dimming it.
func Bar(r theme.Roles, pct float64, width int, fillColor string, noColor bool) string {
	if width <= 0 {
		return ""
	}
	if math.IsNaN(pct) || pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}

	filled := int(math.Round(pct / 100 * float64(width)))
	if filled > width {
		filled = width
	}
	fill := strings.Repeat("█", filled)
	track := strings.Repeat("░", width-filled)

	if noColor || fillColor == "" {
		return fill + track
	}

	fillStyled := lipgloss.NewStyle().Foreground(lipgloss.Color(fillColor)).Render(fill)
	trackStyled := lipgloss.NewStyle().Foreground(lipgloss.Color(r.BarTrack)).Render(track)
	return fillStyled + trackStyled
}
