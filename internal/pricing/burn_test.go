package pricing

import (
	"testing"
	"time"
)

func TestBurnRatePositiveAfterObservations(t *testing.T) {
	tr := NewBurnTracker(60*time.Second, 0.3)
	t0 := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	tr.Observe("s1", 0.00, t0, time.Time{})
	tr.Observe("s1", 0.01, t0.Add(10*time.Second), time.Time{}) // $0.01 in 10s = $3.60/hr instantaneous

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

	tr.Observe("s1", 0.00, t0, time.Time{})
	tr.Observe("s1", 0.02, t0.Add(10*time.Second), time.Time{})
	tr.Observe("s1", 0.05, t0.Add(20*time.Second), time.Time{})

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
		tr.Observe(id, 0.00, t0, time.Time{})
		tr.Observe(id, 0.01, t0.Add(10*time.Second), time.Time{})
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

	tr.Observe("s1", 0.10, t0, time.Time{})
	tr.Observe("s1", 0.05, t0.Add(5*time.Second), time.Time{}) // a corrected/rewound total

	if rate := tr.RatePerHour("s1", t0.Add(5*time.Second)); rate < 0 {
		t.Fatalf("RatePerHour = %v, want a floored, non-negative rate", rate)
	}
}

// TestFirstSightingOfExistingSpendContributesNothing: the tailers read hours
// of transcript in wattop's first second or two, so the first cumulative
// total a session id is ever seen with is money spent before wattop ran. It
// baselines and contributes $0/hr — and it must seed the tracker rather than
// poison it, so a real delta afterwards still reports a plausible rate.
func TestFirstSightingOfExistingSpendContributesNothing(t *testing.T) {
	tr := NewBurnTracker(60*time.Second, 0.3)
	t0 := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	tr.Observe("s1", 300.00, t0, t0)
	if rate := tr.RatePerHour("s1", t0); rate != 0 {
		t.Fatalf("RatePerHour after a $300 first sighting = %v, want exactly 0", rate)
	}

	// $0.02 of genuinely new spend, 20s later.
	at := t0.Add(20 * time.Second)
	tr.Observe("s1", 300.02, at, at)
	if got, want := tr.RatePerHour("s1", at), 3.60; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("RatePerHour after a $0.02/20s delta = %v, want %v", got, want)
	}
}

// TestPostBaselineDeltaIsTheRate: $0.50 in 30s is $60/hr, exactly — the
// first counting delta seeds the EWMA rather than being blended with a zero.
func TestPostBaselineDeltaIsTheRate(t *testing.T) {
	tr := NewBurnTracker(60*time.Second, 0.3)
	t0 := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	at := t0.Add(30 * time.Second)

	tr.Observe("s1", 1.00, t0, t0)
	tr.Observe("s1", 1.50, at, at)

	if got, want := tr.RatePerHour("s1", at), 60.0; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("RatePerHour for $0.50 in 30s = %v, want %v", got, want)
	}
}

// TestBackfilledDeltaOlderThanWindowContributesNothing: a resumed session
// whose newest usage record is two hours old is history the tailer has only
// just caught up with, however recently it was read. It must not register as
// spend inside the window.
func TestBackfilledDeltaOlderThanWindowContributesNothing(t *testing.T) {
	tr := NewBurnTracker(60*time.Second, 0.3)
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	// Both observations land ~now on the wall clock (the tailer read them
	// 1.3s apart at launch); their usage timestamps are two hours old.
	tr.Observe("s1", 300.00, now, now.Add(-2*time.Hour-30*time.Second))
	tr.Observe("s1", 338.50, now.Add(1300*time.Millisecond), now.Add(-2*time.Hour))

	if rate := tr.RatePerHour("s1", now.Add(1300*time.Millisecond)); rate != 0 {
		t.Fatalf("RatePerHour for a $38.50 backfill of 2h-old usage = %v, want exactly 0", rate)
	}
}

// TestFirstEventTimestampDoesNotSpike: the backfilled chunk that baselines a
// session can contain no tool_use record at all, so its event time is
// unknown while the next observation's is fresh. That pair bounds no span,
// and falling back to the wall clock there is exactly the $105k/hr bug —
// the delta must be discarded, and must not ride along into the next one.
func TestFirstEventTimestampDoesNotSpike(t *testing.T) {
	tr := NewBurnTracker(60*time.Second, 0.3)
	t0 := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	tr.Observe("s1", 324.33, t0, time.Time{})
	at1 := t0.Add(1300 * time.Millisecond)
	tr.Observe("s1", 362.83, at1, at1)
	if rate := tr.RatePerHour("s1", at1); rate != 0 {
		t.Fatalf("RatePerHour across an unknown→known event time = %v, want exactly 0", rate)
	}

	// The discarded $38.50 must not reappear in the next delta: $0.01 more,
	// 10s later, is $3.60/hr and nothing else.
	at2 := at1.Add(10 * time.Second)
	tr.Observe("s1", 362.84, at2, at2)
	if got, want := tr.RatePerHour("s1", at2), 3.60; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("RatePerHour after the discarded delta = %v, want %v", got, want)
	}
}

// TestSameInstantUsageFallsBackToWallClock: several usage records sharing one
// transcript timestamp bound a zero-length event span. The wall clock is the
// longer of the two spans there, so the delta still reports a rate instead of
// dividing by zero or being thrown away.
func TestSameInstantUsageFallsBackToWallClock(t *testing.T) {
	tr := NewBurnTracker(60*time.Second, 0.3)
	t0 := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	ev := t0.Add(-time.Second)

	tr.Observe("s1", 1.00, t0, ev)
	at := t0.Add(10 * time.Second)
	tr.Observe("s1", 1.01, at, ev)

	if got, want := tr.RatePerHour("s1", at), 3.60; got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("RatePerHour for $0.01 over a zero-length event span = %v, want the wall-clock %v", got, want)
	}
}
