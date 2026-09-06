package panel

import (
	"math"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

func TestGraphsFitAllThemesAndSizes(t *testing.T) {
	now := time.Unix(1000, 0)
	s := &domain.Snapshot{At: now, TokenRate: &domain.TokenRate{InputPerSec: 1200, OutputPerSec: 42}}
	history := func(key string) []float64 {
		if key == "time" {
			return []float64{900, 930, 960, 990, 1000}
		}
		return []float64{10, 30, 70, 20, 42}
	}
	for _, name := range theme.Names() {
		r, err := theme.Load(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range []int{80, 120, 160} {
			for _, h := range []int{9, 13} {
				for _, plain := range []bool{true, false} {
					out := HistoryRender(s, history, r, w, h, Options{NoColor: plain})
					lines := strings.Split(out, "\n")
					if len(lines) != h {
						t.Fatalf("height %d != %d", len(lines), h)
					}
					for _, line := range lines {
						if lipgloss.Width(line) != w {
							t.Fatalf("width %d != %d: %q", lipgloss.Width(line), w, line)
						}
					}
					for _, want := range []string{"OUT 42.0 tok/s", "IN  1200.0 tok/s", "CPU %", "GPU %", "POWER W", "60s"} {
						if !strings.Contains(out, want) {
							t.Fatalf("missing %q: %s", want, out)
						}
					}
					if plain && strings.Contains(out, "\x1b[") {
						t.Fatal("colour in plain graph")
					}
				}
			}
		}
	}
}

func TestGraphMissingIsNotZeroAndAxisUsesTime(t *testing.T) {
	r := areaGraph([]float64{0, math.NaN(), 100}, []float64{20, 40, 120}, 120, 100, 12, 2, "", Options{NoColor: true})
	if r[1][0] != ' ' {
		t.Fatal("unexpected leading point")
	}
	if !strings.Contains(r[1], "·") || !strings.HasSuffix(r[0], "█") {
		t.Fatalf("missing zero baseline or latest peak: %q", r)
	}
	if strings.Contains(r[0][:len(r[0])-3], "█") {
		t.Fatalf("peak moved off its time coordinate: %q", r)
	}
}

func TestSessionOutputRateIsVisible(t *testing.T) {
	s := domain.Session{Agent: "codex", ID: "rate", TokenRate: &domain.TokenRate{OutputPerSec: 42.5}}
	out := SessionsRender([]domain.Session{s}, loadDarkRoles(t), 80, 3, 0, time.Time{}, Options{NoColor: true})
	if !strings.Contains(out, "OUT/s") || !strings.Contains(out, "42.5") {
		t.Fatalf("rate missing: %s", out)
	}
}
