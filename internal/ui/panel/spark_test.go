package panel

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// TestSparkline asserts a known series produces a non-empty string of the
// requested width, and that an empty series returns width spaces rather
// than panicking.
func TestSparkline(t *testing.T) {
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}

	t.Run("known series", func(t *testing.T) {
		series := []float64{1, 4, 2, 8, 5, 7, 3, 6}
		out := Sparkline(r, series, 8)
		if out == "" {
			t.Fatal("Sparkline returned an empty string for a non-empty series")
		}
		if got := lipgloss.Width(out); got != 8 {
			t.Errorf("Sparkline display width = %d, want 8: %q", got, out)
		}
	})

	t.Run("empty series", func(t *testing.T) {
		out := Sparkline(r, nil, 8)
		if out != strings.Repeat(" ", 8) {
			t.Errorf("Sparkline(nil) = %q, want 8 spaces", out)
		}
	})
}
