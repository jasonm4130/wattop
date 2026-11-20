package panel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/jasonm4130/wattop/internal/collect/replay"
	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/fixture"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

func loadFixtureSample(t *testing.T, filename string) domain.SysSample {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(fixture.CorpusDir(), "soc", filename)
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", src, err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), raw, 0o644); err != nil {
		t.Fatalf("writing temp fixture: %v", err)
	}

	sampler, err := replay.NewSysSampler(dir)
	if err != nil {
		t.Fatalf("NewSysSampler: %v", err)
	}
	sample, err := sampler.Sample(context.Background(), 1000)
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	return sample
}

func TestSoCGolden(t *testing.T) {
	sample := loadFixtureSample(t, "mactop-ndjson.jsonl")
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}

	out := Render(sample, r, 120, 40, Options{})
	golden.RequireEqual(t, []byte(out))
}

func TestDegradedRendersDash(t *testing.T) {
	sample := loadFixtureSample(t, "mactop-degraded.jsonl")
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}

	out := Render(sample, r, 120, 40, Options{})

	if !strings.Contains(out, "BW     DRAM R —  W —") {
		t.Errorf("expected absent DRAM bandwidth channels to render as dashes, got:\n%s", out)
	}
	if !strings.Contains(out, "BW     DRAM R —  W —  ANE —") {
		t.Errorf("expected the present-but-zero ANE channel to dash on the headless-JSON path, got:\n%s", out)
	}
}

func TestBandwidthCombinedRendersAsEstimate(t *testing.T) {
	combined := 32.8
	line := bandwidthLine(domain.Bandwidth{DRAMCombinedGBs: &combined, DRAMEstimated: true})

	want := "BW     DRAM R —  W —  Total ~32.8 GB/s  ANE —"
	if line != want {
		t.Errorf("bandwidthLine = %q, want %q", line, want)
	}
}

func TestBandwidthEstimatedZeroStaysAnEstimate(t *testing.T) {
	zero := 0.0
	line := bandwidthLine(domain.Bandwidth{DRAMCombinedGBs: &zero, DRAMEstimated: true})

	want := "BW     DRAM R —  W —  Total ~0.0 GB/s  ANE —"
	if line != want {
		t.Errorf("bandwidthLine = %q, want %q", line, want)
	}
}

func TestBandwidthDirectionalRendersBothDirections(t *testing.T) {
	read, write, combined := 12.5, 7.5, 20.0
	line := bandwidthLine(domain.Bandwidth{
		DRAMReadGBs:     &read,
		DRAMWriteGBs:    &write,
		DRAMCombinedGBs: &combined,
	})

	want := "BW     DRAM R 12.5 GB/s  W 7.5 GB/s  Total 20.0 GB/s  ANE —"
	if line != want {
		t.Errorf("bandwidthLine = %q, want %q", line, want)
	}
}

func TestTempLineIgnoresRawSensorKeys(t *testing.T) {
	temps := map[string]float64{
		"soc": 52.0, "gpu": 51.25, "cpu": 50.5,
		"TAOL": 28.5, "Tg5q": 51.0, "TB0T": 30.0, "Nv00": 0.0,
	}

	line := tempLine(temps)
	want := "Temp   CPU 50.5°C  GPU 51.2°C  SOC 52.0°C"
	if line != want {
		t.Errorf("tempLine = %q, want %q", line, want)
	}
}

func TestTempLineOmitsAbsentKey(t *testing.T) {
	line := tempLine(map[string]float64{"cpu": 50.5})
	want := "Temp   CPU 50.5°C"
	if line != want {
		t.Errorf("tempLine = %q, want %q", line, want)
	}
	if line := tempLine(map[string]float64{"TAOL": 28.5}); line != "Temp   —" {
		t.Errorf("tempLine with only raw keys = %q, want %q", line, "Temp   —")
	}
}

var clusterLinePattern = regexp.MustCompile(`(?m)^\S+ \(\d+\)\s+\[`)

func TestClustersFromTopologyNotConstants(t *testing.T) {
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}

	t.Run("single unconventional label", func(t *testing.T) {
		pct := 42.0
		sample := domain.SysSample{
			SoCName: "Test SoC",
			Clusters: []domain.Cluster{
				{Label: "Q", CoreCount: 4, ActivePct: &pct},
			},
			Memory: domain.MemorySample{},
		}
		out := Render(sample, r, 120, 40, Options{})

		matches := clusterLinePattern.FindAllString(out, -1)
		if len(matches) != 1 {
			t.Fatalf("expected exactly 1 cluster gauge line, got %d:\n%s", len(matches), out)
		}
		if !strings.Contains(out, "Q (4)") {
			t.Errorf("expected the Q cluster's own label in the render, got:\n%s", out)
		}
		for _, hardcoded := range []string{"E (", "P (", "S ("} {
			if strings.Contains(out, hardcoded) {
				t.Errorf("render contains hardcoded cluster label %q that was never in the sample", hardcoded)
			}
		}
	})

	t.Run("three clusters", func(t *testing.T) {
		a, b, c := 10.0, 20.0, 30.0
		sample := domain.SysSample{
			SoCName: "Test SoC",
			Clusters: []domain.Cluster{
				{Label: "E", CoreCount: 4, ActivePct: &a},
				{Label: "P", CoreCount: 12, ActivePct: &b},
				{Label: "S", CoreCount: 6, ActivePct: &c},
			},
		}
		out := Render(sample, r, 120, 40, Options{})

		matches := clusterLinePattern.FindAllString(out, -1)
		if len(matches) != 3 {
			t.Fatalf("expected exactly 3 cluster gauge lines, got %d:\n%s", len(matches), out)
		}
	})
}

func TestNoColorDropsPalette(t *testing.T) {
	sample := loadFixtureSample(t, "mactop-ndjson.jsonl")
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}

	out := Render(sample, r, 120, 40, Options{NoColor: true})
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NoColor render still contains an ANSI escape sequence:\n%q", out)
	}
}

func TestNilOptionalNeverRendersZero(t *testing.T) {
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}
	sample := domain.SysSample{
		SoCName: "Test SoC",
		Clusters: []domain.Cluster{
			{Label: "P", CoreCount: 12},
		},
	}
	out := Render(sample, r, 120, 40, Options{})
	if !strings.Contains(out, "Power  CPU —  GPU —  ANE —  DRAM —  Sys —") {
		t.Errorf("nil Power fields should all render as dashes, got:\n%s", out)
	}
	if !strings.Contains(out, "P (12)") {
		t.Fatalf("expected the P cluster line, got:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "P (12)") {
			if strings.Contains(line, "0.0%") || strings.Contains(line, " 0 MHz") {
				t.Errorf("nil cluster ActivePct/FreqMHz rendered as a zero rather than a dash: %q", line)
			}
		}
	}
}

func TestRenderNegativeOrTinyHeightNeverPanics(t *testing.T) {
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}
	sample := domain.SysSample{
		SoCName:  "Test SoC",
		Clusters: []domain.Cluster{{Label: "P", CoreCount: 12}},
	}

	for _, height := range []int{-1, 0, 1, 2, 3} {
		height := height
		t.Run(fmt.Sprintf("height=%d", height), func(t *testing.T) {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("Render panicked at height=%d: %v", height, rec)
				}
			}()
			out := Render(sample, r, 40, height, Options{})
			wantLines := height
			if wantLines < 0 {
				wantLines = 0
			}
			if wantLines == 0 {
				if out != "" {
					t.Errorf("height=%d: expected empty output, got %q", height, out)
				}
				return
			}
			gotLines := len(strings.Split(out, "\n"))
			if gotLines != wantLines {
				t.Errorf("height=%d: expected %d lines, got %d", height, wantLines, gotLines)
			}
		})
	}
}

func TestFrameNegativeWidthNeverPanics(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("frame panicked with a negative width: %v", rec)
		}
	}()
	out := frame([]string{"a", "b"}, -1, 2)
	want := "a\nb"
	if out != want {
		t.Errorf("frame with negative width: got %q, want %q", out, want)
	}
}

func TestThermalLabelsMatchPressureScale(t *testing.T) {
	want := map[int]string{
		0: "Nominal", 1: "Moderate", 2: "Heavy", 3: "Trapping", 4: "Sleeping",
		-1: "Unknown", 5: "Unknown",
	}
	for state, label := range want {
		if got := thermalLabel(state); got != label {
			t.Errorf("thermalLabel(%d) = %q, want %q", state, got, label)
		}
	}
}

func TestThermalLineShowsThrottledBadge(t *testing.T) {
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}
	opts := Options{NoColor: true}
	if got := thermalLine(r, 2, true, opts); !strings.Contains(got, "THROTTLED") {
		t.Errorf("throttled thermal line = %q, want a THROTTLED badge", got)
	}
	if got := thermalLine(r, 0, false, opts); strings.Contains(got, "THROTTLED") {
		t.Errorf("unthrottled thermal line = %q, want no THROTTLED badge", got)
	}
}
