package proc

import (
	"testing"
	"time"
)

func TestCPUPct(t *testing.T) {
	cases := []struct {
		name   string
		prevNs uint64
		curNs  uint64
		wall   time.Duration
		want   float64
	}{
		{
			name:   "known 500ms wall, 250ms cpu -> 50%",
			prevNs: 1_000_000_000,
			curNs:  1_250_000_000,
			wall:   500 * time.Millisecond,
			want:   50,
		},
		{
			name:   "zero wall time guards against divide-by-zero",
			prevNs: 1_000_000_000,
			curNs:  1_250_000_000,
			wall:   0,
			want:   0,
		},
		{
			name:   "negative wall time also guards",
			prevNs: 1_000_000_000,
			curNs:  1_250_000_000,
			wall:   -time.Second,
			want:   0,
		},
		{
			name:   "counter wrap (curNs < prevNs) reports 0, not a negative percentage",
			prevNs: 5_000_000_000,
			curNs:  1_000_000,
			wall:   time.Second,
			want:   0,
		},
		{
			name:   "equal samples over real wall time is legitimately 0%",
			prevNs: 42,
			curNs:  42,
			wall:   time.Second,
			want:   0,
		},
		{
			name:   "cpu can exceed 100% with multiple threads, never clamped",
			prevNs: 0,
			curNs:  3_000_000_000,
			wall:   time.Second,
			want:   300,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CPUPct(c.prevNs, c.curNs, c.wall)
			if got != c.want {
				t.Fatalf("CPUPct(%d, %d, %s) = %v, want %v", c.prevNs, c.curNs, c.wall, got, c.want)
			}
		})
	}
}

func TestCPUTrackerFirstSampleYieldsNoPercentage(t *testing.T) {
	tr := NewCPUTracker()
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)

	pct, ok := tr.Update(407, 1_000_000_000, now)
	if ok {
		t.Fatalf("first sample: ok = true, want false (no baseline yet); got pct=%v", pct)
	}

	// A second sample 500ms later, 250ms of CPU consumed, must now report
	// a real delta-based percentage rather than a lifetime average
	// (1.25s cumulative / process age would read flat and wrong).
	pct, ok = tr.Update(407, 1_250_000_000, now.Add(500*time.Millisecond))
	if !ok {
		t.Fatalf("second sample: ok = false, want true")
	}
	if pct != 50 {
		t.Fatalf("second sample: pct = %v, want 50", pct)
	}
}

func TestCPUTrackerPrune(t *testing.T) {
	tr := NewCPUTracker()
	now := time.Now()
	tr.Update(1, 100, now)
	tr.Update(2, 100, now)

	tr.Prune(map[int]bool{1: true})

	// pid 2 was pruned, so it must look like a first sample again.
	_, ok := tr.Update(2, 200, now.Add(time.Second))
	if ok {
		t.Fatalf("pid 2 should have been pruned and re-baseline, got ok=true")
	}
	// pid 1 was kept, so it must still have its baseline.
	_, ok = tr.Update(1, 200, now.Add(time.Second))
	if !ok {
		t.Fatalf("pid 1 should have kept its baseline, got ok=false")
	}
}

func TestGPUTracker(t *testing.T) {
	tr := NewGPUTracker()
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)

	// First sample: baseline only, pid omitted from the result.
	deltas := tr.Update(map[int]uint64{407: 1_000_000_000}, now)
	if _, ok := deltas[407]; ok {
		t.Fatalf("first GPU sample should be omitted (baseline only), got %v", deltas)
	}

	// Second sample 1.22s later: the plan's own measured datapoint, pid
	// 407 at 20.026 ms/sec, corresponds to a ~24.43e6 ns delta.
	elapsed := 1220 * time.Millisecond
	deltas = tr.Update(map[int]uint64{407: 1_000_000_000 + 24_431_720}, now.Add(elapsed))
	got, ok := deltas[407]
	if !ok {
		t.Fatalf("second GPU sample should report a delta")
	}
	if got < 20.0 || got > 20.06 {
		t.Fatalf("GPU ms/sec = %v, want ~20.026", got)
	}
}

func TestGPUTrackerPrune(t *testing.T) {
	tr := NewGPUTracker()
	now := time.Now()
	tr.Update(map[int]uint64{1: 100, 2: 100}, now)

	tr.Prune(map[int]bool{1: true})

	deltas := tr.Update(map[int]uint64{1: 200, 2: 200}, now.Add(time.Second))
	if _, ok := deltas[2]; ok {
		t.Fatalf("pid 2 should have been pruned and re-baseline, got a delta")
	}
	if _, ok := deltas[1]; !ok {
		t.Fatalf("pid 1 should have kept its baseline")
	}
}

func TestRescaleGPUPct(t *testing.T) {
	t.Run("nil system pct leaves every entry nil", func(t *testing.T) {
		out := RescaleGPUPct(map[int]float64{1: 10, 2: 30}, nil)
		for pid, v := range out {
			if v != nil {
				t.Fatalf("pid %d: got %v, want nil", pid, *v)
			}
		}
	})

	t.Run("zero total leaves every entry nil rather than dividing by a stand-in", func(t *testing.T) {
		sysPct := 40.0
		out := RescaleGPUPct(map[int]float64{1: 0, 2: 0}, &sysPct)
		for pid, v := range out {
			if v != nil {
				t.Fatalf("pid %d: got %v, want nil", pid, *v)
			}
		}
	})

	t.Run("proportional split sums to systemGPUActivePct", func(t *testing.T) {
		sysPct := 40.0
		out := RescaleGPUPct(map[int]float64{1: 10, 2: 30}, &sysPct)
		if out[1] == nil || out[2] == nil {
			t.Fatalf("expected non-nil results, got %v", out)
		}
		if *out[1] != 10 { // 10/40 * 40
			t.Fatalf("pid 1 pct = %v, want 10", *out[1])
		}
		if *out[2] != 30 { // 30/40 * 40
			t.Fatalf("pid 2 pct = %v, want 30", *out[2])
		}
	})
}

func TestStartWall(t *testing.T) {
	numer, denom := uint32(125), uint32(3) // Apple Silicon mach timebase
	nowTicks := uint64(1_000_000_000)
	nowWall := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)

	t.Run("10 seconds earlier", func(t *testing.T) {
		startTicks := nowTicks - 240_000_000 // 240e6 ticks * 125/3 ns/tick = 10s
		got := StartWall(startTicks, nowTicks, numer, denom, nowWall)
		want := nowWall.Add(-10 * time.Second)
		if !got.Equal(want) {
			t.Fatalf("StartWall = %v, want %v", got, want)
		}
	})

	t.Run("equal ticks yields exactly nowWall", func(t *testing.T) {
		got := StartWall(nowTicks, nowTicks, numer, denom, nowWall)
		if !got.Equal(nowWall) {
			t.Fatalf("StartWall = %v, want %v", got, nowWall)
		}
	})

	t.Run("startTicks after nowTicks returns the zero time", func(t *testing.T) {
		got := StartWall(nowTicks+1, nowTicks, numer, denom, nowWall)
		if !got.IsZero() {
			t.Fatalf("StartWall = %v, want the zero time", got)
		}
	})

	t.Run("naive epoch interpretation lands decades away from the anchor", func(t *testing.T) {
		startTicks := nowTicks - 240_000_000
		correct := StartWall(startTicks, nowTicks, numer, denom, nowWall)

		// The bug this guards against: treating the raw tick count as if
		// it were itself a duration since the Unix epoch, skipping the
		// anchor subtraction entirely.
		naiveNs := startTicks * uint64(numer) / uint64(denom)
		naive := time.Unix(0, 0).Add(time.Duration(naiveNs))

		diff := correct.Sub(naive)
		if diff < 0 {
			diff = -diff
		}
		const decade = 10 * 365 * 24 * time.Hour
		if diff < decade {
			t.Fatalf("naive interpretation (%v) is not decades away from the correct anchor (%v): diff %v", naive, correct, diff)
		}
	})
}
