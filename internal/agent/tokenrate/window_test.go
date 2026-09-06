package tokenrate

import (
	"testing"
	"time"
)

func TestWindowEventTimeAndExpiry(t *testing.T) {
	var w Window
	now := time.Unix(10_000, 0)
	if w.Rate(now) != nil {
		t.Fatal("unobserved usage must be unknown")
	}
	w.Add(now.Add(-time.Hour), 9_000_000, 900_000, 0)
	w.Add(now.Add(-30*time.Second), 6000, 600, 3000)
	w.Add(now.Add(-time.Second), 6000, 1200, 3000)
	r := w.Rate(now)
	if r.InputPerSec != 200 || r.OutputPerSec != 30 || r.CacheReadPerSec != 100 {
		t.Fatalf("rate = %+v, want input 200, output 30, cache 100", r)
	}
	if r := w.Rate(now.Add(time.Minute)); r == nil || r.OutputPerSec != 0 {
		t.Fatalf("idle rate must expire to zero: %+v", r)
	}
}

func TestWindowStorageBounded(t *testing.T) {
	var w Window
	for i := int64(1); i <= 10000; i++ {
		w.Add(time.Unix(i, 0), 1, 1, 0)
	}
	if len(w.buckets) > 61 {
		t.Fatalf("retained %d buckets", len(w.buckets))
	}
}
