//go:build darwin && arm64 && cgo

package soc

import (
	"reflect"
	"slices"
	"testing"
)

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

func TestThrottledReachesSysSample(t *testing.T) {
	var c Composite
	c.Thermal = 2
	c.Throttled = true
	if s := sysSampleFromComposite(c); !s.Throttled {
		t.Error("SysSample.Throttled = false, want true for a throttled Composite")
	}
}
