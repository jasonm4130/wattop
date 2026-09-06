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

// Golden-test determinism: lipgloss v2's Style.Render() never consults the
// terminal or an environment variable -- confirmed by reading
// charm.land/lipgloss/v2@v2.0.6/style.go, which has no os.Getenv or
// colorprofile reference anywhere in Render(). Downsampling by detected
// color profile only happens in the *print* helpers (lipgloss.Println,
// lipgloss.Writer, ...), which this package never calls: Render returns a
// plain string built from Style.Render() calls and is written to the
// golden file (or a real terminal) unmodified. That is what makes the
// identical output land in both an interactive macOS terminal (a real TTY,
// which the auto-detecting print helpers would treat as TrueColor) and
// under `CGO_ENABLED=0 go test ... > file` (a pipe, which they would treat
// as NoTTY and strip): there is no detection step in this code path for the
// two environments to disagree on. No TestMain profile pin is needed as a
// result, since there is no profile to pin -- verified by running this
// file's TestSoCGolden both ways (see the task's Acceptance section).

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

// TestSoCGolden renders the replay sampler's decoded mactop-ndjson.jsonl at
// a fixed 120x40 and golden-compares it. Run with -update to regenerate.
func TestSoCGolden(t *testing.T) {
	sample := loadFixtureSample(t, "mactop-ndjson.jsonl")
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}

	out := Render(sample, r, 120, 40, Options{})
	golden.RequireEqual(t, []byte(out))
}

// TestDegradedRendersDash renders mactop-degraded.jsonl, which is missing the
// dram_*_bw_gbs keys entirely, and asserts every bandwidth figure renders as
// a dash.
//
// The ANE key IS present in that fixture and reads exactly 0, and it dashes
// too. That is not the same rule the live sampler follows: internal/soc
// tracks which ioreport.m branch produced DRAM bytes (DRAMBWSource) and can
// therefore publish a measured 0.0 as a reading. mactop's headless JSON --
// the only thing this fixture format carries -- has no such flag, so a 0 in
// it is indistinguishable from an absent channel and
// internal/collect/replay treats it as unresolved. Distinguishing the two
// where the data allows it is what stopped live DRAM traffic hiding behind a
// dash (QA 2026-09-06 §2); claiming to distinguish them where it does not
// would be the same error in the other direction.
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

// TestBandwidthCombinedRendersAsEstimate covers the source that this M5 Max
// actually falls back to: one DRAM figure derived from DRAM power. It must
// render once, as a marked estimate, with both directions dashed -- never as
// a read figure and a write figure carrying the same number.
func TestBandwidthCombinedRendersAsEstimate(t *testing.T) {
	combined := 32.8
	line := bandwidthLine(domain.Bandwidth{DRAMCombinedGBs: &combined, DRAMEstimated: true})

	want := "BW     DRAM R —  W —  Total ~32.8 GB/s  ANE —"
	if line != want {
		t.Errorf("bandwidthLine = %q, want %q", line, want)
	}
}

// TestBandwidthDirectionalRendersBothDirections is the counterpart: a source
// that measured both directions prints both, unmarked, plus their total.
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

// TestTempLineIgnoresRawSensorKeys is the Temp-row regression. The live
// sampler once put every raw SMC key into Temps -- 325 of them -- and this
// row printed all of them, clipping the frame (QA 2026-09-06 §2). The row
// renders the three keys it knows, in a fixed order, and nothing else.
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

// TestTempLineOmitsAbsentKey proves a key the sampler never published drops
// out of the row rather than rendering a zero.
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

// TestClustersFromTopologyNotConstants proves the panel iterates whatever
// clusters domain.SysSample.Clusters reports, rather than assuming a
// hardcoded E/P pair.
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

// TestNoColorDropsPalette asserts --no-color/NO_COLOR renders plain block
// characters with no ANSI escape sequences at all.
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

// TestNilOptionalNeverRendersZero guards the "nil renders as — never 0"
// rule directly against a synthetic sample with every optional field nil.
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

// TestRenderNegativeOrTinyHeightNeverPanics guards frame() (and borderLine's
// width handling) against a terminal short enough that the caller's
// height-3/width arithmetic goes to zero or negative -- Model.View derives
// the panel height as m.height-3 with only a >0 guard on m.height, so a 1-,
// 2- or 3-row terminal passes 0 or a negative height straight through. Before
// the fix, make([]string, height) with a negative length panicked with
// "makeslice: len out of range".
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

// TestFrameNegativeWidthNeverPanics guards frame()/borderLine() directly
// against a negative width, the same shape of bug as the height case.
func TestFrameNegativeWidthNeverPanics(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("frame panicked with a negative width: %v", rec)
		}
	}()
	out := frame([]string{"a", "b"}, -1, 2)
	// A negative width clamps to 0 for padding/fill purposes; content lines
	// already at or past that width are returned unpadded (padLine never
	// truncates), so "a" and "b" pass through as-is with no panic.
	want := "a\nb"
	if out != want {
		t.Errorf("frame with negative width: got %q, want %q", out, want)
	}
}
