package panel

import (
	"context"
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

// TestDegradedRendersDash renders mactop-degraded.jsonl, which is missing
// the dram_*_bw_gbs keys entirely, and asserts DRAM bandwidth renders as a
// dash with no literal "0.0 GB/s" anywhere in the output -- the degraded
// fixture's ANE bandwidth channel is present but reads exactly 0, which per
// Render's doc comment renders as a dash too (the IOReport counter-zeroing
// latch, not a real reading).
func TestDegradedRendersDash(t *testing.T) {
	sample := loadFixtureSample(t, "mactop-degraded.jsonl")
	r, err := theme.Load("wattop-dark")
	if err != nil {
		t.Fatalf("theme.Load: %v", err)
	}

	out := Render(sample, r, 120, 40, Options{})

	if !strings.Contains(out, "—") {
		t.Error("expected a dash somewhere in the degraded render, found none")
	}
	if strings.Contains(out, "0.0 GB/s") {
		t.Errorf("degraded render contains a literal \"0.0 GB/s\" bandwidth reading:\n%s", out)
	}
	if !strings.Contains(out, "BW     DRAM R —  W —") {
		t.Errorf("expected DRAM bandwidth to render as a dash, got:\n%s", out)
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
