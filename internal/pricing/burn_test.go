package pricing

import (
	"testing"
	"time"
)

func TestBurnRatePositiveAfterObservations(t *testing.T) {
	tr := NewBurnTracker(60*time.Second, 0.3)
	t0 := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	tr.Observe("s1", 0.00, t0)
	tr.Observe("s1", 0.01, t0.Add(10*time.Second)) // $0.01 in 10s = $3.60/hr instantaneous

	rate := tr.RatePerHour("s1", t0.Add(10*time.Second))
	if rate <= 0 {
		t.Fatalf("RatePerHour = %v, want > 0 after a cost increase", rate)
	}
}

// TestBurnRateGoesQuietDropsToZero: a session with no new records inside
// the window reports $0.00/hr, never the last nonzero value.
func TestBurnRateGoesQuietDropsToZero(t *testing.T) {
	window := 60 * time.Second
	tr := NewBurnTracker(window, 0.3)
	t0 := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	tr.Observe("s1", 0.00, t0)
	tr.Observe("s1", 0.02, t0.Add(10*time.Second))
	tr.Observe("s1", 0.05, t0.Add(20*time.Second))

	if rate := tr.RatePerHour("s1", t0.Add(20*time.Second)); rate <= 0 {
		t.Fatalf("RatePerHour = %v immediately after activity, want > 0", rate)
	}

	quietAt := t0.Add(20*time.Second + window + time.Second)
	if rate := tr.RatePerHour("s1", quietAt); rate != 0 {
		t.Fatalf("RatePerHour = %v once quiet past the window, want exactly 0, not the last nonzero value", rate)
	}
}

func TestBurnRateUnknownSessionIsZero(t *testing.T) {
	tr := NewBurnTracker(60*time.Second, 0.3)
	if rate := tr.RatePerHour("never-observed", time.Now()); rate != 0 {
		t.Fatalf("RatePerHour(never-observed) = %v, want 0", rate)
	}
}

// TestForgetRemovesTrackedSessions: forgetting a set of observed sessions
// leaves the tracker reporting 0 for each, as if they were never observed.
func TestForgetRemovesTrackedSessions(t *testing.T) {
	tr := NewBurnTracker(60*time.Second, 0.3)
	t0 := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	ids := []string{"s1", "s2", "s3"}
	for _, id := range ids {
		tr.Observe(id, 0.00, t0)
		tr.Observe(id, 0.01, t0.Add(10*time.Second))
		if rate := tr.RatePerHour(id, t0.Add(10*time.Second)); rate <= 0 {
			t.Fatalf("RatePerHour(%s) = %v before Forget, want > 0", id, rate)
		}
	}

	for _, id := range ids {
		tr.Forget(id)
	}

	for _, id := range ids {
		if rate := tr.RatePerHour(id, t0.Add(10*time.Second)); rate != 0 {
			t.Fatalf("RatePerHour(%s) = %v after Forget, want 0", id, rate)
		}
	}

	if got := len(tr.byID); got != 0 {
		t.Fatalf("byID has %d entries after forgetting all observed sessions, want 0", got)
	}
}

func TestBurnRateNegativeDeltaFlooredAtZero(t *testing.T) {
	tr := NewBurnTracker(60*time.Second, 0.3)
	t0 := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	tr.Observe("s1", 0.10, t0)
	tr.Observe("s1", 0.05, t0.Add(5*time.Second)) // a corrected/rewound total

	if rate := tr.RatePerHour("s1", t0.Add(5*time.Second)); rate < 0 {
		t.Fatalf("RatePerHour = %v, want a floored, non-negative rate", rate)
	}
}
