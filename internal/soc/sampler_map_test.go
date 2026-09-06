//go:build darwin && arm64 && cgo

// sampler_map_test.go covers the pure Composite -> domain.SysSample mapping.
// It deliberately carries no `hardware` tag: the mapping is where both SoC
// defects from the 2026-09-06 QA run lived, and a rule that only runs under
// `go test -tags=hardware` is a rule the default suite cannot defend.
// Sampling real IOReport stays in sampler_hw_test.go.

package soc

import (
	"reflect"
	"slices"
	"testing"
)

// bwFixture builds the only part of a Composite bandwidthFromComposite reads.
func bwFixture(src DRAMBWSource, read, write, ane float64) Composite {
	var c Composite
	c.CPU.DRAMBWSource = src
	c.CPU.DRAMReadBW = read
	c.CPU.DRAMWriteBW = write
	c.CPU.ANEBW = ane
	return c
}

func deref(t *testing.T, name string, v *float64) float64 {
	t.Helper()
	if v == nil {
		t.Fatalf("%s: expected a reading, got nil (unresolved)", name)
	}
	return *v
}

// TestBandwidthZeroIsAReadingNotAnAbsence is the QA regression: a DRAM
// channel that resolved and counted zero bytes must publish 0.0 GB/s, not a
// dash. Treating an exact 0.0 as "unresolved" is what made wattop dash live
// DRAM traffic and claim the channel was dead on hardware where it is not.
func TestBandwidthZeroIsAReadingNotAnAbsence(t *testing.T) {
	var missing []string
	bw := bandwidthFromComposite(bwFixture(DRAMBWDirectional, 0, 0, 0), &missing)

	if got := deref(t, "dram_read_gbs", bw.DRAMReadGBs); got != 0 {
		t.Errorf("dram_read_gbs = %v, want 0", got)
	}
	if got := deref(t, "dram_write_gbs", bw.DRAMWriteGBs); got != 0 {
		t.Errorf("dram_write_gbs = %v, want 0", got)
	}
	if got := deref(t, "dram_combined_gbs", bw.DRAMCombinedGBs); got != 0 {
		t.Errorf("dram_combined_gbs = %v, want 0", got)
	}
	for _, key := range []string{"dram_read_bw_gbs", "dram_write_bw_gbs", "dram_bw_combined_gbs"} {
		if slices.Contains(missing, key) {
			t.Errorf("%q reported missing, but its source resolved and counted zero", key)
		}
	}
}

// TestBandwidthAbsentSourceIsUnresolved is the other half of the same rule:
// with no source at all, every DRAM field is nil and named in Missing.
func TestBandwidthAbsentSourceIsUnresolved(t *testing.T) {
	var missing []string
	bw := bandwidthFromComposite(bwFixture(DRAMBWNone, 0, 0, 0), &missing)

	if bw.DRAMReadGBs != nil || bw.DRAMWriteGBs != nil || bw.DRAMCombinedGBs != nil {
		t.Errorf("no DRAM source should leave every field nil, got %+v", bw)
	}
	want := []string{"dram_read_bw_gbs", "dram_write_bw_gbs", "dram_bw_combined_gbs", "ane_bw_combined_gbs"}
	if !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
}

// TestBandwidthNeverPublishesOneChannelTwice is the duplication regression.
// mactop hands over two byte counts whatever the source; on a combined
// counter, and on the DRAM-power estimate, those two are one figure halved.
// Publishing them as dram_read_gbs and dram_write_gbs produced two fields
// identical to fifteen significant figures on every live sample. Only a
// directional source may fill the two directions.
func TestBandwidthNeverPublishesOneChannelTwice(t *testing.T) {
	for _, tc := range []struct {
		name          string
		src           DRAMBWSource
		wantEstimated bool
	}{
		{"combined counter", DRAMBWCombinedCounter, false},
		{"power estimate", DRAMBWEstimated, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var missing []string
			// 16.4 + 16.4: one 32.8 GB/s figure split across the two fields.
			bw := bandwidthFromComposite(bwFixture(tc.src, 16.4, 16.4, 0), &missing)

			if bw.DRAMReadGBs != nil || bw.DRAMWriteGBs != nil {
				t.Errorf("a non-directional source must leave both directions nil, got read=%v write=%v",
					bw.DRAMReadGBs, bw.DRAMWriteGBs)
			}
			if got := deref(t, "dram_combined_gbs", bw.DRAMCombinedGBs); got != 32.8 {
				t.Errorf("dram_combined_gbs = %v, want 32.8 (the two halves added back)", got)
			}
			if bw.DRAMEstimated != tc.wantEstimated {
				t.Errorf("DRAMEstimated = %v, want %v", bw.DRAMEstimated, tc.wantEstimated)
			}
			for _, key := range []string{"dram_read_bw_gbs", "dram_write_bw_gbs"} {
				if !slices.Contains(missing, key) {
					t.Errorf("%q should be reported missing when direction was never measured", key)
				}
			}
			if slices.Contains(missing, "dram_bw_combined_gbs") {
				t.Error("dram_bw_combined_gbs reported missing, but the combined figure resolved")
			}
		})
	}
}

// TestBandwidthDirectionalKeepsBothDirections proves a genuinely directional
// source is passed through untouched, with the total as their sum.
func TestBandwidthDirectionalKeepsBothDirections(t *testing.T) {
	var missing []string
	bw := bandwidthFromComposite(bwFixture(DRAMBWDirectional, 12.5, 7.5, 0), &missing)

	if got := deref(t, "dram_read_gbs", bw.DRAMReadGBs); got != 12.5 {
		t.Errorf("dram_read_gbs = %v, want 12.5", got)
	}
	if got := deref(t, "dram_write_gbs", bw.DRAMWriteGBs); got != 7.5 {
		t.Errorf("dram_write_gbs = %v, want 7.5", got)
	}
	if got := deref(t, "dram_combined_gbs", bw.DRAMCombinedGBs); got != 20 {
		t.Errorf("dram_combined_gbs = %v, want 20", got)
	}
	if bw.DRAMEstimated {
		t.Error("a counted directional reading must not be flagged as an estimate")
	}
}

// TestTempsPublishOnlyAggregatedKeys is the Temp-row regression: the sampler
// used to fold every raw SMC and HID sensor key into Temps -- 325 of them on
// the QA machine -- and the panel printed all of them across the frame.
func TestTempsPublishOnlyAggregatedKeys(t *testing.T) {
	var c Composite
	c.CPU.CPUTemp = 50.5
	c.CPU.GPUTemp = 51.25
	c.CPU.SoCTemp = 52.0
	c.CPU.TempSensors = []TempSensor{
		{Key: "TAOL", Name: "Ambient", Value: 28.5},
		{Key: "Tg5q", Name: "GPU die", Value: 51.0},
		{Key: "TB0T", Name: "Battery", Value: 30.0},
	}

	got := tempsFromComposite(c)
	want := map[string]float64{"cpu": 50.5, "gpu": 51.25, "soc": 52.0}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("temps = %v, want %v (raw sensor keys are diagnostics, not metrics)", got, want)
	}
}

// TestTempsOmitUnreadSensor keeps a sensor that read zero out of the map
// rather than publishing a literal 0.0 °C: the panel dashes a missing key.
func TestTempsOmitUnreadSensor(t *testing.T) {
	var c Composite
	c.CPU.CPUTemp = 50.5
	c.CPU.GPUTemp = 0
	c.CPU.SoCTemp = 0

	got := tempsFromComposite(c)
	want := map[string]float64{"cpu": 50.5}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("temps = %v, want %v", got, want)
	}
}

// TestChannelsResolveOnSourceNotValue guards doctor's channel report. Sampler
// .Channels() takes a fresh 50 ms sample, in which the DRAM-power estimator
// can never have calibrated, so a value-based rule reported DRAM bandwidth
// permanently unresolved no matter what the TUI was showing.
func TestChannelsResolveOnSourceNotValue(t *testing.T) {
	t.Run("directional zero resolves", func(t *testing.T) {
		ch := channelsFromComposite(bwFixture(DRAMBWDirectional, 0, 0, 0))
		for _, key := range []string{"dram_read_bw_gbs", "dram_write_bw_gbs", "dram_bw_combined_gbs"} {
			if !ch[key] {
				t.Errorf("%q reported unresolved on a live channel reading zero", key)
			}
		}
	})

	t.Run("combined-only resolves the total, not the directions", func(t *testing.T) {
		ch := channelsFromComposite(bwFixture(DRAMBWEstimated, 16.4, 16.4, 0))
		if ch["dram_read_bw_gbs"] || ch["dram_write_bw_gbs"] {
			t.Error("directions reported resolved on a source that never measured direction")
		}
		if !ch["dram_bw_combined_gbs"] {
			t.Error("dram_bw_combined_gbs reported unresolved on a resolved combined figure")
		}
	})

	t.Run("no source resolves nothing", func(t *testing.T) {
		ch := channelsFromComposite(bwFixture(DRAMBWNone, 0, 0, 0))
		for _, key := range []string{"dram_read_bw_gbs", "dram_write_bw_gbs", "dram_bw_combined_gbs"} {
			if ch[key] {
				t.Errorf("%q reported resolved with no source at all", key)
			}
		}
	})
}
