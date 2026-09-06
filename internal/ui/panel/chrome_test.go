package panel

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

func TestHardwareLayout(t *testing.T) {
	s := loadFixtureSample(t, "mactop-ndjson.jsonl")
	total := 24.5
	s.Bandwidth.DRAMCombinedGBs = &total
	s.Bandwidth.DRAMEstimated = true
	for _, name := range theme.Names() {
		r, err := theme.Load(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, width := range []int{80, 110, 120, 160} {
			for _, plain := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%d/plain=%t", name, width, plain), func(t *testing.T) {
					height := HardwareHeight(s, width)
					out := HardwareRender(s, r, width, height, Options{NoColor: plain})
					lines := strings.Split(out, "\n")
					if len(lines) != height {
						t.Fatalf("got %d rows, want %d", len(lines), height)
					}
					for i, line := range lines {
						if got := lipgloss.Width(line); got != width {
							t.Errorf("row %d: got width %d, want %d", i, got, width)
						}
					}
					for _, want := range []string{"~24.5 GB/s", "ANE", "Disk", "Net", "Swap", "RPM"} {
						if !strings.Contains(ansi.Strip(out), want) {
							t.Errorf("missing %q in hardware panel", want)
						}
					}
					if plain && strings.Contains(out, "\x1b[") {
						t.Error("no-colour frame contains ANSI")
					}
				})
			}
		}
	}
}

func TestHardwareExtraClustersRemainVisible(t *testing.T) {
	s := domain.SysSample{Clusters: []domain.Cluster{{Label: "E"}, {Label: "P"}, {Label: "S"}, {Label: "Q"}}}
	r := loadDarkRoles(t)
	out := HardwareRender(s, r, 120, HardwareHeight(s, 120), Options{NoColor: true})
	for _, want := range []string{"E (0)", "P (0)", "S (0)", "Q (0)", "ANE BW —", "Disk"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q: %s", want, out)
		}
	}
}

func TestHardwareGolden(t *testing.T) {
	s := loadFixtureSample(t, "mactop-ndjson.jsonl")
	requireGoldenText(t, "hardware", HardwareRender(s, loadDarkRoles(t), 120, HardwareHeight(s, 120), Options{}))
}
