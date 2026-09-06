package panel

import (
	"strings"
	"testing"

	"github.com/jasonm4130/wattop/internal/ui/theme"
)

func TestBar(t *testing.T) {
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}

	t.Run("width is preserved", func(t *testing.T) {
		out := Bar(r, 50, 10, r.GaugeMid, true)
		if got := len([]rune(out)); got != 10 {
			t.Errorf("Bar rune length = %d, want 10: %q", got, out)
		}
	})

	t.Run("no color drops styling", func(t *testing.T) {
		out := Bar(r, 50, 10, r.GaugeMid, true)
		if strings.Contains(out, "\x1b[") {
			t.Errorf("noColor Bar contains an ANSI escape: %q", out)
		}
	})

	t.Run("colored bar is styled", func(t *testing.T) {
		out := Bar(r, 50, 10, r.GaugeMid, false)
		if !strings.Contains(out, "\x1b[") {
			t.Errorf("colored Bar has no ANSI escape at all: %q", out)
		}
	})

	t.Run("pct is clamped", func(t *testing.T) {
		full := Bar(r, 500, 10, r.GaugeMid, true)
		empty := Bar(r, -50, 10, r.GaugeMid, true)
		if full != strings.Repeat("█", 10) {
			t.Errorf("pct > 100 should fully fill: %q", full)
		}
		if empty != strings.Repeat("░", 10) {
			t.Errorf("negative pct should render empty: %q", empty)
		}
	})
}
