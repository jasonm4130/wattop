//go:build hardware

package soc

import (
	"context"
	"testing"
)

// TestLiveSample exercises the real vendored collectors against live
// hardware. It t.Skips (with a reason) when Init fails — e.g. running on a
// non-Apple-Silicon machine, where sampler_stub.go's ErrUnsupportedPlatform
// is what Init returns.
func TestLiveSample(t *testing.T) {
	s := NewSampler()
	if err := s.Init(); err != nil {
		t.Skipf("soc.Sampler.Init failed, skipping live hardware test: %v", err)
	}
	defer s.Close()

	sample, err := s.Sample(context.Background(), 1000)
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}

	if sample.SoCName == "" {
		t.Error("SoCName is empty")
	}

	nonzeroClusters := 0
	for _, c := range sample.Clusters {
		if c.CoreCount > 0 {
			nonzeroClusters++
		}
	}
	if nonzeroClusters < 2 {
		t.Errorf("want at least two clusters with nonzero CoreCount, got %d (clusters=%+v)", nonzeroClusters, sample.Clusters)
	}

	if sample.Power.CPUWatts == nil {
		t.Error("Power.CPUWatts is nil")
	} else if v := *sample.Power.CPUWatts; v <= 0 || v >= 200 {
		t.Errorf("Power.CPUWatts = %v, want in (0, 200)", v)
	}

	if cpuTemp, ok := sample.Temps["cpu"]; !ok {
		t.Error(`Temps["cpu"] missing`)
	} else if cpuTemp <= 0 || cpuTemp >= 120 {
		t.Errorf(`Temps["cpu"] = %v, want in (0, 120)`, cpuTemp)
	}
}
